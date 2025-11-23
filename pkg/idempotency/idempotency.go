package idempotency

import (
	"github.com/ceyewan/genesis/internal/idempotency"
	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/connector"
	"github.com/ceyewan/genesis/pkg/idempotency/types"
)

// ========================================
// 类型导出 (Type Exports)
// ========================================

// Idempotent 接口别名
type Idempotent = types.Idempotent

// Config 配置别名
type Config = types.Config

// Status 状态别名
type Status = types.Status

// 状态常量导出
const (
	StatusProcessing = types.StatusProcessing
	StatusSuccess    = types.StatusSuccess
	StatusFailed     = types.StatusFailed
)

// DoOption Do 方法选项别名
type DoOption = types.DoOption

// WithTTL 导出
var WithTTL = types.WithTTL

// 错误导出
var (
	ErrProcessing = types.ErrProcessing
	ErrKeyEmpty   = types.ErrKeyEmpty
	ErrNotFound   = types.ErrNotFound
)

// ========================================
// 工厂函数 (Factory Functions)
// ========================================

// New 创建幂等组件实例 (独立模式)
// 这是标准的工厂函数，支持在不依赖 Container 的情况下独立实例化
//
// 参数:
//   - redisConn: Redis 连接器
//   - cfg: 幂等组件配置
//   - opts: 可选参数 (Logger, Meter, Tracer)
//
// 使用示例:
//
//	idem, _ := idempotency.New(redisConn, &idempotency.Config{
//	    Prefix: "myapp:idem:",
//	    DefaultTTL: 24 * time.Hour,
//	}, idempotency.WithLogger(logger))
func New(redisConn connector.RedisConnector, cfg *Config, opts ...Option) (Idempotent, error) {
	// 应用选项
	opt := Options{
		Logger: clog.Default(), // 默认 Logger
	}
	for _, o := range opts {
		o(&opt)
	}

	return idempotency.New(redisConn, cfg, opt.Logger, opt.Meter, opt.Tracer)
}

