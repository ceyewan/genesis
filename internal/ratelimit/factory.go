package ratelimit

import (
	"fmt"

	"github.com/ceyewan/genesis/internal/ratelimit/distributed"
	"github.com/ceyewan/genesis/internal/ratelimit/standalone"
	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/connector"
	"github.com/ceyewan/genesis/pkg/ratelimit/types"
	telemetrytypes "github.com/ceyewan/genesis/pkg/telemetry/types"
)

// New 创建限流器实例
func New(
	cfg *types.Config,
	redisConn connector.RedisConnector,
	logger clog.Logger,
	meter telemetrytypes.Meter,
	tracer telemetrytypes.Tracer,
) (types.Limiter, error) {
	// 派生 Logger (添加 "ratelimit" component)
	if logger != nil {
		logger = logger.With(clog.String("component", "ratelimit"))
	}

	switch cfg.Mode {
	case types.ModeStandalone:
		if logger != nil {
			logger.Info("creating standalone rate limiter")
		}
		return standalone.New(cfg, logger, meter, tracer)

	case types.ModeDistributed:
		if redisConn == nil {
			return nil, fmt.Errorf("redis connector is required for distributed mode")
		}
		if logger != nil {
			logger.Info("creating distributed rate limiter")
		}
		return distributed.New(cfg, redisConn, logger, meter, tracer)

	default:
		return nil, fmt.Errorf("unsupported mode: %s", cfg.Mode)
	}
}

