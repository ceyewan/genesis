package metrics

import (
	"strings"

	"github.com/ceyewan/genesis/xerrors"
)

// Config 定义全局 metrics 初始化参数。
//
// 当前实现采用 Prometheus exporter，并可选在同一进程内暴露 /metrics HTTP 端点。
type Config struct {
	// ServiceName 是必填的 OTel service.name。
	ServiceName string `mapstructure:"service_name" json:"service_name" yaml:"service_name"`
	// Version 是 OTel service.version。
	Version string `mapstructure:"version" json:"version" yaml:"version"`
	// InstanceID 是 OTel service.instance.id。
	InstanceID string `mapstructure:"instance_id" json:"instance_id" yaml:"instance_id"`
	// Environment 是 OTel deployment.environment。
	Environment string `mapstructure:"environment" json:"environment" yaml:"environment"`
	// ListenAddress 是 Prometheus HTTP 端点的监听地址。
	// 空值保持历史行为（监听所有网卡）；开发默认配置使用 127.0.0.1。
	ListenAddress string `mapstructure:"listen_address" json:"listen_address" yaml:"listen_address"`
	// Port 是 Prometheus HTTP 端口；0 禁用 HTTP 暴露。
	Port int `mapstructure:"port" json:"port" yaml:"port"`
	// Path 是 Prometheus HTTP 路径；空值禁用 HTTP 暴露。
	Path string `mapstructure:"path" json:"path" yaml:"path"`
	// EnableRuntime 启用 Go runtime 指标采集。
	EnableRuntime bool `mapstructure:"enable_runtime" json:"enable_runtime" yaml:"enable_runtime"`
	// ServerErrors 接收 Prometheus HTTP Serve 的异步错误；发送非阻塞，建议使用缓冲通道。
	ServerErrors chan<- error `mapstructure:"-" json:"-" yaml:"-"`
}

func (c *Config) validate() error {
	if c == nil {
		return xerrors.Wrap(ErrInvalidConfig, "config is required")
	}
	if strings.TrimSpace(c.ServiceName) == "" {
		return xerrors.Wrap(ErrInvalidConfig, "service_name is required")
	}
	if c.Port < 0 {
		return xerrors.Wrap(ErrInvalidConfig, "port must be greater than or equal to 0")
	}
	if c.Path != "" && !strings.HasPrefix(c.Path, "/") {
		return xerrors.Wrap(ErrInvalidConfig, "path must start with /")
	}
	return nil
}

// NewDevDefaultConfig 开发环境默认配置
func NewDevDefaultConfig(serviceName string) *Config {
	return &Config{
		ServiceName:   serviceName,
		Version:       "dev",
		ListenAddress: "127.0.0.1",
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
		ListenAddress: "0.0.0.0",
		Port:          9090,
		Path:          "/metrics",
		EnableRuntime: false,
	}
}
