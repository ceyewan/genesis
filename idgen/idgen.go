// Package idgen 提供高性能的 ID 生成能力，支持多种 ID 生成策略：
//
//   - Snowflake: 基于雪花算法的分布式有序 ID 生成
//   - UUID: 标准 UUID 生成，支持 v4 和 v7 版本（默认 v7）
//   - Sequence: 基于 Redis 的分布式序列号生成器
//
// 设计原则:
//   - 简单易用: 提供简洁的静态方法和实例 API
//   - 高性能: 优化热路径，无锁或低锁竞争
//   - 解耦设计: 核心算法无外部依赖，扩展功能基于 Connector
//   - 实例优先: 支持多实例共存，全局单例作为便捷选项
//
// 基本使用:
//
//	// 静态模式 (最常用)
//	idgen.Setup(1) // 设置 workerID
//	id := idgen.Next()      // Snowflake ID
//	uid := idgen.NextUUID() // UUID v7
//
//	// 实例模式 (多实例场景)
//	sf, _ := idgen.NewSnowflake(1, idgen.WithDatacenterID(1))
//	id := sf.NextInt64()
package idgen

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/xerrors"
)

// ========================================
// 接口定义 (Interface Definitions)
// ========================================

// Generator 通用 ID 生成器接口
type Generator interface {
	// Next 返回字符串形式的 ID (UUID / Snowflake string)
	Next() string
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

// ========================================
// 全局静态 API (Global Static API)
// ========================================

var (
	globalSnowflake   *Snowflake
	globalSnowflakeMu sync.RWMutex
)

// Setup 初始化全局 Snowflake 单例
// 在使用 Next() 之前必须调用此方法
//
// 参数:
//   - workerID: 工作节点 ID [0, 1023]
//   - opts: 可选参数 (DatacenterID, Logger)
//
// 使用示例:
//
//	idgen.Setup(1) // 使用默认配置
//	idgen.Setup(100, idgen.WithDatacenterID(1)) // 带配置
func Setup(workerID int64, opts ...SnowflakeOption) error {
	globalSnowflakeMu.Lock()
	defer globalSnowflakeMu.Unlock()

	sf, err := NewSnowflake(workerID, opts...)
	if err != nil {
		return err
	}

	globalSnowflake = sf
	return nil
}

// Next 生成下一个 Snowflake ID (int64)
// 使用全局单例，必须先调用 Setup
//
// 使用示例:
//
//	idgen.Setup(1)
//	id := idgen.Next()
func Next() int64 {
	globalSnowflakeMu.RLock()
	defer globalSnowflakeMu.RUnlock()

	if globalSnowflake == nil {
		// 未初始化，使用默认 workerID=0
		globalSnowflakeMu.RUnlock()
		globalSnowflakeMu.Lock()
		if globalSnowflake == nil {
			globalSnowflake, _ = NewSnowflake(0)
		}
		globalSnowflakeMu.Unlock()
		globalSnowflakeMu.RLock()
	}

	return globalSnowflake.Next()
}

// NextUUID 生成 UUID (默认 v7)
// 这是一个无状态的便捷函数，无需预先初始化
//
// 使用示例:
//
//	uid := idgen.NextUUID() // UUID v7
func NextUUID() string {
	return NewUUIDV7()
}

// ========================================
// InstanceID 分配 (WorkerID Allocation)
// ========================================

// AssignInstanceID 基于 Redis 抢占分配唯一 WorkerID
// 用于在集群环境中自动分配唯一标识，避免手动配置冲突
//
// 参数:
//   - ctx: 上下文
//   - redis: Redis 连接器
//   - key: 租约键前缀 (如 "myapp:worker")
//   - maxID: 最大 ID 范围 [0, maxID)
//
// 返回:
//   - instanceID: 分配的实例 ID
//   - stop: 停止保活的函数，应在服务关闭时调用
//   - failCh: 保活失败通知通道 (如 Redis 连接断开)，调用者应监听此通道
//   - err: 错误信息
//
// 使用示例:
//
//	workerID, stop, failCh, err := idgen.AssignInstanceID(ctx, redisConn, "myapp", 1024)
//	if err != nil { ... }
//	defer stop()
//
//	go func() {
//	    if err := <-failCh; err != nil {
//	        // 处理保活失败 (e.g., 停止服务)
//	    }
//	}()
//
//	idgen.Setup(workerID)
//	id := idgen.Next()
func AssignInstanceID(ctx context.Context, redis connector.RedisConnector, key string, maxID int) (instanceID int64, stop func(), failCh <-chan error, err error) {
	if redis == nil {
		return 0, nil, nil, xerrors.WithCode(ErrConnectorNil, "redis_connector_nil")
	}
	if maxID <= 0 || maxID > 1024 {
		return 0, nil, nil, xerrors.WithCode(ErrInvalidInput, "max_id_out_of_range")
	}
	if key == "" {
		key = "genesis:idgen:worker"
	}

	client := redis.GetClient()

	// Lua 脚本：原子分配 WorkerID
	script := `
		local prefix = KEYS[1]
		local value = ARGV[1]
		local ttl = tonumber(ARGV[2])
		local max_id = tonumber(ARGV[3])
		for i = 0, max_id - 1 do
			local key = prefix .. ":" .. i
			if redis.call("SET", key, value, "NX", "EX", ttl) then
				return i
			end
		end
		return -1
	`

	ttl := 30 // 默认 30 秒 TTL
	value := fmt.Sprintf("host:%d", time.Now().UnixNano())
	result, err := client.Eval(ctx, script, []string{key}, value, ttl, maxID).Result()
	if err != nil {
		return 0, nil, nil, xerrors.Wrap(err, "redis_eval_failed")
	}

	id, ok := result.(int64)
	if !ok || id < 0 {
		return 0, nil, nil, xerrors.WithCode(ErrWorkerIDExhausted, "no_available_worker_id")
	}

	// 启动保活
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	redisKey := fmt.Sprintf("%s:%d", key, id)

	go func() {
		ticker := time.NewTicker(time.Duration(ttl/3) * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := client.Expire(context.Background(), redisKey, time.Duration(ttl)*time.Second).Err(); err != nil {
					select {
					case errCh <- xerrors.Wrap(err, "keep_alive_failed"):
					default:
					}
					return
				}
			}
		}
	}()

	stop = func() {
		cancel()
		// 释放租约
		client.Del(context.Background(), redisKey)
	}

	return id, stop, errCh, nil
}

