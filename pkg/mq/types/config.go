package types

// DriverType 驱动类型
type DriverType string

const (
	DriverNatsCore      DriverType = "nats_core"
	DriverNatsJetStream DriverType = "nats_jetstream"
)

// Config MQ 组件配置
type Config struct {
	// 驱动类型
	Driver DriverType `json:"driver" yaml:"driver"`

	// JetStream 特有配置 (仅当 Driver 为 nats_jetstream 时有效)
	JetStream *JetStreamConfig `json:"jetstream" yaml:"jetstream"`
}

// JetStreamConfig JetStream 特有配置
type JetStreamConfig struct {
	// 是否自动创建 Stream (如果不存在)
	AutoCreateStream bool `json:"auto_create_stream" yaml:"auto_create_stream"`

	// 默认的 Stream 配置 (用于自动创建)
	// 简单起见，可以约定 Subject 前缀映射到 Stream Name
	// 例如: "orders.>" -> Stream "ORDERS"
}
