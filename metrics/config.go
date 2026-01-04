package metrics

// Config 指标系统配置
type Config struct {
	// ServiceName 服务名称，用于标识指标的来源
	ServiceName string `mapstructure:"service_name"`

	// Version 服务版本
	Version string `mapstructure:"version"`

	// Port Prometheus HTTP 服务器端口
	Port int `mapstructure:"port"`

	// Path Prometheus 指标的 HTTP 路径
	Path string `mapstructure:"path"`

	// EnableRuntime 是否启用 Go Runtime 指标采集
	EnableRuntime bool `mapstructure:"enable_runtime"`
}

// NewDevDefaultConfig 创建开发环境默认配置
func NewDevDefaultConfig(serviceName string) *Config {
	return &Config{
		ServiceName:   serviceName,
		Version:       "dev",
		Port:          9090,
		Path:          "/metrics",
		EnableRuntime: false,
	}
}

// NewProdDefaultConfig 创建生产环境默认配置
func NewProdDefaultConfig(serviceName, version string) *Config {
	return &Config{
		ServiceName:   serviceName,
		Version:       version,
		Port:          9090,
		Path:          "/metrics",
		EnableRuntime: false,
	}
}
