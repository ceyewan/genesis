// Package config 为 Genesis 框架提供统一的配置管理能力。
// 支持多源配置加载、热更新和配置验证，基于 Viper 实现。
//
// 特性：
//   - 多源配置加载：YAML/JSON 文件、环境变量、.env 文件
//   - 配置优先级：环境变量 > .env > 环境特定配置 > 基础配置
//   - 热更新支持：实时监听配置文件变化，自动通知应用
//   - 接口优先设计：基于接口的 API，隐藏实现细节
//
// 基本使用：
//
//	// 一步创建并加载配置
//	loader := config.MustLoad(
//		config.WithConfigName("config"),
//		config.WithConfigPaths("./config"),
//		config.WithEnvPrefix("GENESIS"),
//	)
//
//	var cfg AppConfig
//	if err := loader.Unmarshal(&cfg); err != nil {
//		panic(err)
//	}
//
//	// 获取单个字段
//	appName := loader.Get("app.name").(string)
//
//	// 监听配置变化
//	ch, _ := loader.Watch(context.Background(), "app.debug")
//	for event := range ch {
//		fmt.Printf("配置变化: %s = %v\n", event.Key, event.Value)
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
