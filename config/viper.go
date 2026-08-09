package config

import (
	"context"
	"errors"
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

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/xerrors"
)

const defaultWatchDebounce = 250 * time.Millisecond

var envLoadMu sync.Mutex

// loader 实现 Loader 接口
type loader struct {
	cfg       *Config
	v         *viper.Viper
	logger    clog.Logger
	mu        sync.RWMutex
	loaded    bool
	watches   map[string][]chan Event
	oldValues map[string]any

	watchOnce sync.Once
	watchErr  error
	watcher   *fsnotify.Watcher
	stopCh    chan struct{}
	wg        sync.WaitGroup
	closed    bool
	closeOnce sync.Once
}

// newLoader 创建一个新的配置加载器（内部使用）
func newLoader(cfg *Config, opts ...Option) (Loader, error) {
	l := &loader{
		v:         viper.New(),
		cfg:       cfg,
		logger:    clog.Discard(),
		watches:   make(map[string][]chan Event),
		oldValues: make(map[string]any),
		stopCh:    make(chan struct{}),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(l)
		}
	}
	return l, nil
}

func (l *loader) newConfiguredViper() *viper.Viper {
	v := viper.New()
	v.SetConfigName(l.cfg.Name)
	v.SetConfigType(l.cfg.FileType)

	for _, path := range l.cfg.Paths {
		v.AddConfigPath(path)
	}

	v.SetEnvPrefix(l.cfg.EnvPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.AutomaticEnv()

	return v
}

// Load 初始化并从所有来源加载配置。
func (l *loader) Load(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return ErrClosed
	}

	next := l.newConfiguredViper()

	if err := l.loadDotEnv(); err != nil {
		return err
	}

	if err := next.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			return xerrors.Wrapf(err, "failed to read config file %s", l.cfg.Name)
		}
	}

	if err := l.loadEnvironmentConfig(next); err != nil {
		return err
	}
	if err := l.validateViper(next); err != nil {
		return err
	}

	// Commit the new snapshot only after every source has loaded and validated.
	// A failed reload must leave the last known-good configuration intact.
	l.v = next
	l.loaded = true
	l.captureCurrentValues()

	return nil
}

// loadDotEnv 尝试从项目目录加载 .env 文件。
// .env 只补齐缺失的环境变量，不覆盖当前进程里已经存在的同名变量。
// 由于进程环境变量是全局状态，这里使用包级锁串行化 .env 处理，避免多个 Loader
// 在并发场景下出现 LookupEnv/Setenv 之间的竞态。
func (l *loader) loadDotEnv() error {
	envLoadMu.Lock()
	defer envLoadMu.Unlock()

	for _, path := range l.cfg.Paths {
		envPath := filepath.Join(path, ".env")
		if _, err := os.Stat(envPath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return xerrors.Wrapf(err, "failed to stat .env file %s", envPath)
		}

		values, err := godotenv.Read(envPath)
		if err != nil {
			return xerrors.Wrapf(err, "failed to read .env file %s", envPath)
		}

		for key, value := range values {
			if _, exists := os.LookupEnv(key); exists {
				continue
			}
			if err := os.Setenv(key, value); err != nil {
				return xerrors.Wrapf(err, "failed to set env from .env file %s", envPath)
			}
		}
	}

	return nil
}

// loadEnvironmentConfig 加载环境特定配置文件
func (l *loader) loadEnvironmentConfig(v *viper.Viper) error {
	env := os.Getenv(fmt.Sprintf("%s_ENV", l.cfg.EnvPrefix))
	if env == "" {
		return nil
	}

	originalName := l.cfg.Name
	envConfigName := fmt.Sprintf("%s.%s", l.cfg.Name, env)
	v.SetConfigName(envConfigName)

	if err := v.MergeInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			return xerrors.Wrapf(err, "failed to merge environment config %s", envConfigName)
		}
	}

	v.SetConfigName(originalName)
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
	l.mu.Lock()
	defer l.mu.Unlock()
	l.materializeEnvironmentForTarget("", v)
	return l.v.Unmarshal(v)
}

// UnmarshalKey 将特定配置 key 反序列化到结构体
func (l *loader) UnmarshalKey(key string, v any) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.materializeEnvironmentForTarget(key, v)
	return l.v.UnmarshalKey(key, v)
}

