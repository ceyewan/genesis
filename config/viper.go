package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/joho/godotenv"
	"github.com/spf13/viper"

	"github.com/ceyewan/genesis/xerrors"
)

const defaultWatchDebounce = 250 * time.Millisecond

// loader 实现 Loader 接口
type loader struct {
	v         *viper.Viper
	cfg       *Config
	mu        sync.RWMutex
	watches   map[string][]chan Event
	oldValues map[string]interface{}

	watchOnce sync.Once
	watchErr  error
}

// newLoader 创建一个新的配置加载器（内部使用）
func newLoader(cfg *Config) (Loader, error) {
	v := viper.New()
	return &loader{
		v:         v,
		cfg:       cfg,
		watches:   make(map[string][]chan Event),
		oldValues: make(map[string]interface{}),
	}, nil
}

// Load 初始化并从所有来源加载配置
func (l *loader) Load(ctx context.Context) error {
	// 1. 配置 Viper
	l.mu.Lock()
	l.v.SetConfigName(l.cfg.Name)
	l.v.SetConfigType(l.cfg.FileType)

	for _, path := range l.cfg.Paths {
		l.v.AddConfigPath(path)
	}

	// 2. 环境变量设置（最高优先级）- 先设置，确保能捕获所有环境变量
	l.v.SetEnvPrefix(l.cfg.EnvPrefix)
	l.v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	l.v.AutomaticEnv()

	// 3. 尝试加载 .env 文件（高优先级）- 在配置文件之前加载
	if err := l.loadDotEnv(); err != nil {
		l.mu.Unlock()
		return err
	}

	// 4. 加载基础配置（最低优先级）
	if err := l.v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			l.mu.Unlock()
			return xerrors.Wrapf(err, "failed to read config file %s", l.cfg.Name)
		}
	}

	// 5. 加载环境特定配置（中等优先级）
	if err := l.loadEnvironmentConfig(); err != nil {
		l.mu.Unlock()
		return err
	}

	// 6. 验证配置
	if err := l.validateLocked(); err != nil {
		l.mu.Unlock()
		return err
	}

	// 7. 保存当前值作为基线
	l.captureCurrentValues()

	l.mu.Unlock()

	return nil
}

// loadDotEnv 尝试从项目目录加载 .env 文件
// 注意：godotenv.Load 会覆盖已有环境变量，但 Viper 的 AutomaticEnv 会在 Get 时
// 优先读取运行时环境变量，因此实际效果仍是"运行时环境变量 > 文件配置"。
func (l *loader) loadDotEnv() error {
	var lastErr error

	for _, path := range l.cfg.Paths {
		envPath := filepath.Join(path, ".env")
		if _, err := os.Stat(envPath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return xerrors.Wrapf(err, "failed to stat .env file %s", envPath)
		}

		if err := godotenv.Load(envPath); err != nil {
			lastErr = err
		}
	}

	return lastErr
}

// loadEnvironmentConfig 加载环境特定配置文件
func (l *loader) loadEnvironmentConfig() error {
	env := os.Getenv(fmt.Sprintf("%s_ENV", l.cfg.EnvPrefix))
	if env == "" {
		return nil
	}

	originalName := l.cfg.Name
	envConfigName := fmt.Sprintf("%s.%s", l.cfg.Name, env)
	l.v.SetConfigName(envConfigName)

	if err := l.v.MergeInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return xerrors.Wrapf(err, "failed to merge environment config %s", envConfigName)
		}
	}

	l.v.SetConfigName(originalName)
	return nil
}

// captureCurrentValues 保存当前配置值用于变更检测
func (l *loader) captureCurrentValues() {
	for key := range l.watches {
		l.oldValues[key] = l.v.Get(key)
	}
}

// Get 根据 key 获取配置值
func (l *loader) Get(key string) any {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.v.Get(key)
}

// Unmarshal 将整个配置反序列化到结构体
func (l *loader) Unmarshal(v any) error {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.v.Unmarshal(v)
}

// UnmarshalKey 将特定配置 key 反序列化到结构体
func (l *loader) UnmarshalKey(key string, v any) error {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.v.UnmarshalKey(key, v)
}

// Watch 订阅特定配置 key 的变更
func (l *loader) Watch(ctx context.Context, key string) (<-chan Event, error) {
	if err := l.ensureWatching(); err != nil {
		return nil, err
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	ch := make(chan Event, 10)
	l.watches[key] = append(l.watches[key], ch)
	l.oldValues[key] = l.v.Get(key)

	go func() {
		<-ctx.Done()
		l.removeWatch(key, ch)
	}()

	return ch, nil
}

// removeWatch 从注册表中移除监听通道
func (l *loader) removeWatch(key string, ch chan Event) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if chans, ok := l.watches[key]; ok {
		for i, c := range chans {
			if c == ch {
				l.watches[key] = append(chans[:i], chans[i+1:]...)
				break
			}
		}
		if len(l.watches[key]) == 0 {
			delete(l.watches, key)
			delete(l.oldValues, key)
		}
	}

	close(ch)
}

