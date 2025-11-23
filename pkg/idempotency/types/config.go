package types

import "time"

// Config 幂等组件配置
type Config struct {
	// Prefix Key 前缀，默认 "idempotency:"
	Prefix string `yaml:"prefix" json:"prefix"`

	// DefaultTTL 默认记录保留时间，默认 24h
	DefaultTTL time.Duration `yaml:"default_ttl" json:"default_ttl"`

	// ProcessingTTL 处理中状态的 TTL，默认 5m
	// 防止业务逻辑执行时间过长导致锁一直占用
	ProcessingTTL time.Duration `yaml:"processing_ttl" json:"processing_ttl"`
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		Prefix:        "idempotency:",
		DefaultTTL:    24 * time.Hour,
		ProcessingTTL: 5 * time.Minute,
	}
}

