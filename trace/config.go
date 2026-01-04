package trace

// Config 是 Trace 组件的配置结构
type Config struct {
	// ServiceName 服务名称，用于在 Trace 系统中标识服务
	ServiceName string `mapstructure:"service_name"`

	// Endpoint OTLP gRPC 收集器地址 (例如: "tempo:4317" 或 "jaeger:4317")
	Endpoint string `mapstructure:"endpoint"`

	// Sampler 采样率 (0.0 - 1.0)
	// 1.0 表示全量采集，0.0 表示不采集
	// 推荐使用 1.0 (Tempo) 或 0.1 (Jaeger)
	Sampler float64 `mapstructure:"sampler"`

	// Batcher 上报策略: "batch" (批量) 或 "simple" (实时)
	// 生产环境推荐 "batch"
	Batcher string `mapstructure:"batcher"`

	// Insecure 是否使用非安全连接 (true for HTTP/NoTLS)
	Insecure bool `mapstructure:"insecure"`
}

// DefaultConfig 返回默认配置
func DefaultConfig(serviceName string) *Config {
	return &Config{
		ServiceName: serviceName,
		Endpoint:    "localhost:4317",
		Sampler:     1.0, // 默认全量采样 (适合开发环境和 Tempo)
		Batcher:     "batch",
		Insecure:    true,
	}
}
