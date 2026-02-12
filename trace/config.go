package trace

// Config 配置
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
