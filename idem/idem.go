// Package idem 提供了幂等性组件，用于确保在分布式环境中操作的"一次且仅一次"执行。
//
// idem 是 Genesis 业务层的核心组件，它提供了：
// - 统一的 Idempotency 接口，支持手动调用、Gin 中间件、gRPC 拦截器
// - 结果缓存：自动缓存执行结果，重复请求直接返回缓存数据
// - 并发控制：内置分布式锁机制，防止同一幂等键的并发穿透
// - 后端可配置：支持 Redis / Memory
// - 与 L0 基础组件（日志）的深度集成
//
// ## 基本使用
//
//	// 创建幂等组件
//	idem, _ := idem.New(&idem.Config{
//	    Driver:     idem.DriverRedis,
//	    Prefix:     "myapp:idem:",
//	    DefaultTTL: 24 * time.Hour,
//	}, idem.WithRedisConnector(redisConn), idem.WithLogger(logger))
//
//	// 执行幂等操作
//	result, err := idem.Execute(ctx, "order:create:12345", func(ctx context.Context) (interface{}, error) {
//	    // 业务逻辑
//	    return map[string]interface{}{"order_id": "12345"}, nil
//	})
//
// ## Gin 中间件
//
//	r := gin.Default()
//	r.POST("/orders", idem.GinMiddleware(), func(c *gin.Context) {
//	    c.JSON(200, gin.H{"order_id": "123"})
//	})
//
// ## gRPC 拦截器
//
//	s := grpc.NewServer(
//	    grpc.UnaryInterceptor(idem.UnaryServerInterceptor()),
//	)
package idem

import (
	"context"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/xerrors"

	"google.golang.org/grpc"
)

// ========================================
// 接口定义 (Interface Definitions)
// ========================================

// Idempotency 幂等性组件核心接口
//
// 支持三种使用方式：
// 1. Execute: 手动调用，适合业务层直接使用
// 2. GinMiddleware: Gin 框架中间件，自动处理 HTTP 请求幂等性
// 3. UnaryServerInterceptor: gRPC 一元拦截器，处理单次 RPC 调用幂等性
type Idempotency interface {
	// Execute 执行幂等操作
	//
	// 工作流程：
	//   1. 如果 key 已存在且完成 → 直接返回缓存结果
	//   2. 如果 key 正在处理中 → 返回 ErrConcurrentRequest
	//   3. 如果 key 不存在 → 执行 fn 并缓存结果
	//
	// 参数：
	//   - ctx: 上下文，用于取消和超时控制
	//   - key: 幂等性键，全局唯一标识这次操作
	//   - fn: 业务逻辑函数，只在第一次请求时执行
	//
	// 返回：
	//   - 执行结果或缓存的结果
	//   - 错误：ErrKeyEmpty, ErrConcurrentRequest 等
	Execute(ctx context.Context, key string, fn func(ctx context.Context) (interface{}, error)) (interface{}, error)

	// Consume 用于消息消费的幂等处理
	//
	// 工作流程：
	//   1. 如果 key 已存在且完成 → 直接返回 false
	//   2. 如果 key 正在处理中 → 返回 ErrConcurrentRequest
	//   3. 如果 key 不存在 → 执行 fn 并标记已处理
	//
	// 返回：
	//   - executed: 是否执行了 fn
	//   - 错误：ErrKeyEmpty, ErrConcurrentRequest 等
	Consume(ctx context.Context, key string, ttl time.Duration, fn func(ctx context.Context) error) (executed bool, err error)

	// GinMiddleware 创建 Gin 框架中间件
	//
	// 使用示例：
	//   middleware := idem.GinMiddleware().(func(*gin.Context))
	//   router.POST("/orders", middleware, handler)
	//   // 或者直接使用（Gin 会自动处理）：
	//   router.Use(idem.GinMiddleware().(func(*gin.Context)))
	//
	// 工作原理：
	//   1. 从 HTTP 请求头 X-Idempotency-Key 提取幂等性键
	//   2. 如果缓存命中，直接返回缓存的响应
	//   3. 如果未命中，执行 handler 并缓存响应
	//
	// 参数：
	//   - opts: 中间件选项，可自定义请求头名称等
	//
	// 返回：
	//   - func(*gin.Context) 类型的中间件函数
	//
	// 注意：
	//   返回类型为 interface{} 是为了避免强依赖 gin 包，
	//   实际返回的是 func(*gin.Context) 类型。
	//   可以直接传给 gin 的 router 使用，不需要显式类型断言。
	GinMiddleware(opts ...MiddlewareOption) interface{}

	// UnaryServerInterceptor 创建 gRPC 一元服务端拦截器
	//
	// 使用示例：
	//   server := grpc.NewServer(
	//       grpc.UnaryInterceptor(idem.UnaryServerInterceptor()),
	//   )
	//
	// 工作原理：
	//   1. 从 gRPC metadata 提取 x-idem-key
	//   2. 使用分布式锁防止并发执行
	//   3. 如果缓存命中，返回缓存的 protobuf 响应
	//   4. 如果未命中，执行 RPC handler 并缓存结果
	//
	// 参数：
	//   - opts: 拦截器选项，可自定义 metadata 键名称等
	//
	// 返回：
	//   - gRPC 一元服务端拦截器
	//
	// 注意：
	//   只支持一元 RPC 调用，不支持流式 RPC（因为流式交互的复杂性）。
	UnaryServerInterceptor(opts ...InterceptorOption) grpc.UnaryServerInterceptor
}

