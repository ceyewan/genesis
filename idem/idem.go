// Package idem 提供结果复用型幂等组件，用于抑制同一请求或消息的重复成功提交。
//
// idem 在 Genesis 业务层中承担“去重和结果复用”职责。它的核心语义不是严格的
// exactly-once 执行，而是：
//   - 同一 key 的成功结果会被缓存，后续请求直接复用
//   - 同一 key 的并发执行会被锁保护，避免并发穿透
//   - 业务执行失败不会缓存结果，后续允许重试
//
// 当前组件提供四个入口：
//   - Execute：手动幂等执行，适合业务逻辑直接调用
//   - Consume：消息消费去重，只关心“是否已执行”
//   - GinMiddleware：HTTP 幂等中间件
//   - UnaryServerInterceptor：gRPC 一元服务端幂等拦截器
//
// 组件同时支持 Redis 和 Memory 两种后端。Redis 适合分布式环境，Memory 适合单机、
// 本地开发和测试。
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
	//   2. 如果 key 正在处理中 → 等待结果或重新尝试获取锁
	//   3. 如果 key 不存在 → 执行 fn 并缓存成功结果
	//
	// 参数：
	//   - ctx: 上下文，用于取消和超时控制
	//   - key: 幂等性键，全局唯一标识这次操作
	//   - fn: 业务逻辑函数，只在第一次请求时执行
	//
	// 返回：
	//   - 执行结果或缓存的结果。为保证首次执行与缓存命中的类型一致，返回值会经过同一套 JSON 编解码规范化。
	//   - 错误：ErrKeyEmpty、上下文错误、锁丢失错误等
	Execute(ctx context.Context, key string, fn func(ctx context.Context) (any, error)) (any, error)

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
	//   3. 如果未命中，执行 handler 并按缓存策略缓存响应
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
	//   传给 gin 的 router 时需要显式类型断言为 gin.HandlerFunc。
	GinMiddleware(opts ...MiddlewareOption) any

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
	//   4. 如果未命中，执行 RPC handler 并按缓存策略缓存成功响应
	//
	// 参数：
	//   - opts: 拦截器选项，可自定义 metadata 键名称等
	//
	// 返回：
	//   - gRPC 一元服务端拦截器
	//
	// 注意：
	//   只支持一元 RPC 调用，不支持流式 RPC（因为流式交互的复杂性）。
	//   当前默认只缓存成功的 proto.Message 响应。
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
