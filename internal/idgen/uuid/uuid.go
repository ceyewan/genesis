package uuid

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/idgen/types"
	telemetrytypes "github.com/ceyewan/genesis/pkg/telemetry/types"
)

// Generator UUID 生成器实现
type Generator struct {
	version string
	// 可观测性组件
	logger clog.Logger
	meter  telemetrytypes.Meter
	tracer telemetrytypes.Tracer
}

// New 创建 UUID 生成器
func New(
	cfg *types.UUIDConfig,
	logger clog.Logger,
	meter telemetrytypes.Meter,
	tracer telemetrytypes.Tracer,
) (*Generator, error) {
	if cfg == nil {
		return nil, fmt.Errorf("uuid config is nil")
	}
	version := cfg.Version
	if version == "" {
		version = "v4"
	}
	if version != "v4" && version != "v7" {
		return nil, fmt.Errorf("unsupported uuid version: %s", version)
	}

	if logger != nil {
		logger.Info("uuid generator created", clog.String("version", version))
	}

	return &Generator{
		version: version,
		logger:  logger,
		meter:   meter,
		tracer:  tracer,
	}, nil
}

// String 返回字符串形式的 UUID
func (g *Generator) String() string {
	switch g.version {
	case "v4":
		return uuid.New().String()
	case "v7":
		u, _ := uuid.NewV7()
		return u.String()
	default:
		return uuid.New().String()
	}
}
