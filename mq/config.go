package mq

import (
	"time"

	"github.com/ceyewan/genesis/xerrors"
)

// Driver 驱动类型
type Driver string

const (
	// DriverNATSJetStream NATS JetStream 驱动（持久化，支持 Ack/Nak 重投）
	DriverNATSJetStream Driver = "nats_jetstream"

	// DriverRedisStream Redis Stream 驱动（持久化，Consumer Group）
	DriverRedisStream Driver = "redis_stream"
)

// Config MQ 配置
type Config struct {
	// Driver 底层驱动类型，必填
	// 可选值：nats_jetstream, redis_stream
	Driver Driver `json:"driver" yaml:"driver" mapstructure:"driver"`

	// JetStream JetStream 特有配置（仅 DriverNATSJetStream 时生效）
	JetStream *JetStreamConfig `json:"jetstream,omitempty" yaml:"jetstream,omitempty" mapstructure:"jetstream"`

	// RedisStream Redis Stream 特有配置（仅 DriverRedisStream 时生效）
	RedisStream *RedisStreamConfig `json:"redis_stream,omitempty" yaml:"redis_stream,omitempty" mapstructure:"redis_stream"`
}

// JetStreamConfig JetStream 特有配置
type JetStreamConfig struct {
	// AutoCreateStream 是否自动创建 Stream（如果不存在）
	// 生产环境建议关闭，通过运维手动创建并配置保留策略
	AutoCreateStream bool `json:"auto_create_stream" yaml:"auto_create_stream" mapstructure:"auto_create_stream"`

	// StreamPrefix Stream 名称前缀，默认 "S-"
	StreamPrefix string `json:"stream_prefix" yaml:"stream_prefix" mapstructure:"stream_prefix"`

	// AckWait 等待 Ack 的超时时间，超时后 JetStream 自动重投消息
	// 默认 30s，应设置为业务 Handler 预期最大处理时间的 2 倍
	AckWait time.Duration `json:"ack_wait" yaml:"ack_wait" mapstructure:"ack_wait"`
}

// RedisStreamConfig Redis Stream 特有配置
type RedisStreamConfig struct {
	// MaxLen Stream 最大长度，0 表示不限制，超过后自动裁剪旧消息
	MaxLen int64 `json:"max_len" yaml:"max_len" mapstructure:"max_len"`

	// Approximate 是否使用近似裁剪（MAXLEN ~），开启后性能更好但长度控制不精确
	Approximate bool `json:"approximate" yaml:"approximate" mapstructure:"approximate"`

	// PendingIdle 消息在 Pending 列表中的最大空闲时间，超过后可被其他消费者认领
	// 用于处理消费者崩溃导致消息卡住的场景
	// 默认 30s，应设置为业务 Handler 预期最大处理时间的 2 倍
	PendingIdle time.Duration `json:"pending_idle" yaml:"pending_idle" mapstructure:"pending_idle"`
}

// setDefaults 设置默认值
func (c *Config) setDefaults() {
	if c.JetStream == nil {
		c.JetStream = &JetStreamConfig{}
	}
	if c.JetStream.StreamPrefix == "" {
		c.JetStream.StreamPrefix = "S-"
	}
	if c.JetStream.AckWait == 0 {
		c.JetStream.AckWait = 30 * time.Second
	}

	if c.RedisStream == nil {
		c.RedisStream = &RedisStreamConfig{}
	}
	if c.RedisStream.PendingIdle == 0 {
		c.RedisStream.PendingIdle = 30 * time.Second
	}
}

// validate 验证配置
func (c *Config) validate() error {
	if c.Driver == "" {
		return xerrors.New("driver is required")
	}

	switch c.Driver {
	case DriverNATSJetStream, DriverRedisStream:
		return nil
	default:
		return xerrors.Wrapf(ErrInvalidConfig, "unsupported driver: %s", c.Driver)
	}
}
