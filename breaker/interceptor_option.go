package breaker

import (
	"context"

	"google.golang.org/grpc"
)

// KeyFunc 从 gRPC 调用上下文中提取熔断 Key
type KeyFunc func(ctx context.Context, fullMethod string, cc *grpc.ClientConn) string

// FailureClassifier 判断错误是否应该计为失败
// 返回 true 表示该错误应该触发熔断计数
type FailureClassifier func(err error) bool

// InterceptorOption 拦截器选项函数类型
type InterceptorOption func(*interceptorConfig)

// interceptorConfig 拦截器内部配置（非导出）
type interceptorConfig struct {
	keyFunc           KeyFunc
	breakOnCreate     bool
	breakOnMessage    bool
	failureClassifier FailureClassifier
}

// WithKeyFunc 设置 Key 生成函数
func WithKeyFunc(fn KeyFunc) InterceptorOption {
	return func(cfg *interceptorConfig) {
		cfg.keyFunc = fn
	}
}

// WithBreakOnCreate 设置是否在流创建时进行熔断检查 (默认: true)
func WithBreakOnCreate(enable bool) InterceptorOption {
	return func(cfg *interceptorConfig) {
		cfg.breakOnCreate = enable
	}
}

// WithBreakOnMessage 设置是否在发送/接收消息时进行熔断检查 (默认: false)
// 开启此选项会对每个消息进行熔断检查，适合长连接场景，但会增加性能开销。
// 注意：如果 breakOnCreate 和 breakOnMessage 同时开启，可能会导致双重计数。
func WithBreakOnMessage(enable bool) InterceptorOption {
	return func(cfg *interceptorConfig) {
		cfg.breakOnMessage = enable
	}
}

// WithFailureClassifier 设置自定义错误分类器
// 用于决定哪些错误应该计为熔断失败。
// 默认行为：除了 io.EOF 外的所有 error 都计为失败。
func WithFailureClassifier(fn FailureClassifier) InterceptorOption {
	return func(cfg *interceptorConfig) {
		cfg.failureClassifier = fn
	}
}
