package ratelimit

import (
	"fmt"

	"github.com/ceyewan/genesis/internal/ratelimit"
	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/connector"
	"github.com/ceyewan/genesis/pkg/ratelimit/types"
)

// ========================================
// 类型导出 (Type Exports)
// ========================================

// Limiter 接口别名
type Limiter = types.Limiter

// Config 配置别名
type Config = types.Config

// Limit 限流规则别名
type Limit = types.Limit

// Mode 模式别名
type Mode = types.Mode

// 模式常量导出
const (
	ModeStandalone  = types.ModeStandalone
	ModeDistributed = types.ModeDistributed
)

// 错误导出
var (
	ErrNotSupported = types.ErrNotSupported
	ErrKeyEmpty     = types.ErrKeyEmpty
	ErrInvalidLimit = types.ErrInvalidLimit
)

// ========================================
// 工厂函数 (Factory Functions)
// ========================================

// New 创建限流组件实例 (独立模式)
// 这是标准的工厂函数，支持在不依赖 Container 的情况下独立实例化
//
// 参数:
//   - cfg: 限流组件配置
//   - redisConn: Redis 连接器 (仅分布式模式需要)
//   - opts: 可选参数 (Logger, Meter, Tracer)
//
// 使用示例:
//
//	// 单机模式
//	limiter, _ := ratelimit.New(&ratelimit.Config{
//	    Mode: ratelimit.ModeStandalone,
//	}, nil, ratelimit.WithLogger(logger))
//
//	// 分布式模式
//	limiter, _ := ratelimit.New(&ratelimit.Config{
//	    Mode: ratelimit.ModeDistributed,
//	}, redisConn, ratelimit.WithLogger(logger))
func New(cfg *Config, redisConn connector.RedisConnector, opts ...Option) (Limiter, error) {
	// 使用默认配置
	if cfg == nil {
		cfg = types.DefaultConfig()
	}

	// 验证配置
	if cfg.Mode == types.ModeDistributed && redisConn == nil {
		return nil, fmt.Errorf("redis connector is required for distributed mode")
	}

	// 应用选项
	opt := Options{
		Logger: clog.Default(), // 默认 Logger
	}
	for _, o := range opts {
		o(&opt)
	}

	return ratelimit.New(cfg, redisConn, opt.Logger, opt.Meter, opt.Tracer)
}

