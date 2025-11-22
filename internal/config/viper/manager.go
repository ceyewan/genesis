package viper

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/ceyewan/genesis/pkg/config/types"
	"github.com/fsnotify/fsnotify"
	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

// viperManager 实现 config.Manager 接口
type viperManager struct {
	v            *viper.Viper
	opts         *types.Options
	mu           sync.RWMutex
	watches      map[string][]chan types.Event
	oldValues    map[string]interface{} // 存储旧值，用于变更比较
	watchStarted bool                   // 避免重复启动监听
}

// NewManager 创建一个新的 Viper 配置管理器
func NewManager(opts *types.Options) (types.Manager, error) {
	if opts == nil {
		opts = types.DefaultOptions()
	}

	v := viper.New()
	return &viperManager{
		v:         v,
		opts:      opts,
		watches:   make(map[string][]chan types.Event),
		oldValues: make(map[string]interface{}),
	}, nil
}

// Load 初始化并从所有来源加载配置
func (m *viperManager) Load(ctx context.Context) error {
	// 1. 尝试加载 .env 文件
	if err := m.loadDotEnv(); err != nil {
		// 只记录错误，不终止流程 - .env 文件是可选的
		fmt.Printf("Warning: failed to load .env file: %v\n", err)
	}

	// 2. 配置 Viper
	m.v.SetConfigName(m.opts.Name)
	m.v.SetConfigType(m.opts.FileType)

	for _, path := range m.opts.Paths {
		m.v.AddConfigPath(path)
	}

	// 3. 环境变量设置
	m.v.SetEnvPrefix(m.opts.EnvPrefix)
	m.v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	m.v.AutomaticEnv()

	// 4. 加载基础配置
	if err := m.v.ReadInConfig(); err != nil {
		// 如果是配置文件未找到错误，且我们允许仅使用环境变量，则可以忽略
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return &types.Error{
				Type:    types.ErrorFormatInvalid,
				Key:     m.opts.Name,
				Message: "failed to read config file",
				Cause:   err,
			}
		}
		fmt.Printf("Warning: no configuration file found at %s\n", m.v.ConfigFileUsed())
	}

	// 5. 加载环境特定配置 (e.g., config.dev.yaml)
	if err := m.loadEnvironmentConfig(); err != nil {
		return err
	}

	// 6. 执行配置验证
	if err := m.Validate(); err != nil {
		return err
	}

	// 7. 保存当前配置值作为基线
	m.captureCurrentValues()

	return nil
}

// loadDotEnv 尝试从项目目录加载 .env 文件
func (m *viperManager) loadDotEnv() error {
	// 尝试从各个路径加载 .env 文件
	var envLoaded bool
	var lastErr error

	// 首先尝试项目根目录
	if err := godotenv.Load(); err == nil {
		envLoaded = true
	} else {
		lastErr = err
	}

	// 然后尝试指定的配置路径
	for _, path := range m.opts.Paths {
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
func (m *viperManager) loadEnvironmentConfig() error {
	// 获取当前环境
	env := os.Getenv(fmt.Sprintf("%s_ENV", m.opts.EnvPrefix))
	if env == "" {
		// 没有指定环境，使用默认配置
		return nil
	}

	// 保存当前配置名
	originalName := m.opts.Name

	// 设置环境特定配置名
	envConfigName := fmt.Sprintf("%s.%s", m.opts.Name, env)
	m.v.SetConfigName(envConfigName)

	// 尝试加载环境配置并合并
	if err := m.v.MergeInConfig(); err != nil {
		// 环境特定配置文件是可选的
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return &types.Error{
				Type:    types.ErrorFormatInvalid,
				Key:     envConfigName,
				Message: "failed to merge environment config",
				Cause:   err,
			}
		}
		fmt.Printf("Info: no environment configuration file found for '%s'\n", env)
	} else {
		fmt.Printf("Info: loaded environment configuration '%s'\n", env)
	}

	// 恢复原始配置名，以便后续热更新正常工作
	m.v.SetConfigName(originalName)
	return nil
}

// captureCurrentValues 保存当前配置值用于变更检测
func (m *viperManager) captureCurrentValues() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 保存当前所有被监视 key 的值
	for key := range m.watches {
		m.oldValues[key] = m.v.Get(key)
	}
}

