package idgen

import "github.com/google/uuid"

// ========================================
// UUID 生成器
// ========================================

// UUID 生成 UUID v7 字符串（时间排序，适合数据库主键）
//
// 使用示例:
//
//	id := idgen.UUID()
func UUID() string {
	v7, _ := uuid.NewV7()
	return v7.String()
}
