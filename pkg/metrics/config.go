package metrics

// Config 指标系统的配置
type Config struct {
	Enabled     bool   `mapstructure:"enabled"`      // 是否启用 Metrics
	ServiceName string `mapstructure:"service_name"` // 服务名称
	Version     string `mapstructure:"version"`      // 服务版本
	Port        int    `mapstructure:"port"`         // Prometheus 暴露端口（默认 9090）
	Path        string `mapstructure:"path"`         // Prometheus 指标路径（默认 /metrics）
}
