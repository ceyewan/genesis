package uuid

import (
	"fmt"

	"github.com/ceyewan/genesis/pkg/idgen/types"
	"github.com/google/uuid"
)

// Generator UUID 生成器实现
type Generator struct {
	version string
}

// New 创建 UUID 生成器
func New(cfg *types.UUIDConfig) (*Generator, error) {
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
	return &Generator{version: version}, nil
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
