package mq

import (
	"time"

	"github.com/ceyewan/genesis/xerrors"
)

// Driver 驱动类型
type Driver string

type StreamRetention string

type StreamStorage string

const (
	// DriverNATSJetStream NATS JetStream 驱动（持久化，支持 Ack/Nak 重投）
	DriverNATSJetStream Driver = "nats_jetstream"

	// DriverRedisStream Redis Stream 驱动（持久化，Consumer Group）
	DriverRedisStream Driver = "redis_stream"

	StreamRetentionLimits    StreamRetention = "limits"
	StreamRetentionInterest  StreamRetention = "interest"
	StreamRetentionWorkQueue StreamRetention = "work_queue"

	StreamStorageFile   StreamStorage = "file"
	StreamStorageMemory StreamStorage = "memory"
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

	// MaxDeliver 是单条消息的最大投递次数，默认 5。达到上限后由应用根据
	// JetStream advisory/DLQ 策略处理，Genesis 不定义业务死信主题。
	MaxDeliver int `json:"max_deliver" yaml:"max_deliver" mapstructure:"max_deliver"`

	// 以下字段仅用于 Genesis 自动创建的新 Stream。已存在 Stream 的运维配置会被保留。
	Retention StreamRetention `json:"retention" yaml:"retention" mapstructure:"retention"`
	Storage   StreamStorage   `json:"storage" yaml:"storage" mapstructure:"storage"`
	MaxAge    time.Duration   `json:"max_age" yaml:"max_age" mapstructure:"max_age"`
	MaxBytes  int64           `json:"max_bytes" yaml:"max_bytes" mapstructure:"max_bytes"`
	Replicas  int             `json:"replicas" yaml:"replicas" mapstructure:"replicas"`
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
	if c.JetStream.MaxDeliver == 0 {
		c.JetStream.MaxDeliver = 5
	}
	if c.JetStream.Retention == "" {
		c.JetStream.Retention = StreamRetentionLimits
	}
	if c.JetStream.Storage == "" {
		c.JetStream.Storage = StreamStorageFile
	}
	if c.JetStream.Replicas == 0 {
		c.JetStream.Replicas = 1
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
		return xerrors.Wrap(ErrInvalidConfig, "driver is required")
	}

	switch c.Driver {
	case DriverNATSJetStream:
		if c.JetStream == nil {
			return nil
		}
		if c.JetStream.AckWait < 0 || c.JetStream.MaxDeliver < 1 || c.JetStream.MaxAge < 0 || c.JetStream.MaxBytes < 0 {
			return xerrors.Wrap(ErrInvalidConfig, "jetstream durations, limits, and max_deliver must be positive")
		}
		if c.JetStream.Replicas < 1 || c.JetStream.Replicas > 5 {
			return xerrors.Wrap(ErrInvalidConfig, "jetstream replicas must be between 1 and 5")
		}
		switch c.JetStream.Retention {
		case StreamRetentionLimits, StreamRetentionInterest, StreamRetentionWorkQueue:
		default:
			return xerrors.Wrapf(ErrInvalidConfig, "unsupported jetstream retention: %s", c.JetStream.Retention)
		}
		switch c.JetStream.Storage {
		case StreamStorageFile, StreamStorageMemory:
			return nil
		default:
			return xerrors.Wrapf(ErrInvalidConfig, "unsupported jetstream storage: %s", c.JetStream.Storage)
		}
	case DriverRedisStream:
		if c.RedisStream == nil {
			return nil
		}
		if c.RedisStream.MaxLen < 0 || c.RedisStream.PendingIdle < 0 {
			return xerrors.Wrap(ErrInvalidConfig, "redis stream max_len and pending_idle must not be negative")
		}
		return nil
	default:
		return xerrors.Wrapf(ErrInvalidConfig, "unsupported driver: %s", c.Driver)
	}
}
