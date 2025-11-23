package db

import (
	internaldb "github.com/ceyewan/genesis/internal/db"
	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/connector"
	"github.com/ceyewan/genesis/pkg/db/types"
)

// 导出 types 包中的定义，方便用户使用

type DB = types.DB
type Config = types.Config
type ShardingRule = types.ShardingRule

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

	return internaldb.New(conn, (*types.Config)(cfg), opt.Logger, opt.Meter, opt.Tracer)
}
