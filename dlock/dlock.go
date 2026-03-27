// Package dlock 提供 Genesis 的分布式锁组件。
//
// `dlock` 的定位是为常见的“跨实例互斥”场景提供一层克制、统一的锁接口，
// 当前支持 Redis 和 Etcd 两种后端。调用方通过 `Locker` 使用阻塞式 `Lock`、
// 非阻塞式 `TryLock`、显式 `Unlock` 和 `Close`，不需要直接处理 Redis 的
// token 校验或 Etcd 的 session / mutex 细节。
//
// 这个组件的核心目标不是抽象出一个“万能锁框架”，而是把最常见、最稳定的
// 分布式锁能力收敛成少量接口，并把几个容易出错的边界收紧：
//
//   - Redis 使用 token + Lua 脚本，避免误删别人的锁。
//   - Redis 和 Etcd 都会在锁持有期间自动续期。
//   - `Close()` 会停止续期，并尽力释放当前 `Locker` 已持有的锁。
//   - 同一个 `Locker` 不允许本地重入同一个 key。
//
// `dlock` 不负责可重入锁、读写锁、公平锁、锁诊断平台或死锁检测。它更适合
// 任务竞选、资源互斥、短事务串行化这类“需要一把简单分布式锁”的场景。
//
// 需要注意的是，Redis 与 Etcd 并不是完全等价的协议实现。尤其在 TTL 语义上，
// Etcd 依赖 lease，精度为秒级，因此 `DefaultTTL` 和 `WithTTL(...)` 都要求
// 至少 1 秒且必须是整秒；Redis 则直接使用原生 `time.Duration`。
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
