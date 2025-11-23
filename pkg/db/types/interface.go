package types

import (
	"context"

	"gorm.io/gorm"
)

// DB 定义了数据库组件的核心能力
type DB interface {
	// DB 获取底层的 *gorm.DB 实例
	// 绝大多数业务查询直接使用此方法返回的对象
	DB(ctx context.Context) *gorm.DB

	// Transaction 执行事务操作
	// fn 中的 tx 对象仅在当前事务范围内有效
	Transaction(ctx context.Context, fn func(ctx context.Context, tx *gorm.DB) error) error

	// Close 关闭组件
	Close() error
}

