package db

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/sharding"

	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/connector"
	"github.com/ceyewan/genesis/pkg/db/types"
	telemetrytypes "github.com/ceyewan/genesis/pkg/telemetry/types"
)

// database 是 DB 接口的实现
type database struct {
	client *gorm.DB
	logger clog.Logger
	meter  telemetrytypes.Meter
	tracer telemetrytypes.Tracer
}

// New 创建一个新的数据库组件实例
func New(conn connector.MySQLConnector, cfg *types.Config, logger clog.Logger, meter telemetrytypes.Meter, tracer telemetrytypes.Tracer) (types.DB, error) {
	gormDB := conn.GetClient()

	// 使用默认 Logger 如果未提供
	if logger == nil {
		logger = clog.Default()
	}

	// 注册分片中间件
	if cfg.EnableSharding && len(cfg.ShardingRules) > 0 {
		for _, rule := range cfg.ShardingRules {
			// 将字符串表名转换为 interface{} 切片以适配 Register 方法
			tables := make([]interface{}, len(rule.Tables))
			for i, v := range rule.Tables {
				tables[i] = v
			}

			middleware := sharding.Register(sharding.Config{
				ShardingKey:         rule.ShardingKey,
				NumberOfShards:      rule.NumberOfShards,
				PrimaryKeyGenerator: sharding.PKSnowflake, // 默认使用雪花算法
			}, tables...)

			if err := gormDB.Use(middleware); err != nil {
				return nil, fmt.Errorf("failed to register sharding middleware for tables %v: %w", rule.Tables, err)
			}
		}
	}

	return &database{
		client: gormDB,
		logger: logger,
		meter:  meter,
		tracer: tracer,
	}, nil
}

// DB 获取底层的 *gorm.DB 实例
func (d *database) DB(ctx context.Context) *gorm.DB {
	return d.client.WithContext(ctx)
}

// Transaction 执行事务操作
func (d *database) Transaction(ctx context.Context, fn func(ctx context.Context, tx *gorm.DB) error) error {
	return d.client.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(ctx, tx)
	})
}

// Close 关闭组件
func (d *database) Close() error {
	// GORM 的连接由连接器管理，这里不需要额外关闭
	return nil
}
