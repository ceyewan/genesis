package types

import "time"

// Mode 限流模式
type Mode string

const (
	// ModeStandalone 单机模式 (基于内存)
	ModeStandalone Mode = "standalone"

	// ModeDistributed 分布式模式 (基于 Redis)
	ModeDistributed Mode = "distributed"
)

// Config 限流组件配置
type Config struct {
	// Mode 限流模式: standalone | distributed
	Mode Mode `yaml:"mode" json:"mode"`

	// Standalone 单机模式配置
	Standalone StandaloneConfig `yaml:"standalone" json:"standalone"`

	// Distributed 分布式模式配置
	Distributed DistributedConfig `yaml:"distributed" json:"distributed"`
}

// StandaloneConfig 单机模式配置
type StandaloneConfig struct {
	// CleanupInterval 清理过期限流器的间隔
	// 默认: 1m
	CleanupInterval time.Duration `yaml:"cleanup_interval" json:"cleanup_interval"`

	// IdleTimeout 限流器空闲超时时间
	// 超过此时间未使用的限流器将被清理
	// 默认: 5m
	IdleTimeout time.Duration `yaml:"idle_timeout" json:"idle_timeout"`
}

// DistributedConfig 分布式模式配置
type DistributedConfig struct {
	// Prefix Redis Key 前缀
	// 默认: "ratelimit:"
	Prefix string `yaml:"prefix" json:"prefix"`
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		Mode: ModeStandalone,
		Standalone: StandaloneConfig{
			CleanupInterval: 1 * time.Minute,
			IdleTimeout:     5 * time.Minute,
		},
		Distributed: DistributedConfig{
			Prefix: "ratelimit:",
		},
	}
}

