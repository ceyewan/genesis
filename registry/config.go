package registry

import (
	"time"

	"github.com/ceyewan/genesis/xerrors"
)

// Config Registry 组件配置
type Config struct {
	// Namespace Etcd Key 前缀，默认 "/genesis/services"
	Namespace string `yaml:"namespace" json:"namespace"`

	// DefaultTTL 默认服务注册租约时长，默认 30s
	DefaultTTL time.Duration `yaml:"default_ttl" json:"default_ttl"`

	// RetryInterval 重连/重试间隔，默认 1s
	RetryInterval time.Duration `yaml:"retry_interval" json:"retry_interval"`
}

// Validate 验证配置有效性
func (c *Config) validate() error {
	if c == nil {
		return nil // nil 配置使用默认值，在 New() 中处理
	}
	if c.DefaultTTL < 0 {
		return xerrors.New("registry: invalid default_ttl, must be non-negative")
	}
	if c.DefaultTTL > 0 && c.DefaultTTL < time.Second {
		return xerrors.New("registry: invalid default_ttl, must be >= 1s")
	}
	if c.RetryInterval < 0 {
		return xerrors.New("registry: invalid retry_interval, must be non-negative")
	}
	return nil
}
