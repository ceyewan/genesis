package idgen

import "github.com/google/uuid"

// ========================================
// UUID 生成器
// ========================================

// UUID 生成 UUID v7 字符串（时间排序，适合数据库主键）
//
// 使用示例:
//
//	id, err := idgen.UUID()
func UUID() (string, error) {
	v7, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	return v7.String(), nil
}
