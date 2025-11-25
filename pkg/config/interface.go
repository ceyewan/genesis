package config

import (
	"context"
	"time"
)

// Loader 定义配置加载器的核心行为
// 职责：加载、解析和监听配置变化
type Loader interface {
	// Load 加载配置（Bootstrap 阶段调用，在 Start 之前）
	Load(ctx context.Context) error

	// Get 获取原始配置值
	Get(key string) any

	// Unmarshal 将整个配置反序列化到结构体
	Unmarshal(v any) error

	// UnmarshalKey 将指定 Key 的配置反序列化到结构体
	UnmarshalKey(key string, v any) error

	// Watch 监听配置变化，通过 context 取消监听
	Watch(ctx context.Context, key string) (<-chan Event, error)

	// Validate 验证当前配置的有效性
	Validate() error

	// Start 启动后台任务（如文件监听）
	Start(ctx context.Context) error

	// Stop 停止后台任务
	Stop(ctx context.Context) error

	// Phase 返回生命周期阶段（用于容器排序启动顺序）
	Phase() int
}

// Event 配置变更事件
type Event struct {
	Key       string // 配置 key
	Value     any    // 新值
	OldValue  any    // 旧值
	Source    string // "file" | "env" | "remote"
	Timestamp time.Time
}
