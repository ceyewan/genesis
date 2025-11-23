package mq

import (
	"fmt"

	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/connector"
	"github.com/ceyewan/genesis/pkg/mq/types"
	telemetrytypes "github.com/ceyewan/genesis/pkg/telemetry/types"
)

// New 创建 MQ 客户端
func New(conn connector.NATSConnector, cfg *types.Config, logger clog.Logger, meter telemetrytypes.Meter, tracer telemetrytypes.Tracer) (types.Client, error) {
	if cfg == nil {
		return nil, fmt.Errorf("mq config is required")
	}

	// 使用默认 Logger 如果未提供
	if logger == nil {
		logger = clog.Default()
	}

	switch cfg.Driver {
	case types.DriverNatsCore:
		return NewCoreClient(conn, logger, meter, tracer), nil
	case types.DriverNatsJetStream:
		return NewJetStreamClient(conn, cfg.JetStream, logger, meter, tracer)
	default:
		return nil, fmt.Errorf("unsupported mq driver: %s", cfg.Driver)
	}
}
