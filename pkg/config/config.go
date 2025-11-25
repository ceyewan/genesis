package config

// New 创建一个新的配置加载器实例
// 推荐使用此函数而非 NewLoader，以保持简洁的 API
func New(opts ...Option) (Loader, error) {
	return NewLoader(opts...)
}
