package registry

import "time"

// Config Registry 组件配置
type Config struct {
	// Namespace Etcd Key 前缀，默认 "/genesis/services"
	Namespace string `yaml:"namespace" json:"namespace"`

	// Schema 注册到 gRPC resolver 的 schema，默认 "etcd"
	Schema string `yaml:"schema" json:"schema"`

	// DefaultTTL 默认服务注册租约时长，默认 30s
	DefaultTTL time.Duration `yaml:"default_ttl" json:"default_ttl"`

	// RetryInterval 重连/重试间隔，默认 1s
	RetryInterval time.Duration `yaml:"retry_interval" json:"retry_interval"`

	// EnableCache 是否启用本地服务发现缓存，默认 true
	EnableCache bool `yaml:"enable_cache" json:"enable_cache"`

	// CacheExpiration 本地缓存过期时间，默认 10s
	CacheExpiration time.Duration `yaml:"cache_expiration" json:"cache_expiration"`
}
