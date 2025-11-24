package breaker

import (
	"fmt"

	internalbreaker "github.com/ceyewan/genesis/internal/breaker"
	"github.com/ceyewan/genesis/pkg/breaker/types"
	"github.com/ceyewan/genesis/pkg/clog"
)

// ========================================
// 类型导出 (Type Exports)
// ========================================

// Breaker 接口别名
type Breaker = types.Breaker

// Config 配置别名
type Config = types.Config

// Policy 策略别名
type Policy = types.Policy

// State 状态别名
type State = types.State

// 状态常量导出
const (
	StateClosed   = types.StateClosed
	StateOpen     = types.StateOpen
	StateHalfOpen = types.StateHalfOpen
)

// 错误导出
var (
	ErrOpenState       = types.ErrOpenState
	ErrTooManyRequests = types.ErrTooManyRequests
	ErrInvalidConfig   = types.ErrInvalidConfig
)

// ========================================
// 工厂函数 (Factory Functions)
// ========================================

// New 创建熔断器实例 (独立模式)
// 这是标准的工厂函数，支持在不依赖 Container 的情况下独立实例化
//
// 参数:
//   - cfg: 熔断器配置
//   - opts: 可选参数 (Logger, Meter, Tracer)
//
// 使用示例:
//
//	// 创建熔断器
//	b, _ := breaker.New(&breaker.Config{
//	    Default: breaker.Policy{
//	        FailureThreshold:    0.5,  // 50% 失败率触发熔断
//	        WindowSize:          100,
//	        MinRequests:         10,
//	        OpenTimeout:         30 * time.Second,
//	        HalfOpenMaxRequests: 3,
//	    },
//	    Services: map[string]breaker.Policy{
//	        "user.v1.UserService": {
//	            FailureThreshold: 0.3, // 用户服务更敏感
//	            WindowSize:       50,
//	        },
//	    },
//	}, breaker.WithLogger(logger))
func New(cfg *Config, opts ...Option) (Breaker, error) {
	// 使用默认配置
	if cfg == nil {
		cfg = types.DefaultConfig()
	}

	// 验证配置
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}

	// 应用选项
	opt := &Options{
		Logger: clog.Default(), // 默认 Logger
	}
	for _, o := range opts {
		o(opt)
	}

	// 派生 Logger (添加 "breaker" namespace)
	if opt.Logger != nil {
		opt.Logger = opt.Logger.With(clog.String("component", "breaker"))
	}

	return internalbreaker.New(cfg, opt.Logger, opt.Meter, opt.Tracer)
}

// validateConfig 验证配置
func validateConfig(cfg *Config) error {
	// 验证默认策略
	if err := validatePolicy(&cfg.Default); err != nil {
		return fmt.Errorf("invalid default policy: %w", err)
	}

	// 验证服务级策略
	for name, policy := range cfg.Services {
		if err := validatePolicy(&policy); err != nil {
			return fmt.Errorf("invalid policy for service %s: %w", name, err)
		}
	}

	return nil
}

// validatePolicy 验证策略
func validatePolicy(p *Policy) error {
	if p.FailureThreshold < 0 || p.FailureThreshold > 1 {
		return fmt.Errorf("failure threshold must be between 0 and 1, got %f", p.FailureThreshold)
	}
	if p.WindowSize <= 0 {
		return fmt.Errorf("window size must be positive, got %d", p.WindowSize)
	}
	if p.MinRequests < 0 {
		return fmt.Errorf("min requests must be non-negative, got %d", p.MinRequests)
	}
	if p.OpenTimeout <= 0 {
		return fmt.Errorf("open timeout must be positive, got %v", p.OpenTimeout)
	}
	if p.HalfOpenMaxRequests <= 0 {
		return fmt.Errorf("half open max requests must be positive, got %d", p.HalfOpenMaxRequests)
	}
	return nil
}
