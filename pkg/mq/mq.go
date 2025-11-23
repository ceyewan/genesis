package mq

import (
	"github.com/ceyewan/genesis/internal/mq"
	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/connector"
	"github.com/ceyewan/genesis/pkg/mq/types"
)

// 导出 types 包中的定义，方便用户使用

type Client = types.Client
type Message = types.Message
type Subscription = types.Subscription
type Handler = types.Handler
type Config = types.Config
type JetStreamConfig = types.JetStreamConfig
type DriverType = types.DriverType

const (
	DriverNatsCore      = types.DriverNatsCore
	DriverNatsJetStream = types.DriverNatsJetStream
)

// New 创建 MQ 客户端 (独立模式)
// 这是标准的工厂函数，支持在不依赖 Container 的情况下独立实例化
//
// 参数:
//   - conn: NATS 连接器
//   - cfg: MQ 配置
//   - opts: 可选参数 (Logger, Meter, Tracer)
//
// 使用示例:
//
//	natsConn, _ := connector.NewNATS(natsConfig)
//	mqClient, _ := mq.New(natsConn, &mq.Config{
//	    Driver: mq.DriverNatsCore,
//	}, mq.WithLogger(logger))
func New(conn connector.NATSConnector, cfg *Config, opts ...Option) (Client, error) {
	// 应用选项
	opt := Options{
		Logger: clog.Default(), // 默认 Logger
	}
	for _, o := range opts {
		o(&opt)
	}

	return mq.New(conn, cfg, opt.Logger, opt.Meter, opt.Tracer)
}
