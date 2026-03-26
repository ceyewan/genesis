// Package config 为 Genesis 提供统一的多源配置加载与文件驱动的变更通知能力。
//
// 这个组件基于 Viper 实现，但对外收敛成更稳定的 Loader 契约，用来统一处理
// 配置文件、环境变量、.env 文件和环境特定配置之间的关系。它面向微服务和组件库场景，
// 重点解决三类问题：
//
//   - 多源配置的统一加载与覆盖顺序
//   - config.yaml 与 config.{env}.yaml 的合并
//   - 按 key 订阅配置文件变化，而不是让业务代码直接面对 fsnotify
//
// 当前优先级从高到低为：
//
//   - 进程环境变量
//   - .env 文件
//   - 环境特定配置文件，例如 config.dev.yaml
//   - 基础配置文件，例如 config.yaml
//
// 其中 .env 的语义是“补齐缺失项”：只有当前进程中不存在同名环境变量时，才会从
// .env 注入值。这比“无条件覆盖环境变量”更符合常见实践，也更容易解释部署时的最终结果。
//
// 热更新当前只覆盖配置文件本身：
//
//   - Load 负责加载配置，不会自动启动 watcher
//   - Watch 只能在成功 Load 后调用；第一次调用 Watch 时才会启动内部文件监听
//   - 只监听基础配置文件和环境特定配置文件
//   - 不监听 .env 文件，也不监听运行时环境变量变化
//   - 热更新时如果读取或校验失败，不推送变更事件
//   - 如需记录热更新失败原因，可通过 WithLogger 注入日志器
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

// Loader 定义配置加载器的核心行为。
// 它负责加载、读取、反序列化和监听配置变化。
type Loader interface {
	// Load 加载配置并初始化内部状态。
	//
	// Load 可以重复调用。每次调用都会基于当前 Config 重新创建内部 Viper 状态，
	// 并重新读取基础配置、环境配置和环境变量；.env 也会重新处理，但只补齐当前
	// 仍然缺失的环境变量。
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
	//   - 调用 Watch 前必须先成功执行 Load
	//   - 无论调用多少次 Watch，内部只启动一个文件监听 goroutine（sync.Once 保证）
	//   - 返回的 channel 缓冲区大小为 10，若消费者处理过慢可能丢失事件（非阻塞发送）
	//   - 监听基础配置文件和环境特定配置文件（如 config.yaml 和 config.dev.yaml）
	//   - .env 文件变更不会触发通知
	//   - 热更新时若配置文件读取失败，不会推送变更事件，也不会返回错误
	//   - 该方法的 Load 前置检查用于快速失败，不负责等待并发中的 Load 完成
	Watch(ctx context.Context, key string) (<-chan Event, error)

	// Validate 验证当前配置的有效性
	Validate() error
}

// Event 配置变更事件
type Event struct {
	Key       string      // 配置 key
	Value     any         // 新值
	OldValue  any         // 旧值
	Source    EventSource // 事件来源
	Timestamp time.Time
}

// EventSource 表示配置变更事件的来源。
type EventSource string

const (
	// EventSourceFile 表示事件来自配置文件变化。
	EventSourceFile EventSource = "file"
)
