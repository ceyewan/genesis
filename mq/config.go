package mq

// DriverType 驱动类型 (保留用于兼容性或配置解析)
type DriverType string

const (
	DriverNatsCore      DriverType = "nats_core"
	DriverNatsJetStream DriverType = "nats_jetstream"
	DriverRedis         DriverType = "redis"
)

// Config MQ 组件配置
type Config struct {
	// 驱动类型 (建议使用 mq.NewNatsDriver 等工厂函数直接指定)
	Driver DriverType `json:"driver" yaml:"driver"`

	// JetStream 特有配置 (仅当 Driver 为 nats_jetstream 时有效)
	JetStream *JetStreamConfig `json:"jetstream" yaml:"jetstream"`

	// Redis 配置 (预留)
	// Redis *RedisConfig `json:"redis" yaml:"redis"`
}

// JetStreamConfig JetStream 特有配置
type JetStreamConfig struct {
	// 是否自动创建 Stream (如果不存在)
	AutoCreateStream bool `json:"auto_create_stream" yaml:"auto_create_stream"`

	// 默认的 Stream 配置 (用于自动创建)
	// 简单起见，可以约定 Subject 前缀映射到 Stream Name
	// 例如: "orders.>" -> Stream "ORDERS"
}
