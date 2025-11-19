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

// New 创建 MQ 客户端
// 这是对外暴露的工厂方法，实际上是调用 internal/mq 的实现
func New(conn connector.NATSConnector, cfg *types.Config, logger clog.Logger) (types.Client, error) {
	return mq.New(conn, cfg, logger)
}
