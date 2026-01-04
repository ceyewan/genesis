package cache

import "github.com/ceyewan/genesis/xerrors"

// DriverType 缓存驱动类型
type DriverType string

const (
	DriverRedis  DriverType = "redis"
	DriverMemory DriverType = "memory"
)

// Config 缓存组件统一配置
type Config struct {
	// Driver 缓存驱动: "redis" | "memory" (默认 "redis")
	Driver DriverType `json:"driver" yaml:"driver"`

	// Prefix: 全局 Key 前缀 (e.g., "app:v1:")
	Prefix string `json:"prefix" yaml:"prefix"`

	// Serializer: "json" | "msgpack"
	Serializer string `json:"serializer" yaml:"serializer"`

	// Standalone 单机缓存配置
	Standalone *StandaloneConfig `json:"standalone" yaml:"standalone"`
}

// StandaloneConfig 单机缓存配置
type StandaloneConfig struct {
	// Capacity 缓存最大容量（条目数，默认：10000）
	Capacity int `json:"capacity" yaml:"capacity"`
}

func (c *Config) setDefaults() {
	if c == nil {
		return
	}
	if c.Driver == "" {
		c.Driver = DriverRedis
	}
	if c.Serializer == "" {
		c.Serializer = "json"
	}
	if c.Driver == DriverMemory {
		if c.Standalone == nil {
			c.Standalone = &StandaloneConfig{}
		}
		if c.Standalone.Capacity <= 0 {
			c.Standalone.Capacity = 10000
		}
	}
}

func (c *Config) validate() error {
	if c == nil {
		return xerrors.New("config is nil")
	}
	switch c.Driver {
	case DriverRedis, DriverMemory:
		return nil
	default:
		return xerrors.New("unsupported driver: " + string(c.Driver))
	}
}
