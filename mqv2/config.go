package mqv2

import "github.com/ceyewan/genesis/xerrors"

// Driver 驱动类型
type Driver string

const (
	// DriverNATSCore NATS Core 驱动（高性能，无持久化）
	DriverNATSCore Driver = "nats_core"

	// DriverNATSJetStream NATS JetStream 驱动（持久化，支持重投）
	DriverNATSJetStream Driver = "nats_jetstream"

	// DriverRedisStream Redis Stream 驱动（持久化队列）
	DriverRedisStream Driver = "redis_stream"

	// DriverKafka Kafka 驱动（预留，未实现）
	DriverKafka Driver = "kafka"
)

// Config MQ 配置
type Config struct {
	// Driver 底层驱动类型
	// 必填，可选值：nats_core, nats_jetstream, redis_stream
	Driver Driver `json:"driver" yaml:"driver"`

	// JetStream JetStream 特有配置（仅 DriverNATSJetStream 时生效）
	JetStream *JetStreamConfig `json:"jetstream,omitempty" yaml:"jetstream,omitempty"`

	// RedisStream Redis Stream 特有配置（仅 DriverRedisStream 时生效）
	RedisStream *RedisStreamConfig `json:"redis_stream,omitempty" yaml:"redis_stream,omitempty"`
}

// JetStreamConfig JetStream 特有配置
type JetStreamConfig struct {
	// AutoCreateStream 是否自动创建 Stream（如果不存在）
	// 生产环境建议关闭，通过运维手动创建
	AutoCreateStream bool `json:"auto_create_stream" yaml:"auto_create_stream"`

	// StreamPrefix Stream 名称前缀，默认 "S-"
	StreamPrefix string `json:"stream_prefix" yaml:"stream_prefix"`
}

// RedisStreamConfig Redis Stream 特有配置
type RedisStreamConfig struct {
	// MaxLen Stream 最大长度，0 表示不限制
	// 超过后自动裁剪旧消息
	MaxLen int64 `json:"max_len" yaml:"max_len"`

	// Approximate 是否使用近似裁剪（MAXLEN ~）
	// 开启后性能更好，但长度控制不精确
	Approximate bool `json:"approximate" yaml:"approximate"`
}

// setDefaults 设置默认值
func (c *Config) setDefaults() {
	if c.JetStream != nil && c.JetStream.StreamPrefix == "" {
		c.JetStream.StreamPrefix = "S-"
	}
}

// validate 验证配置
func (c *Config) validate() error {
	if c.Driver == "" {
		return xerrors.New("driver is required")
	}

	switch c.Driver {
	case DriverNATSCore, DriverNATSJetStream, DriverRedisStream:
		return nil
	default:
		return xerrors.WithCode(xerrors.New("unsupported driver"), string(c.Driver))
	}
}
