package types

import "time"

// BackendType 定义支持的后端类型
type BackendType string

const (
	BackendRedis BackendType = "redis"
	BackendEtcd  BackendType = "etcd"
)

// Config 组件静态配置
type Config struct {
	// Backend 选择使用的后端 (redis | etcd)
	Backend BackendType `json:"backend" yaml:"backend"`

	// Prefix 锁 Key 的全局前缀，例如 "dlock:"
	Prefix string `json:"prefix" yaml:"prefix"`

	// DefaultTTL 默认锁超时时间
	DefaultTTL time.Duration `json:"default_ttl" yaml:"default_ttl"`

	// RetryInterval 加锁重试间隔 (仅 Lock 模式有效)
	RetryInterval time.Duration `json:"retry_interval" yaml:"retry_interval"`
}
