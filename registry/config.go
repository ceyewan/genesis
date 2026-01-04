package registry

import "time"

// Config Registry 组件配置
type Config struct {
	// Namespace Etcd Key 前缀，默认 "/genesis/services"
	Namespace string `yaml:"namespace" json:"namespace"`

	// DefaultTTL 默认服务注册租约时长，默认 30s
	DefaultTTL time.Duration `yaml:"default_ttl" json:"default_ttl"`

	// RetryInterval 重连/重试间隔，默认 1s
	RetryInterval time.Duration `yaml:"retry_interval" json:"retry_interval"`
}
