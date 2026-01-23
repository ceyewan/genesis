package metrics

// Config 配置
type Config struct {
	ServiceName   string `mapstructure:"service_name"`
	Version       string `mapstructure:"version"`
	Port          int    `mapstructure:"port"`
	Path          string `mapstructure:"path"`
	EnableRuntime bool   `mapstructure:"enable_runtime"`
}

// NewDevDefaultConfig 开发环境默认配置
func NewDevDefaultConfig(serviceName string) *Config {
	return &Config{
		ServiceName:   serviceName,
		Version:       "dev",
		Port:          9090,
		Path:          "/metrics",
		EnableRuntime: false,
	}
}

// NewProdDefaultConfig 生产环境默认配置
func NewProdDefaultConfig(serviceName, version string) *Config {
	return &Config{
		ServiceName:   serviceName,
		Version:       version,
		Port:          9090,
		Path:          "/metrics",
		EnableRuntime: false,
	}
}