// materializeEnvironmentForTarget binds environment values using the target
// schema. An underscore is valid inside a field name, so an environment name
// cannot be safely converted to a dotted path without knowing that schema.
func (l *loader) materializeEnvironmentForTarget(root string, target any) {
	t := reflect.TypeOf(target)
	if t == nil {
		return
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	l.materializeStructEnvironment(root, t)
}

func (l *loader) materializeStructEnvironment(root string, t reflect.Type) {
	for field := range t.Fields() {
		if field.PkgPath != "" {
			continue
		}
		name, skip, squash := configFieldName(field)
		if skip {
			continue
		}
		path := root
		if !squash {
			if path != "" {
				path += "."
			}
			path += name
		}
		fieldType := field.Type
		for fieldType.Kind() == reflect.Pointer {
			fieldType = fieldType.Elem()
		}
		if fieldType.Kind() == reflect.Struct && fieldType.PkgPath() != "time" {
			l.materializeStructEnvironment(path, fieldType)
			continue
		}
		envName := strings.ToUpper(l.cfg.EnvPrefix + "_" + strings.NewReplacer(".", "_", "-", "_").Replace(path))
		if value, ok := os.LookupEnv(envName); ok {
			l.v.Set(path, value)
		}
	}
}

func configFieldName(field reflect.StructField) (name string, skip, squash bool) {
	name = field.Tag.Get("mapstructure")
	if name == "" {
		name = field.Tag.Get("yaml")
	}
	if name == "" {
		name = field.Tag.Get("json")
	}
	parts := strings.Split(name, ",")
	name = parts[0]
	if name == "-" {
		return "", true, false
	}
	for _, option := range parts[1:] {
		if option == "squash" {
			squash = true
		}
	}
	if name == "" {
		name = strings.ToLower(field.Name)
	}
	return name, false, squash
}

// Watch 订阅特定配置 key 的变更。
func (l *loader) Watch(ctx context.Context, key string) (<-chan Event, error) {
	l.mu.RLock()
	loaded := l.loaded
	closed := l.closed
	l.mu.RUnlock()
	if closed {
		return nil, ErrClosed
	}
	if !loaded {
		return nil, xerrors.Wrapf(ErrNotLoaded, "call Load before Watch")
	}

	if err := l.ensureWatching(); err != nil {
		return nil, err
	}

	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil, ErrClosed
	}

	ch := make(chan Event, 10)
	l.watches[key] = append(l.watches[key], ch)
	l.oldValues[key] = l.v.Get(key)
	l.wg.Add(1)
	l.mu.Unlock()

	go func() {
		defer l.wg.Done()
		select {
		case <-ctx.Done():
			l.removeWatch(key, ch)
		case <-l.stopCh:
		}
	}()

	return ch, nil
}

// removeWatch 从注册表中移除监听通道
func (l *loader) removeWatch(key string, ch chan Event) {
	l.mu.Lock()
	defer l.mu.Unlock()

	found := false
	if chans, ok := l.watches[key]; ok {
		for i, c := range chans {
			if c == ch {
				l.watches[key] = append(chans[:i], chans[i+1:]...)
				found = true
				break
			}
		}
		if len(l.watches[key]) == 0 {
			delete(l.watches, key)
			delete(l.oldValues, key)
		}
	}

	if found {
		close(ch)
	}
}

// Validate 验证配置
func (l *loader) Validate() error {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return l.validateViper(l.v)
}

func (l *loader) validateViper(v *viper.Viper) error {
	if len(v.AllSettings()) > 0 {
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
		_ = watcher.Close()
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

	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		_ = watcher.Close()
		return ErrClosed
	}
	l.watcher = watcher
	l.wg.Add(1)
	l.mu.Unlock()
	go func() {
		defer l.wg.Done()
		l.watchLoop(watcher, watchFiles)
	}()
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
		case <-l.stopCh:
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if _, ok := targets[filepath.Clean(event.Name)]; !ok {
				continue
			}
			if !event.Has(fsnotify.Write) && !event.Has(fsnotify.Create) && !event.Has(fsnotify.Rename) && !event.Has(fsnotify.Remove) && !event.Has(fsnotify.Chmod) {
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

// Close 停止文件监听并关闭所有订阅通道。
func (l *loader) Close() error {
	l.closeOnce.Do(func() {
		l.mu.Lock()
		l.closed = true
		close(l.stopCh)
		watcher := l.watcher
		l.watcher = nil
		for key, channels := range l.watches {
			for _, ch := range channels {
				close(ch)
			}
			delete(l.watches, key)
			delete(l.oldValues, key)
		}
		l.mu.Unlock()
		if watcher != nil {
			_ = watcher.Close()
		}
		l.wg.Wait()
	})
	return nil
}

func (l *loader) reloadAndNotify(event fsnotify.Event) {
	l.mu.Lock()
	defer l.mu.Unlock()

	next := l.newConfiguredViper()

	if err := l.loadDotEnv(); err != nil {
		l.logger.Warn("配置热更新失败：处理 .env 失败",
			clog.String("event", event.Op.String()),
			clog.String("path", event.Name),
			clog.Error(err),
		)
		return
	}

	if err := next.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			l.logger.Warn("配置热更新失败：读取基础配置失败",
				clog.String("event", event.Op.String()),
				clog.String("path", event.Name),
				clog.Error(err),
			)
			return
		}
	}

	if err := l.loadEnvironmentConfig(next); err != nil {
		l.logger.Warn("配置热更新失败：合并环境配置失败",
			clog.String("event", event.Op.String()),
			clog.String("path", event.Name),
			clog.Error(err),
		)
		return
	}
	if err := l.validateViper(next); err != nil {
		l.logger.Warn("配置热更新失败：配置校验失败",
			clog.String("event", event.Op.String()),
			clog.String("path", event.Name),
			clog.Error(err),
		)
		return
	}

	l.v = next
	l.notifyWatches()
}

// notifyWatches 通知所有监听者
func (l *loader) notifyWatches() {
	for key, channels := range l.watches {
		newValue := l.v.Get(key)
		oldValue := l.oldValues[key]

		if !reflect.DeepEqual(oldValue, newValue) {
			event := Event{
				Key:       key,
				Value:     newValue,
				OldValue:  oldValue,
				Source:    EventSourceFile,
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
