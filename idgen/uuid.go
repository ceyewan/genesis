package idgen

import (
	"github.com/google/uuid"
)

// ========================================
// 静态便捷函数 (Static Convenience Functions)
// ========================================

// NewUUIDV7 生成 UUID v7 (时间排序)
// 这是最常用的 UUID 版本，适合作为数据库主键
//
// 使用示例:
//
//	uid := idgen.NewUUIDV7()
func NewUUIDV7() string {
	v7, _ := uuid.NewV7()
	return v7.String()
}

// NewUUIDV4 生成 UUID v4 (随机)
// 适用于不需要时间排序的场景
//
// 使用示例:
//
//	uid := idgen.NewUUIDV4()
func NewUUIDV4() string {
	return uuid.New().String()
}

// ========================================
// 实例模式 (Instance Mode)
// ========================================

// UUID UUID 生成器
// 支持多个版本，默认使用 v7
type UUID struct {
	version string
}

// UUIDOption UUID 初始化选项
type UUIDOption func(*UUID)

// NewUUID 创建 UUID 生成器
//
// 参数:
//   - opts: 可选参数 (Version)
//
// 使用示例:
//
//	// 默认 v7
//	gen := idgen.NewUUID()
//	uid := gen.Next()
//
//	// 指定 v4
//	gen := idgen.NewUUID(idgen.WithUUIDVersion("v4"))
//	uid := gen.Next()
func NewUUID(opts ...UUIDOption) *UUID {
	u := &UUID{
		version: "v7", // 默认 v7
	}
	for _, opt := range opts {
		opt(u)
	}
	return u
}

// WithUUIDVersion 设置 UUID 版本
// 支持: "v4" | "v7"
func WithUUIDVersion(version string) UUIDOption {
	return func(u *UUID) {
		u.version = version
	}
}

// Next 生成 UUID 字符串
func (u *UUID) Next() string {
	switch u.version {
	case "v4":
		return NewUUIDV4()
	case "v7":
		return NewUUIDV7()
	default:
		return NewUUIDV7()
	}
}