// ========================================
// Sequence 静态便捷方法
// ========================================

// NextSequence 基于 Redis 生成简单的序列号
// 这是一个便捷函数，使用默认配置 (Step=1, 无 TTL, 无 Prefix)
// 适用于临时或简单的计数需求
//
// 参数:
//   - ctx: 上下文
//   - redis: Redis 连接器
//   - key: 序列号键名
//
// 使用示例:
//
//	id, _ := idgen.NextSequence(ctx, redisConn, "counter")
func NextSequence(ctx context.Context, redis connector.RedisConnector, key string) (int64, error) {
	if redis == nil {
		return 0, xerrors.WithCode(ErrConnectorNil, "redis_connector_nil")
	}
	if key == "" {
		return 0, xerrors.WithCode(ErrInvalidInput, "key_is_empty")
	}

	client := redis.GetClient()
	result, err := client.Incr(ctx, key).Result()
	if err != nil {
		return 0, xerrors.Wrap(err, "redis_incr_failed")
	}

	return result, nil
}

// ========================================
// 配置定义 (Configuration)
// ========================================

// SequenceConfig 序列号生成器配置
type SequenceConfig struct {
	// KeyPrefix 键前缀
	KeyPrefix string `yaml:"key_prefix" json:"key_prefix"`

	// Step 步长，默认为 1
	Step int64 `yaml:"step" json:"step"`

	// MaxValue 最大值限制，达到后循环（0 表示不限制）
	MaxValue int64 `yaml:"max_value" json:"max_value"`

	// TTL Redis 键过期时间，0 表示永不过期
	TTL int64 `yaml:"ttl" json:"ttl"`
}
