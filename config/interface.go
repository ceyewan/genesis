// Package config 为 Genesis 提供统一的配置加载与变更通知能力，基于 Viper 实现。
//
// 特性：
//   - 多源配置：YAML/JSON 文件、环境变量、.env 文件
//   - 优先级语义（从高到低）：进程环境变量 > .env 文件 > 环境配置 > 基础配置
//   - 环境配置合并：支持 config.{env}.yaml 合并到基础配置
//   - 文件变更通知：监听配置文件变化并向订阅者推送变更事件（带防抖）
//
// 注意事项：
//   - .env 文件会覆盖同名环境变量（godotenv.Load 默认行为），若需"只补齐缺失项"语义，
//     请在启动前自行设置环境变量，Viper 的 AutomaticEnv 会确保运行时环境变量优先
//   - 热更新时若配置文件读取失败，会静默忽略错误以避免中断服务，但不会推送变更事件
//
// 基本使用：
//
//	loader, err := config.New(&config.Config{
//		Name:      "config",
//		Paths:     []string{"./config"},
//		FileType:  "yaml",
//		EnvPrefix: "GENESIS",
//	})
//	if err != nil {
//		panic(err)
//	}
//
//	if err := loader.Load(context.Background()); err != nil {
//		panic(err)
//	}
//
//	var cfg AppConfig
//	if err := loader.Unmarshal(&cfg); err != nil {
//		panic(err)
//	}
//
// 监听特定 Key 的变化：
//
//	ctx, cancel := context.WithCancel(context.Background())
//	defer cancel()
//
//	ch, _ := loader.Watch(ctx, "app.debug")
//	for event := range ch {
//		_ = event
//	}
package config

import (
	"context"
	"time"
)

// Loader 定义配置加载器的核心行为
// 职责：加载、解析和监听配置变化
type Loader interface {
	// Load 加载配置并初始化内部状态
	Load(ctx context.Context) error

	// Get 获取原始配置值
	Get(key string) any

	// Unmarshal 将整个配置反序列化到结构体
	Unmarshal(v any) error

	// UnmarshalKey 将指定 Key 的配置反序列化到结构体
	UnmarshalKey(key string, v any) error

	// Watch 监听配置变化，通过 context 取消监听。
	//
	// 实现细节：
	//   - 无论调用多少次 Watch，内部只启动一个文件监听 goroutine（sync.Once 保证）
	//   - 返回的 channel 缓冲区大小为 10，若消费者处理过慢可能丢失事件（非阻塞发送）
	//   - 监听基础配置文件和环境特定配置文件（如 config.yaml 和 config.dev.yaml）
	//   - .env 文件变更不会触发通知
	//   - 热更新时若配置文件读取失败，不会推送变更事件，也不会返回错误
	Watch(ctx context.Context, key string) (<-chan Event, error)

	// Validate 验证当前配置的有效性
	Validate() error
}

// Event 配置变更事件
type Event struct {
	Key       string // 配置 key
	Value     any    // 新值
	OldValue  any    // 旧值
	Source    string // "file" | "env" | "remote"
	Timestamp time.Time
}
