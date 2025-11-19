package mq

import (
	"fmt"

	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/connector"
	"github.com/ceyewan/genesis/pkg/mq/types"
)

// New 创建 MQ 客户端
func New(conn connector.NATSConnector, cfg *types.Config, logger clog.Logger) (types.Client, error) {
	if cfg == nil {
		return nil, fmt.Errorf("mq config is required")
	}

	switch cfg.Driver {
	case types.DriverNatsCore:
		return NewCoreClient(conn, logger), nil
	case types.DriverNatsJetStream:
		return NewJetStreamClient(conn, cfg.JetStream, logger)
	default:
		return nil, fmt.Errorf("unsupported mq driver: %s", cfg.Driver)
	}
}
