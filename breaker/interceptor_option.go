package breaker

// InterceptorOption 拦截器选项函数类型
type InterceptorOption func(*interceptorConfig)

// interceptorConfig 拦截器内部配置（非导出）
type interceptorConfig struct {
	keyFunc KeyFunc
}

// WithKeyFunc 设置 Key 生成函数
func WithKeyFunc(fn KeyFunc) InterceptorOption {
	return func(cfg *interceptorConfig) {
		cfg.keyFunc = fn
	}
}

// WithServiceLevelKey 使用服务级别 Key（默认）
func WithServiceLevelKey() InterceptorOption {
	return WithKeyFunc(ServiceLevelKey())
}

// WithBackendLevelKey 使用后端级别 Key
// 推荐用于负载均衡场景，实现后端级别的熔断隔离
func WithBackendLevelKey() InterceptorOption {
	return WithKeyFunc(BackendLevelKey())
}

// WithMethodLevelKey 使用方法级别 Key
func WithMethodLevelKey() InterceptorOption {
	return WithKeyFunc(MethodLevelKey())
}

// WithCompositeKey 使用组合 Key（服务 + 后端）
func WithCompositeKey() InterceptorOption {
	return WithKeyFunc(CompositeKey(ServiceLevelKey(), BackendLevelKey()))
}