// Get 根据 key 获取配置值
func (m *viperManager) Get(key string) any {
	return m.v.Get(key)
}

// Unmarshal 将整个配置反序列化到结构体
func (m *viperManager) Unmarshal(v any) error {
	return m.v.Unmarshal(v)
}

// UnmarshalKey 将特定配置 key 反序列化到结构体
func (m *viperManager) UnmarshalKey(key string, v any) error {
	return m.v.UnmarshalKey(key, v)
}

// Watch 订阅特定配置 key 的变更
func (m *viperManager) Watch(ctx context.Context, key string) (<-chan types.Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 创建带缓冲的通道避免阻塞
	ch := make(chan types.Event, 10)
	m.watches[key] = append(m.watches[key], ch)

	// 存储当前值用于变更检测
	m.oldValues[key] = m.v.Get(key)

	// 在 context 结束时清理
	go func() {
		<-ctx.Done()
		m.removeWatch(key, ch)
	}()

	return ch, nil
}

// removeWatch 从注册表中移除监听通道
func (m *viperManager) removeWatch(key string, ch chan types.Event) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if chans, ok := m.watches[key]; ok {
		for i, c := range chans {
			if c == ch {
				// 移除此通道
				m.watches[key] = append(chans[:i], chans[i+1:]...)
				break
			}
		}
		// 如果此 key 没有更多通道，移除 key
		if len(m.watches[key]) == 0 {
			delete(m.watches, key)
			delete(m.oldValues, key)
		}
	}
	close(ch)
}

// Validate 执行配置基本验证
func (m *viperManager) Validate() error {
	// 基本验证 - 可以根据具体需求扩展
	if len(m.v.AllSettings()) == 0 {
		// 只有当确实没有加载到任何配置时才报错
		// 但考虑到可能只用环境变量，这里可以放宽，或者检查是否至少有一个 source
		// 暂时保持原样，但使用 types.Error
		return &types.Error{
			Type:    types.ErrorValidationFailed,
			Message: "configuration is empty",
		}
	}
	return nil
}

// Start 初始化后台进程（如配置监听）
func (m *viperManager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 避免多次启动监听
	if m.watchStarted {
		return nil
	}

	// 配置文件监听
	m.v.OnConfigChange(func(e fsnotify.Event) {
		// 当基础配置变化时重新加载任何环境特定配置
		if err := m.loadEnvironmentConfig(); err != nil {
			fmt.Printf("Error reloading environment config: %v\n", err)
		}

		// 通知所有监听者配置变更
		m.notifyWatches(e)
	})

	m.v.WatchConfig()
	m.watchStarted = true

	return nil
}

// notifyWatches 通知所有已注册的监听者配置变更
func (m *viperManager) notifyWatches(e fsnotify.Event) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 通知所有监听者关于其特定 key 的变更
	for key, channels := range m.watches {
		// 获取新值
		newValue := m.v.Get(key)
		oldValue := m.oldValues[key]

		// 检查值是否真的变化了
		if !reflect.DeepEqual(oldValue, newValue) {
			// 创建事件
			event := types.Event{
				Key:       key,
				Value:     newValue,
				OldValue:  oldValue,
				Source:    "file",
				Timestamp: time.Now(),
			}

			// 更新存储的旧值
			m.oldValues[key] = newValue

			// 通知此 key 的所有监听者
			for _, ch := range channels {
				select {
				case ch <- event:
					// 事件成功发送
				default:
					// 通道已满，记录但不阻塞
					fmt.Printf("Warning: watch channel for key '%s' is full, dropping event\n", key)
				}
			}
		}
	}
}

// Stop 优雅地终止后台进程
func (m *viperManager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 关闭所有监听通道
	for key, channels := range m.watches {
		for _, ch := range channels {
			close(ch)
		}
		delete(m.watches, key)
	}

	m.watchStarted = false
	return nil
}

// Phase 返回用于容器排序的初始化阶段
func (m *viperManager) Phase() int {
	// 配置应该是最先初始化的组件之一
	// 设置为负数确保它在阶段 >= 0 的组件之前启动
	return -10
}
