// Package idgen 提供 Genesis 的 ID 生成能力。
//
// 这个组件覆盖四类能力：
//
//   - Generator: 本地 Snowflake 风格 64bit ID 生成器
//   - UUID: UUID v7 字符串生成
//   - Sequencer: 基于 Redis 的按键递增序列号
//   - Allocator: 基于 Redis/Etcd 的 WorkerID 自动分配器
//
// idgen 更像“多种 ID 能力的组合组件”，而不是单一算法封装。不同能力面向的问题不同：
//
//   - 需要趋势递增、紧凑整数主键时使用 Generator
//   - 需要跨系统字符串唯一标识时使用 UUID
//   - 需要同一业务键下严格递增时使用 Sequencer
//   - 需要为多个实例自动分配 WorkerID 时使用 Allocator
//
// Generator 当前支持两种位布局模式：
//
//   - single_dc: 41bit 时间戳、10bit worker、12bit sequence
//   - multi_dc: 41bit 时间戳、5bit datacenter、5bit worker、12bit sequence
//
// 时间字段使用固定自定义 epoch 2024-01-01T00:00:00Z，调用 Next 或 NextString 时会显式返回错误，
// 以便调用方在时钟回拨等异常情况下做出停机、告警或重试决策。
//
// Sequencer 当前只支持 Redis。Allocator 支持 Redis 和 Etcd，其中 KeepAlive 会启动后台保活并返回错误通道，
// Stop 负责释放租约资源，且实现应被视为幂等清理方法。
package idgen

import "context"

// ========================================
// 接口定义 (Interface Definitions)
// ========================================

// Generator ID 生成器接口
// 提供高性能的分布式 ID 生成能力
type Generator interface {
	// Next 生成下一个 ID
	Next() (int64, error)

	// NextString 生成下一个 ID (字符串形式)
	NextString() (string, error)
}

// Sequencer 序列号生成器接口
// 提供基于 Redis 的分布式序列号生成能力
type Sequencer interface {
	// Next 为指定键生成下一个序列号
	Next(ctx context.Context, key string) (int64, error)

	// NextBatch 为指定键批量生成序列号
	NextBatch(ctx context.Context, key string, count int) ([]int64, error)

	// Set 直接设置序列号的值
	// 警告：此操作会覆盖现有值，请谨慎使用
	Set(ctx context.Context, key string, value int64) error

	// SetIfNotExists 仅当键不存在时设置序列号的值
	// 返回 true 表示设置成功，false 表示键已存在
	SetIfNotExists(ctx context.Context, key string, value int64) (bool, error)
}
