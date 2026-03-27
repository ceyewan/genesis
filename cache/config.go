package cache

import (
	"time"

	"github.com/ceyewan/genesis/xerrors"
)

// DistributedDriverType 分布式缓存驱动类型。
type DistributedDriverType string

// LocalDriverType 本地缓存驱动类型。
type LocalDriverType string

const (
	// DriverRedis 表示 Redis 分布式缓存。
	DriverRedis DistributedDriverType = "redis"

	// DriverOtter 表示基于 otter 的本地缓存。
	DriverOtter LocalDriverType = "otter"
)

// DistributedConfig 分布式缓存配置。
type DistributedConfig struct {
	// Driver 后端类型，目前仅支持 redis。
	Driver DistributedDriverType `json:"driver" yaml:"driver"`

	// KeyPrefix 全局 Key 前缀。
	KeyPrefix string `json:"key_prefix" yaml:"key_prefix"`

	// Serializer 序列化器类型："json" | "msgpack"。
	Serializer string `json:"serializer" yaml:"serializer"`

	// DefaultTTL 默认 TTL，当 Set 或 Expire 传入 ttl<=0 时使用。默认 24 小时。
	DefaultTTL time.Duration `json:"default_ttl" yaml:"default_ttl"`
}

// LocalConfig 本地缓存配置。
type LocalConfig struct {
	// Driver 后端类型，目前仅支持 otter。
	Driver LocalDriverType `json:"driver" yaml:"driver"`

	// MaxEntries 缓存最大条目数。
	MaxEntries int `json:"max_entries" yaml:"max_entries"`

	// Serializer 序列化器类型："json" | "msgpack"。
	Serializer string `json:"serializer" yaml:"serializer"`

	// DefaultTTL 默认 TTL，当 Set 或 Expire 传入 ttl<=0 时使用。默认 1 小时。
	DefaultTTL time.Duration `json:"default_ttl" yaml:"default_ttl"`
}

// MultiConfig 多级缓存配置。
type MultiConfig struct {
	// LocalTTL 写入本地缓存时使用的 TTL。0 表示跟随写入 TTL。
	LocalTTL time.Duration `json:"local_ttl" yaml:"local_ttl"`

	// BackfillTTL 远程回填本地缓存时使用的 TTL，默认 1 分钟。
	BackfillTTL time.Duration `json:"backfill_ttl" yaml:"backfill_ttl"`

	// FailOpenOnLocalError 本地缓存异常时是否继续访问远程缓存。默认 true。
	// 使用指针以区分"未设置"（nil）和"显式设置为 false"。
	FailOpenOnLocalError *bool `json:"fail_open_on_local_error" yaml:"fail_open_on_local_error"`
}

func (c *DistributedConfig) setDefaults() {
	if c == nil {
		return
	}
	if c.Driver == "" {
		c.Driver = DriverRedis
	}
	if c.Serializer == "" {
		c.Serializer = "json"
	}
	if c.DefaultTTL <= 0 {
		c.DefaultTTL = 24 * time.Hour
	}
}

func (c *DistributedConfig) validate() error {
	if c == nil {
		return xerrors.New("cache: distributed config is nil")
	}
	switch c.Driver {
	case DriverRedis:
		return nil
	default:
		return xerrors.New("cache: unsupported distributed driver: " + string(c.Driver))
	}
}

func (c *LocalConfig) setDefaults() {
	if c == nil {
		return
	}
	if c.Driver == "" {
		c.Driver = DriverOtter
	}
	if c.MaxEntries <= 0 {
		c.MaxEntries = 10000
	}
	if c.Serializer == "" {
		c.Serializer = "json"
	}
	if c.DefaultTTL <= 0 {
		c.DefaultTTL = time.Hour
	}
}

func (c *LocalConfig) validate() error {
	if c == nil {
		return xerrors.New("cache: local config is nil")
	}
	switch c.Driver {
	case DriverOtter:
		return nil
	default:
		return xerrors.New("cache: unsupported local driver: " + string(c.Driver))
	}
}

func (c *MultiConfig) setDefaults() {
	if c == nil {
		return
	}
	if c.BackfillTTL <= 0 {
		c.BackfillTTL = time.Minute
	}
	// FailOpenOnLocalError 默认值为 true（通过指针 nil 表示）
	if c.FailOpenOnLocalError == nil {
		defaultFailOpen := true
		c.FailOpenOnLocalError = &defaultFailOpen
	}
}
