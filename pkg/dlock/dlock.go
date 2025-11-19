package dlock

import (
	"github.com/ceyewan/genesis/internal/dlock"
	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/connector"
	"github.com/ceyewan/genesis/pkg/dlock/types"
)

// Locker 接口别名，方便使用
type Locker = types.Locker

// Config 配置别名
type Config = types.Config

// Option 选项别名
type Option = types.Option

// 导出常用选项函数
var (
	WithTTL = types.WithTTL
)

// NewRedis 创建 Redis 分布式锁
func NewRedis(conn connector.RedisConnector, cfg *types.Config, logger clog.Logger) (Locker, error) {
	return dlock.NewRedis(conn, cfg, logger)
}

// NewEtcd 创建 Etcd 分布式锁
func NewEtcd(conn connector.EtcdConnector, cfg *types.Config, logger clog.Logger) (Locker, error) {
	return dlock.NewEtcd(conn, cfg, logger)
}
