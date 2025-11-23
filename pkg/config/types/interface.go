package types

import (
	"context"
	"time"

	"github.com/ceyewan/genesis/pkg/container"
)

// Manager 定义配置管理器的核心行为
// 它同时实现了 container.Lifecycle 接口，以便被容器管理（用于启动 Watcher 等后台任务）
type Manager interface {
	container.Lifecycle // Start, Stop, Phase

	// Load 加载配置（通常在 Start 之前调用，用于 Bootstrap）
	Load(ctx context.Context) error

	// Get 获取原始配置值
	Get(key string) any

	// Unmarshal 将配置解析到结构体
	Unmarshal(v any) error

	// UnmarshalKey 将指定 Key 的配置解析到结构体
	UnmarshalKey(key string, v any) error

	// Watch 监听配置变化
	// 返回一个只读 channel，当配置发生变化时发送事件
	// 可以通过 context 取消监听
	Watch(ctx context.Context, key string) (<-chan Event, error)

	// Validate 验证当前配置的有效性
	Validate() error
}

// Event 配置变更事件
type Event struct {
	Key       string
	Value     any
	OldValue  any    // 旧值 (如果支持)
	Source    string // "file", "env", "remote"
	Timestamp time.Time
}
