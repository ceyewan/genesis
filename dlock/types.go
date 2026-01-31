package dlock

import (
	"context"
	"time"

	"github.com/ceyewan/genesis/xerrors"
)

// DriverType 定义支持的后端类型
type DriverType string

const (
	DriverRedis DriverType = "redis"
	DriverEtcd  DriverType = "etcd"
)

// Config 组件静态配置
type Config struct {
	// Driver 选择使用的后端 (redis | etcd)
	Driver DriverType `json:"driver" yaml:"driver"`

	// Prefix 锁 Key 的全局前缀，例如 "dlock:"
	Prefix string `json:"prefix" yaml:"prefix"`

	// DefaultTTL 默认锁超时时间
	// Redis 会启动 Watchdog 自动续期；Etcd 使用 Session KeepAlive 自动续期。
	DefaultTTL time.Duration `json:"default_ttl" yaml:"default_ttl"`

	// RetryInterval 加锁重试间隔 (仅 Lock 模式有效)
	RetryInterval time.Duration `json:"retry_interval" yaml:"retry_interval"`
}

func (c *Config) setDefaults() {
	if c == nil {
		return
	}
	if c.DefaultTTL <= 0 {
		c.DefaultTTL = 10 * time.Second
	}
	if c.RetryInterval <= 0 {
		c.RetryInterval = 100 * time.Millisecond
	}
}

func (c *Config) validate() error {
	if c == nil {
		return ErrConfigNil
	}
	if c.Driver == "" {
		return xerrors.New("dlock: driver is required")
	}
	switch c.Driver {
	case DriverRedis, DriverEtcd:
		return nil
	default:
		return xerrors.New("dlock: unsupported driver: " + string(c.Driver))
	}
}

// Locker 定义了分布式锁的核心行为
type Locker interface {
	// Lock 阻塞式加锁
	// 成功返回 nil，失败返回错误
	// 如果上下文取消，返回 context.Canceled 或 context.DeadlineExceeded
	//
	// opts 支持的选项:
	//   - WithTTL(duration): 设置锁的超时时间
	Lock(ctx context.Context, key string, opts ...LockOption) error

	// TryLock 非阻塞式尝试加锁
	// 成功获取锁返回 true, nil
	// 锁已被占用返回 false, nil
	// 发生错误返回 false, err
	//
	// opts 支持的选项:
	//   - WithTTL(duration): 设置锁的超时时间
	TryLock(ctx context.Context, key string, opts ...LockOption) (bool, error)

	// Unlock 释放锁
	// 只有锁的持有者才能成功释放
	Unlock(ctx context.Context, key string) error

	// Close 关闭 Locker，释放底层资源
	// 对于 Etcd 会关闭 session，对于 Redis 是 no-op
	Close() error
}
