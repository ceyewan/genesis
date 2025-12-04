package idgen

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/metrics"
)

// uuidGenerator UUID 生成器实现（非导出）
type uuidGenerator struct {
	version string
	// 可观测性组件
	logger clog.Logger
	meter  metrics.Meter
	tracer interface{} // TODO: 实现 Tracer 接口，暂时使用 interface{}
}

// newUUID 创建 UUID 生成器（内部函数）
func newUUID(
	cfg *UUIDConfig,
	logger clog.Logger,
	meter metrics.Meter,
	tracer interface{},
) (Generator, error) {
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

	return &uuidGenerator{
		version: version,
		logger:  logger,
		meter:   meter,
		tracer:  tracer,
	}, nil
}

// String 返回字符串形式的 UUID
func (g *uuidGenerator) String() string {
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

// Close 实现 io.Closer 接口，但由于 uuidGenerator 不拥有任何资源，
// 所以这是 no-op，符合资源所有权规范
func (g *uuidGenerator) Close() error {
	// No-op: UUIDGenerator 不拥有任何资源
	return nil
}
