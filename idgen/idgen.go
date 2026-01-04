// Package idgen 提供高性能的 ID 生成能力，支持多种 ID 生成策略：
//
//   - Generator: 基于雪花算法的分布式有序 ID 生成接口
//   - UUID: UUID v7 生成（时间排序，适合数据库主键）
//   - Sequencer: 基于 Redis/Etcd 的分布式序列号生成器接口
//   - Allocator: 支持 Redis/Etcd 的 WorkerID 自动分配器
//
// 设计原则:
//   - 配置驱动: 统一使用 New(cfg, opts...) 模式
//   - 接口优先: 面向接口编程，便于切换底层实现
//   - 高性能: 优化热路径，无锁或低锁竞争
//   - 实例优先: 支持多实例共存
//
// 基本使用:
//
//	// Generator (Snowflake)
//	gen, _ := idgen.NewGenerator(&idgen.GeneratorConfig{WorkerID: 1})
//	id := gen.Next()
//
//	// UUID
//	uid := idgen.UUID()
//
//	// Sequencer
//	seq, _ := idgen.NewSequencer(&idgen.SequencerConfig{
//	    KeyPrefix: "order:",
//	    Step:      1,
//	}, idgen.WithRedisConnector(redisConn))
//	nextID, _ := seq.Next(ctx, "user:1")
//
//	// Allocator (自动分配 WorkerID)
//	allocator, _ := idgen.NewAllocator(&idgen.AllocatorConfig{
//	    Driver: "redis",
//	    MaxID:  512,
//	}, idgen.WithRedisConnector(redisConn))
//	workerID, _ := allocator.Allocate(ctx)
//	defer allocator.Stop()
//	go func() { if err := <-allocator.KeepAlive(ctx); err != nil { ... } }()
package idgen

import "context"

// ========================================
// 接口定义 (Interface Definitions)
// ========================================

// Generator ID 生成器接口
// 提供高性能的分布式 ID 生成能力
type Generator interface {
	// Next 生成下一个 ID
	Next() int64

	// NextString 生成下一个 ID (字符串形式)
	NextString() string
}

// Sequencer 序列号生成器接口
// 提供基于 Redis/Etcd 的分布式序列号生成能力
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