// Validate 验证配置
func (l *loader) Validate() error {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return l.validateLocked()
}

func (l *loader) validateLocked() error {
	if len(l.v.AllSettings()) > 0 {
		return nil
	}

	// Viper 的 AutomaticEnv 不会把环境变量映射进 AllSettings/AllKeys，
	// 这里用 EnvPrefix 做一次兜底判断，避免“仅环境变量配置”被误判为空配置。
	prefix := strings.ToUpper(l.cfg.EnvPrefix) + "_"
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, prefix) {
			return nil
		}
	}

	return xerrors.Wrapf(ErrValidationFailed, "configuration is empty")
}

func (l *loader) ensureWatching() error {
	l.watchOnce.Do(func() {
		l.watchErr = l.startFileWatch()
	})
	return l.watchErr
}

func (l *loader) startFileWatch() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return xerrors.Wrapf(err, "failed to create fsnotify watcher")
	}

	watchDirs := make([]string, 0, len(l.cfg.Paths))
	for _, p := range l.cfg.Paths {
		if p == "" {
			continue
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		if st, err := os.Stat(abs); err != nil || !st.IsDir() {
			continue
		}
		watchDirs = append(watchDirs, abs)
	}

	if len(watchDirs) == 0 {
		return nil
	}

	// 先监听目录，避免“原子保存/rename”场景丢事件
	for _, dir := range watchDirs {
		if err := watcher.Add(dir); err != nil {
			_ = watcher.Close()
			return xerrors.Wrapf(err, "failed to watch config dir %s", dir)
		}
	}

	// 构建需要监听的配置文件列表：基础配置 + 环境特定配置
	watchFiles := make(map[string]struct{}, len(watchDirs)*2)
	for _, dir := range watchDirs {
		// 基础配置文件：config.yaml
		watchFiles[filepath.Clean(filepath.Join(dir, l.cfg.Name+"."+l.cfg.FileType))] = struct{}{}

		// 环境特定配置文件：config.{env}.yaml
		if env := os.Getenv(fmt.Sprintf("%s_ENV", l.cfg.EnvPrefix)); env != "" {
			envConfigName := fmt.Sprintf("%s.%s.%s", l.cfg.Name, env, l.cfg.FileType)
			watchFiles[filepath.Clean(filepath.Join(dir, envConfigName))] = struct{}{}
		}
	}

	go l.watchLoop(watcher, watchFiles)
	return nil
}

func (l *loader) watchLoop(watcher *fsnotify.Watcher, targets map[string]struct{}) {
	defer watcher.Close()

	var (
		timer   *time.Timer
		timerC  <-chan time.Time
		pending bool
		last    fsnotify.Event
	)

	stopTimer := func() {
		if timer == nil {
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer = nil
		timerC = nil
	}
	defer stopTimer()

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if _, ok := targets[filepath.Clean(event.Name)]; !ok {
				continue
			}
			if !(event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) || event.Has(fsnotify.Remove) || event.Has(fsnotify.Chmod)) {
				continue
			}

			last = event
			if !pending {
				pending = true
				timer = time.NewTimer(defaultWatchDebounce)
				timerC = timer.C
				continue
			}
			if timer != nil {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(defaultWatchDebounce)
			}

		case <-timerC:
			pending = false
			stopTimer()
			l.reloadAndNotify(last)

		case _, ok := <-watcher.Errors:
			if !ok {
				return
			}
			// 忽略 watcher 错误，尽量不中断服务；后续事件仍可能继续到来。
		}
	}
}

func (l *loader) reloadAndNotify(_ fsnotify.Event) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 重新读取基础配置（Viper ReadInConfig 自带“找文件”的逻辑）
	if err := l.v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			// 配置文件读取失败时，不广播变更（避免把不一致状态推给业务）。
			return
		}
	}

	// 先刷新 .env，保证 ENV 等选择逻辑能拿到最新值
	_ = l.loadDotEnv()
	_ = l.loadEnvironmentConfig()
	l.notifyWatches(fsnotify.Event{})
}

// notifyWatches 通知所有监听者
func (l *loader) notifyWatches(_ fsnotify.Event) {
	for key, channels := range l.watches {
		newValue := l.v.Get(key)
		oldValue := l.oldValues[key]

		if !reflect.DeepEqual(oldValue, newValue) {
			event := Event{
				Key:       key,
				Value:     newValue,
				OldValue:  oldValue,
				Source:    "file",
				Timestamp: time.Now(),
			}

			l.oldValues[key] = newValue

			for _, ch := range channels {
				select {
				case ch <- event:
				default:
				}
			}
		}
	}
}
