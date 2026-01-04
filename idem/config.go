package idem

import (
	"time"

	"github.com/ceyewan/genesis/xerrors"
)

// DriverType 幂等组件驱动类型
type DriverType string

const (
	// DriverRedis 使用 Redis 作为后端
	DriverRedis DriverType = "redis"
)

// Config 幂等性组件配置
type Config struct {
	// Driver 后端类型: "redis" (默认 "redis")
	Driver DriverType `json:"driver" yaml:"driver"`

	// Prefix Redis Key 前缀，默认 "idem:"
	// 例如："myapp:idem:" 将使用 "myapp:idem:{key}" 作为存储键
	Prefix string `json:"prefix" yaml:"prefix"`

	// DefaultTTL 幂等记录有效期，默认 24h
	// 超过此时间后，缓存的结果将被清理，后续相同请求将重新执行
	DefaultTTL time.Duration `json:"default_ttl" yaml:"default_ttl"`

	// LockTTL 处理过程中的锁超时时间，默认 30s
	// 防止业务逻辑崩溃导致死锁，超时后锁自动释放
	LockTTL time.Duration `json:"lock_ttl" yaml:"lock_ttl"`
}

func (c *Config) setDefaults() {
	if c == nil {
		return
	}
	if c.Driver == "" {
		c.Driver = DriverRedis
	}
	if c.Prefix == "" {
		c.Prefix = "idem:"
	}
	if c.DefaultTTL <= 0 {
		c.DefaultTTL = 24 * time.Hour
	}
	if c.LockTTL <= 0 {
		c.LockTTL = 30 * time.Second
	}
}

func (c *Config) validate() error {
	if c == nil {
		return ErrConfigNil
	}
	switch c.Driver {
	case DriverRedis:
		return nil
	default:
		return xerrors.New("idem: unsupported driver: " + string(c.Driver))
	}
}
