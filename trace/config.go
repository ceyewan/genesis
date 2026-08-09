package trace

import "time"

// BatcherMode 定义 Span 的异步导出批处理模式。
type BatcherMode string

const (
	// BatcherBatch 使用 OpenTelemetry 默认批处理参数。
	BatcherBatch BatcherMode = "batch"
	// BatcherImmediate 使用批量大小 1 和极短延迟异步导出。
	BatcherImmediate BatcherMode = "immediate"
)

// Config 定义全局 tracing 初始化参数。
//
// 当前实现是 OTLP gRPC 初始化器，支持统一 service resource 字段、异步错误通知
// 和导出超时；支持静态 OTLP gRPC 认证/路由头。
type Config struct {
	// ServiceName 是必填的 OTel service.name。
	ServiceName string `mapstructure:"service_name" json:"service_name" yaml:"service_name"`
	// Version 是 OTel service.version。
	Version string `mapstructure:"version" json:"version" yaml:"version"`
	// InstanceID 是 OTel service.instance.id。
	InstanceID string `mapstructure:"instance_id" json:"instance_id" yaml:"instance_id"`
	// Environment 是 OTel deployment.environment。
	Environment string `mapstructure:"environment" json:"environment" yaml:"environment"`
	// Endpoint 是 OTLP gRPC collector 地址。
	Endpoint string `mapstructure:"endpoint" json:"endpoint" yaml:"endpoint"`
	// Sampler 是 [0,1] 范围的父级感知采样比例。
	Sampler float64 `mapstructure:"sampler" json:"sampler" yaml:"sampler"`
	// Batcher 选择异步导出批处理模式。
	Batcher BatcherMode `mapstructure:"batcher" json:"batcher" yaml:"batcher"`
	// Insecure 为 true 时禁用 OTLP gRPC TLS。
	Insecure bool `mapstructure:"insecure" json:"insecure" yaml:"insecure"`
	// ExporterTimeout 是单次 OTLP 导出的超时。
	ExporterTimeout time.Duration `mapstructure:"exporter_timeout" json:"exporter_timeout" yaml:"exporter_timeout"`
	// Headers 是发送给 OTLP gRPC exporter 的静态认证/路由头；Init 会复制该 map。
	Headers map[string]string `mapstructure:"headers" json:"headers" yaml:"headers"`

	// ExportErrors 接收异步 OTLP 导出错误。发送是非阻塞的；调用方应提供有缓冲通道。
	ExportErrors chan<- error `mapstructure:"-" json:"-" yaml:"-"`
}

func (c *Config) setDefaults() {
	if c.ExporterTimeout == 0 {
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
		Batcher:         BatcherBatch,
		Insecure:        true,
		ExporterTimeout: 5 * time.Second,
	}
}
