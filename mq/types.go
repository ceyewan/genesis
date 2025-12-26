package mq

// DriverType 驱动类型 (保留用于兼容性或配置解析)
type DriverType string

const (
	DriverNatsCore      DriverType = "nats_core"
	DriverNatsJetStream DriverType = "nats_jetstream"
	DriverRedis         DriverType = "redis"
	DriverKafka         DriverType = "kafka"
)

// Config MQ 组件配置
// 主要用于用户通过配置加载驱动，Genesis 核心库内部不强制使用此结构
type Config struct {
	Driver    DriverType       `json:"driver" yaml:"driver"`
	JetStream *JetStreamConfig `json:"jetstream" yaml:"jetstream"`
}
