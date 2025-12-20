package dlock

import (
	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/dlock/types"
	internaldlock "github.com/ceyewan/genesis/internal/dlock"
)

// 导出 types 包中的定义，方便用户使用

type Locker = types.Locker
type Config = types.Config
type BackendType = types.BackendType

// Lock 操作相关类型
type LockOptions = types.LockOptions
type LockOption = types.LockOption

const (
	BackendRedis = types.BackendRedis
	BackendEtcd  = types.BackendEtcd
)

// 导出 Lock 操作的选项函数
var (
	WithTTL = types.WithTTL
)

// NewRedis 创建 Redis 分布式锁 (独立模式)
// 这是标准的工厂函数，支持在不依赖 Container 的情况下独立实例化
//
// 参数:
//   - conn: Redis 连接器
//   - cfg: DLock 配置
//   - opts: 可选参数 (Logger, Meter, Tracer)
//
// 使用示例:
//
//	redisConn, _ := connector.NewRedis(redisConfig)
//	locker, _ := dlock.NewRedis(redisConn, &dlock.Config{
//	    Prefix: "myapp:lock:",
//	    DefaultTTL: 30 * time.Second,
//	}, dlock.WithLogger(logger))
func NewRedis(conn connector.RedisConnector, cfg *Config, opts ...Option) (Locker, error) {
	// 应用选项
	opt := Options{
		Logger: clog.Default(), // 默认 Logger
	}
	for _, o := range opts {
		o(&opt)
	}

	return internaldlock.NewRedis(conn, cfg, opt.Logger, opt.Meter)
}

// NewEtcd 创建 Etcd 分布式锁 (独立模式)
// 这是标准的工厂函数，支持在不依赖 Container 的情况下独立实例化
//
// 参数:
//   - conn: Etcd 连接器
//   - cfg: DLock 配置
//   - opts: 可选参数 (Logger, Meter, Tracer)
//
// 使用示例:
//
//	etcdConn, _ := connector.NewEtcd(etcdConfig)
//	locker, _ := dlock.NewEtcd(etcdConn, &dlock.Config{
//	    Prefix: "myapp:lock:",
//	    DefaultTTL: 30 * time.Second,
//	}, dlock.WithLogger(logger))
func NewEtcd(conn connector.EtcdConnector, cfg *Config, opts ...Option) (Locker, error) {
	// 应用选项
	opt := Options{
		Logger: clog.Default(), // 默认 Logger
	}
	for _, o := range opts {
		o(&opt)
	}

	return internaldlock.NewEtcd(conn, cfg, opt.Logger, opt.Meter)
}
