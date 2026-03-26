package metrics

import (
	"strings"

	"github.com/ceyewan/genesis/xerrors"
)

// Config 定义全局 metrics 初始化参数。
//
// 当前实现采用 Prometheus exporter，并可选在同一进程内暴露 /metrics HTTP 端点。
type Config struct {
	ServiceName   string `mapstructure:"service_name"`
	Version       string `mapstructure:"version"`
	Port          int    `mapstructure:"port"`
	Path          string `mapstructure:"path"`
	EnableRuntime bool   `mapstructure:"enable_runtime"`
}

func (c *Config) validate() error {
	if c == nil {
		return xerrors.New("config is required")
	}
	if strings.TrimSpace(c.ServiceName) == "" {
		return xerrors.New("service_name is required")
	}
	if c.Port < 0 {
		return xerrors.New("port must be greater than or equal to 0")
	}
	if c.Path != "" && !strings.HasPrefix(c.Path, "/") {
		return xerrors.New("path must start with /")
	}
	return nil
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
