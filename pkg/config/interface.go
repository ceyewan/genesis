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

	// Watch 监听配置变化，通过 context 取消监听
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
