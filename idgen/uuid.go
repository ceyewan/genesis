package idgen

import (
	"github.com/google/uuid"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/xerrors"
)

// uuidGen UUID 生成器实现（非导出）
type uuidGen struct {
	version string
	logger  clog.Logger
	meter   metrics.Meter
}

// newUUIDGen 创建 UUID 生成器（内部函数）
func newUUIDGen(
	cfg *UUIDConfig,
	logger clog.Logger,
	meter metrics.Meter,
) (Generator, error) {
	if cfg == nil {
		return nil, xerrors.WithCode(ErrConfigNil, "uuid_config_nil")
	}

	version := cfg.Version
	if version == "" {
		version = "v4"
	}
	if version != "v4" && version != "v7" {
		return nil, xerrors.Wrapf(ErrUnsupportedVersion, "version: %s", version)
	}

	if logger != nil {
		logger.Info("uuid generator created", clog.String("version", version))
	}

	return &uuidGen{
		version: version,
		logger:  logger,
		meter:   meter,
	}, nil
}

// Next 返回字符串形式的 UUID
func (u *uuidGen) Next() string {
	var id string
	switch u.version {
	case "v4":
		id = uuid.New().String()
	case "v7":
		v7, _ := uuid.NewV7()
		id = v7.String()
	default:
		id = uuid.New().String()
	}

	return id
}

// Close 实现 io.Closer 接口，但由于 uuidGen 不拥有任何资源，
// 所以这是 no-op，符合资源所有权规范
func (u *uuidGen) Close() error {
	return nil
}
