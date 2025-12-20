package db

import (
	"context"
	"fmt"

	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/connector"
	"github.com/ceyewan/genesis/pkg/metrics"
	"gorm.io/gorm"
	"gorm.io/sharding"
)

// database 是 DB 接口的实现
type database struct {
	client *gorm.DB
	logger clog.Logger
	meter  metrics.Meter
}

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

// New 创建数据库组件实例 (独立模式)
// 这是标准的工厂函数，支持在不依赖 Container 的情况下独立实例化
//
// 参数:
//   - conn: MySQL 连接器
//   - cfg: DB 配置
//   - opts: 可选参数 (Logger, Meter, Tracer)
//
// 使用示例:
//
//	mysqlConn, _ := connector.NewMySQL(mysqlConfig)
//	database, _ := db.New(mysqlConn, &db.Config{
//	    EnableSharding: true,
//	    ShardingRules: []db.ShardingRule{
//	        {
//	            ShardingKey:    "user_id",
//	            NumberOfShards: 64,
//	            Tables:         []string{"orders"},
//	        },
//	    },
//	}, db.WithLogger(logger))
func New(conn connector.MySQLConnector, cfg *Config, opts ...Option) (DB, error) {
	// 应用选项
	opt := Options{
		Logger: clog.Default(), // 默认 Logger
	}
	for _, o := range opts {
		o(&opt)
	}

	gormDB := conn.GetClient()

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
		logger: opt.Logger,
		meter:  opt.Meter,
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
