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

	"github.com/ceyewan/genesis/pkg/xerrors"
)

// loader 实现 Loader 接口
type loader struct {
	v            *viper.Viper
	opts         *Options
	mu           sync.RWMutex
	watches      map[string][]chan Event
	oldValues    map[string]interface{}
	watchStarted bool
}

// NewLoader 创建一个新的配置加载器
func NewLoader(opts ...Option) (Loader, error) {
	options := defaultOptions()
	for _, o := range opts {
		o(options)
	}

	v := viper.New()
	return &loader{
		v:         v,
		opts:      options,
		watches:   make(map[string][]chan Event),
		oldValues: make(map[string]interface{}),
	}, nil
}

// Load 初始化并从所有来源加载配置
func (l *loader) Load(ctx context.Context) error {
	// 1. 尝试加载 .env 文件
	if err := l.loadDotEnv(); err != nil {
		fmt.Printf("Warning: failed to load .env file: %v\n", err)
	}

	// 2. 配置 Viper
	l.v.SetConfigName(l.opts.Name)
	l.v.SetConfigType(l.opts.FileType)

	for _, path := range l.opts.Paths {
		l.v.AddConfigPath(path)
	}

	// 3. 环境变量设置
	l.v.SetEnvPrefix(l.opts.EnvPrefix)
	l.v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	l.v.AutomaticEnv()

	// 4. 加载基础配置
	if err := l.v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return xerrors.Wrapf(err, "failed to read config file %s", l.opts.Name)
		}
		fmt.Printf("Warning: no configuration file found at %s\n", l.v.ConfigFileUsed())
	}

	// 5. 加载环境特定配置
	if err := l.loadEnvironmentConfig(); err != nil {
		return err
	}

	// 6. 验证配置
	if err := l.Validate(); err != nil {
		return err
	}

	// 7. 保存当前值作为基线
	l.captureCurrentValues()

	return nil
}

// loadDotEnv 尝试从项目目录加载 .env 文件
func (l *loader) loadDotEnv() error {
	var envLoaded bool
	var lastErr error

	if err := godotenv.Load(); err == nil {
		envLoaded = true
	} else {
		lastErr = err
	}

	for _, path := range l.opts.Paths {
		envPath := filepath.Join(path, ".env")
		if err := godotenv.Load(envPath); err == nil {
			envLoaded = true
		} else {
			lastErr = err
		}
	}

	if !envLoaded && lastErr != nil {
		return lastErr
	}
	return nil
}

// loadEnvironmentConfig 加载环境特定配置文件
func (l *loader) loadEnvironmentConfig() error {
	env := os.Getenv(fmt.Sprintf("%s_ENV", l.opts.EnvPrefix))
	if env == "" {
		return nil
	}

	originalName := l.opts.Name
	envConfigName := fmt.Sprintf("%s.%s", l.opts.Name, env)
	l.v.SetConfigName(envConfigName)

	if err := l.v.MergeInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return xerrors.Wrapf(err, "failed to merge environment config %s", envConfigName)
		}
		fmt.Printf("Info: no environment configuration file found for '%s'\n", env)
	} else {
		fmt.Printf("Info: loaded environment configuration '%s'\n", env)
	}

	l.v.SetConfigName(originalName)
	return nil
}

// captureCurrentValues 保存当前配置值用于变更检测
func (l *loader) captureCurrentValues() {
	l.mu.Lock()
	defer l.mu.Unlock()

	for key := range l.watches {
		l.oldValues[key] = l.v.Get(key)
	}
}

// Get 根据 key 获取配置值
func (l *loader) Get(key string) any {
	return l.v.Get(key)
}

// Unmarshal 将整个配置反序列化到结构体
func (l *loader) Unmarshal(v any) error {
	return l.v.Unmarshal(v)
}

// UnmarshalKey 将特定配置 key 反序列化到结构体
func (l *loader) UnmarshalKey(key string, v any) error {
	return l.v.UnmarshalKey(key, v)
}

// Watch 订阅特定配置 key 的变更
func (l *loader) Watch(ctx context.Context, key string) (<-chan Event, error) {
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
	if len(l.v.AllSettings()) == 0 {
		return xerrors.Wrapf(ErrValidationFailed, "configuration is empty")
	}
	return nil
}

// Start 启动后台任务
func (l *loader) Start(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.watchStarted {
		return nil
	}

	l.v.OnConfigChange(func(e fsnotify.Event) {
		if err := l.loadEnvironmentConfig(); err != nil {
			fmt.Printf("Error reloading environment config: %v\n", err)
		}
		l.notifyWatches(e)
	})

	l.v.WatchConfig()
	l.watchStarted = true

	return nil
}

// notifyWatches 通知所有监听者
func (l *loader) notifyWatches(_ fsnotify.Event) {
	l.mu.Lock()
	defer l.mu.Unlock()

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
					fmt.Printf("Warning: watch channel for key '%s' is full\n", key)
				}
			}
		}
	}
}

// Stop 停止后台任务
func (l *loader) Stop(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, channels := range l.watches {
		for _, ch := range channels {
			close(ch)
		}
	}
	l.watches = make(map[string][]chan Event)
	l.watchStarted = false
	return nil
}

// Phase 返回生命周期阶段
func (l *loader) Phase() int {
	return -10
}
