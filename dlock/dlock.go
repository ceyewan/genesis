// Package dlock 提供分布式锁组件，支持 Redis 和 Etcd 后端。
//
// 特性：
//
//   - 统一的 Locker 接口，屏蔽后端差异
//
//   - 阻塞式 Lock / 非阻塞 TryLock
//
//   - 自动续期（Redis Watchdog / Etcd Session KeepAlive）
//
//   - 防误删机制（token 校验）
//
//     redisConn, _ := connector.NewRedis(&cfg.Redis, connector.WithLogger(logger))
//     locker, _ := dlock.New(&dlock.Config{
//     Driver: dlock.DriverRedis,
//     Prefix: "myapp:lock:",
//     }, dlock.WithRedisConnector(redisConn), dlock.WithLogger(logger))
//
//     if err := locker.Lock(ctx, "resource-key"); err != nil {
//     return err
//     }
//     defer locker.Unlock(ctx, "resource-key")
package dlock

import (
	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/xerrors"
)

// New 创建分布式锁组件（配置驱动）
//
// 通过 cfg.Driver 选择后端，连接器通过 Option 注入：
//   - DriverRedis: WithRedisConnector
//   - DriverEtcd: WithEtcdConnector
func New(cfg *Config, opts ...Option) (Locker, error) {
	if cfg == nil {
		return nil, ErrConfigNil
	}

	cfg.setDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	opt := options{}
	for _, o := range opts {
		o(&opt)
	}

	logger := opt.logger
	if logger != nil {
		logger = logger.With(clog.String("component", "dlock"))
	}

	switch cfg.Driver {
	case DriverRedis:
		if opt.redisConnector == nil {
			return nil, xerrors.New("dlock: redis connector is required, use WithRedisConnector")
		}
		return newRedis(opt.redisConnector, cfg, logger)
	case DriverEtcd:
		if opt.etcdConnector == nil {
			return nil, xerrors.New("dlock: etcd connector is required, use WithEtcdConnector")
		}
		return newEtcd(opt.etcdConnector, cfg, logger)
	default:
		return nil, xerrors.New("dlock: unsupported driver: " + string(cfg.Driver))
	}
}
