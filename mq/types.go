package mq

import "github.com/ceyewan/genesis/xerrors"

// DriverType 驱动类型 (保留用于兼容性或配置解析)
type DriverType string

const (
	DriverNatsCore      DriverType = "nats_core"
	DriverNatsJetStream DriverType = "nats_jetstream"
	DriverRedis         DriverType = "redis"
)

// Config MQ 组件配置
// 主要用于配置驱动初始化；Driver 对应的连接器需通过 Option 注入。
type Config struct {
	// Driver 指定底层驱动: nats_core | nats_jetstream | redis
	Driver    DriverType       `json:"driver" yaml:"driver"`
	JetStream *JetStreamConfig `json:"jetstream" yaml:"jetstream"`
}

func (c *Config) setDefaults() {
	if c == nil {
		return
	}
}

func (c *Config) validate() error {
	if c == nil {
		return xerrors.New("config is nil")
	}
	if c.Driver == "" {
		return xerrors.New("driver is required")
	}
	switch c.Driver {
	case DriverNatsCore, DriverNatsJetStream, DriverRedis:
		return nil
	default:
		return xerrors.New("unsupported driver: " + string(c.Driver))
	}
}