// ========================================
// 工厂函数 (Factory Functions)
// ========================================

// New 创建幂等性组件实例
//
// 这是标准的工厂函数，支持配置驱动和显式依赖注入。
//
// 参数：
//   - cfg: 幂等性配置，不可为 nil
//   - opts: 可选配置，如 WithLogger(), WithRedisConnector()
//
// 返回：
//   - Idempotency 组件实例
//   - 错误：缺少必要连接器或配置非法
//
// 使用示例：
//
//	idem, err := idem.New(&idem.Config{
//	    Driver:     idem.DriverRedis,
//	    Prefix:     "myapp:idem:",
//	    DefaultTTL: 24 * time.Hour,
//	    LockTTL:    30 * time.Second,
//	}, idem.WithRedisConnector(redisConn), idem.WithLogger(logger))
func New(cfg *Config, opts ...Option) (Idempotency, error) {
	if cfg == nil {
		return nil, ErrConfigNil
	}

	cfg.setDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	// 应用选项
	opt := options{}
	for _, o := range opts {
		o(&opt)
	}

	// 派生 Logger（添加 component 字段）
	logger := opt.logger
	if logger != nil {
		logger = logger.With(clog.String("component", "idem"))
	}

	switch cfg.Driver {
	case DriverRedis:
		if opt.redisConn == nil {
			return nil, xerrors.New("idem: redis connector is required, use WithRedisConnector")
		}
		if logger != nil {
			logger.Info("creating idem component",
				clog.String("driver", string(cfg.Driver)),
				clog.String("prefix", cfg.Prefix),
				clog.Duration("default_ttl", cfg.DefaultTTL),
				clog.Duration("lock_ttl", cfg.LockTTL))
		}
		return newIdempotency(cfg, newRedisStore(opt.redisConn, cfg.Prefix), logger), nil
	case DriverMemory:
		if logger != nil {
			logger.Info("creating idem component",
				clog.String("driver", string(cfg.Driver)),
				clog.String("prefix", cfg.Prefix),
				clog.Duration("default_ttl", cfg.DefaultTTL),
				clog.Duration("lock_ttl", cfg.LockTTL))
		}
		return newIdempotency(cfg, newMemoryStore(cfg.Prefix), logger), nil
	default:
		return nil, xerrors.New("idem: unsupported driver: " + string(cfg.Driver))
	}
}
