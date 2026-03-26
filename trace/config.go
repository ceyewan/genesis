package trace

// Config 定义全局 tracing 初始化参数。
//
// 当前实现是一个最小 OTLP gRPC 初始化器，不包含 TLS、认证头和附加 resource
// 属性等更复杂的 exporter 配置能力。
type Config struct {
	ServiceName string  `mapstructure:"service_name"`
	Endpoint    string  `mapstructure:"endpoint"`
	Sampler     float64 `mapstructure:"sampler"`
	Batcher     string  `mapstructure:"batcher"`
	Insecure    bool    `mapstructure:"insecure"`
}

// DefaultConfig 返回默认配置
func DefaultConfig(serviceName string) *Config {
	return &Config{
		ServiceName: serviceName,
		Endpoint:    "localhost:4317",
		Sampler:     1.0,
		Batcher:     "batch",
		Insecure:    true,
	}
}
