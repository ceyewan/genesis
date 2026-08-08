package trace

import "time"

// Config 定义全局 tracing 初始化参数。
//
// 当前实现是 OTLP gRPC 初始化器，支持统一 service resource 字段、异步错误通知
// 和导出超时；不包含认证头等更复杂的 exporter 配置能力。
type Config struct {
	ServiceName     string        `mapstructure:"service_name"`
	Version         string        `mapstructure:"version"`
	InstanceID      string        `mapstructure:"instance_id"`
	Environment     string        `mapstructure:"environment"`
	Endpoint        string        `mapstructure:"endpoint"`
	Sampler         float64       `mapstructure:"sampler"`
	Batcher         string        `mapstructure:"batcher"`
	Insecure        bool          `mapstructure:"insecure"`
	ExporterTimeout time.Duration `mapstructure:"exporter_timeout"`

	// ExportErrors 接收异步 OTLP 导出错误。发送是非阻塞的；调用方应提供有缓冲通道。
	ExportErrors chan<- error `mapstructure:"-" json:"-" yaml:"-"`
}

func (c *Config) setDefaults() {
	if c.ExporterTimeout <= 0 {
		c.ExporterTimeout = 5 * time.Second
	}
}

// DefaultConfig 返回默认配置
func DefaultConfig(serviceName string) *Config {
	return &Config{
		ServiceName:     serviceName,
		Version:         "dev",
		Endpoint:        "localhost:4317",
		Sampler:         1.0,
		Batcher:         "batch",
		Insecure:        true,
		ExporterTimeout: 5 * time.Second,
	}
}
