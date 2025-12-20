package ratelimit

import (
	"fmt"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/internal/ratelimit/distributed"
	"github.com/ceyewan/genesis/internal/ratelimit/standalone"
	metrics "github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/ratelimit/types"
)

// New 创建限流器实例
func New(
	cfg *types.Config,
	redisConn connector.RedisConnector,
	logger clog.Logger,
	meter metrics.Meter,
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
		return standalone.New(cfg, logger, meter)

	case types.ModeDistributed:
		if redisConn == nil {
			return nil, fmt.Errorf("redis connector is required for distributed mode")
		}
		if logger != nil {
			logger.Info("creating distributed rate limiter")
		}
		return distributed.New(cfg, redisConn, logger, meter)

	default:
		return nil, fmt.Errorf("unsupported mode: %s", cfg.Mode)
	}
}
