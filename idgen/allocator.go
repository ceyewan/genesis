package idgen

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/xerrors"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// ========================================
// Allocator 接口 (WorkerID Allocation)
// ========================================

// Allocator WorkerID 分配器接口
// 用于在集群环境中自动分配唯一的 WorkerID，避免手动配置冲突
type Allocator interface {
	// Allocate 分配 WorkerID（阻塞直到分配成功）
	Allocate(ctx context.Context) (int64, error)

	// KeepAlive 保持租约（阻塞方法，应在 goroutine 中运行）
	// 返回错误通道，保活失败时会发送错误
	KeepAlive(ctx context.Context) <-chan error

	// Stop 停止保活并释放资源
	Stop()
}

// ========================================
// 统一工厂函数
// ========================================

// NewAllocator 创建 WorkerID 分配器
// 根据 cfg.Driver 选择 redis 或 etcd 实现
//
// 使用示例:
//
//	// Redis 分配器
//	allocator, _ := idgen.NewAllocator(&idgen.AllocatorConfig{
//	    Driver: "redis",
//	    MaxID:  512,
//	}, idgen.WithRedisConnector(redisConn))
//
//	workerID, _ := allocator.Allocate(ctx)
//	defer allocator.Stop()
//
//	go func() {
//	    if err := <-allocator.KeepAlive(ctx); err != nil {
//	        // 处理保活失败
//	    }
//	}()
func NewAllocator(cfg *AllocatorConfig, opts ...Option) (Allocator, error) {
	if cfg == nil {
		return nil, xerrors.WithCode(ErrInvalidInput, "config_nil")
	}

	cfg.setDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	// 应用选项
	opt := options{}
	for _, o := range opts {
		o(&opt)
	}

	switch cfg.Driver {
	case "redis":
		if opt.RedisConnector == nil {
			return nil, xerrors.WithCode(ErrConnectorNil, "redis_connector_required")
		}
		return newRedisAllocator(cfg, opt.RedisConnector, opt.Logger)

	case "etcd":
		if opt.EtcdConnector == nil {
			return nil, xerrors.WithCode(ErrConnectorNil, "etcd_connector_required")
		}
		return newEtcdAllocator(cfg, opt.EtcdConnector, opt.Logger)

	default:
		return nil, xerrors.WithCode(ErrInvalidInput, "unsupported_driver")
	}
}

// ========================================
// Redis 实现
// ========================================

// redisAllocator Redis 实现的 WorkerID 分配器
type redisAllocator struct {
	redis  connector.RedisConnector
	cfg    *AllocatorConfig
	logger clog.Logger

	instanceID int64
	redisKey   string
	stopCh     chan struct{}
}

// newRedisAllocator 创建 Redis 分配器
func newRedisAllocator(cfg *AllocatorConfig, redis connector.RedisConnector, logger clog.Logger) (Allocator, error) {
	return &redisAllocator{
		redis:  redis,
		cfg:    cfg,
		logger: logger,
		stopCh: make(chan struct{}),
	}, nil
}

// Allocate 分配 WorkerID（使用随机起点遍历优化并发性能）
func (a *redisAllocator) Allocate(ctx context.Context) (int64, error) {
	client := a.redis.GetClient()

	// 随机起点，减少并发冲突
	offset := rand.Int64N(int64(a.cfg.MaxID))

	// Lua 脚本：从 offset 开始环形遍历，原子分配 WorkerID
	script := `
		local prefix = KEYS[1]
		local value = ARGV[1]
		local ttl = tonumber(ARGV[2])
		local max_id = tonumber(ARGV[3])
		local offset = tonumber(ARGV[4])

		-- 从 offset 开始环形遍历
		for i = 0, max_id - 1 do
			local id = (offset + i) % max_id
			local key = prefix .. ":" .. id
			if redis.call("SET", key, value, "NX", "EX", ttl) then
				return id
			end
		end
		return -1
	`

	ttl := a.cfg.TTL
	value := fmt.Sprintf("host:%d", time.Now().UnixNano())
	result, err := client.Eval(ctx, script, []string{a.cfg.KeyPrefix}, value, ttl, a.cfg.MaxID, offset).Result()
	if err != nil {
		if a.logger != nil {
			a.logger.Error("redis eval failed",
				clog.Error(err),
				clog.String("key_prefix", a.cfg.KeyPrefix),
			)
		}
		return 0, xerrors.Wrap(err, "redis_eval_failed")
	}

	id, ok := result.(int64)
	if !ok || id < 0 {
		return 0, xerrors.WithCode(ErrWorkerIDExhausted, "no_available_worker_id")
	}

	a.instanceID = id
	a.redisKey = fmt.Sprintf("%s:%d", a.cfg.KeyPrefix, id)

	if a.logger != nil {
		a.logger.Info("worker id allocated",
			clog.Int64("worker_id", id),
			clog.String("key", a.redisKey),
		)
	}

	return id, nil
}

// KeepAlive 保持租约
func (a *redisAllocator) KeepAlive(ctx context.Context) <-chan error {
	errCh := make(chan error, 1)

	go func() {
		ticker := time.NewTicker(time.Duration(a.cfg.TTL/3) * time.Second)
		defer ticker.Stop()
		client := a.redis.GetClient()

		for {
			select {
			case <-a.stopCh:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := client.Expire(context.Background(), a.redisKey, time.Duration(a.cfg.TTL)*time.Second).Err(); err != nil {
					if a.logger != nil {
						a.logger.Error("keep alive failed",
							clog.Error(err),
							clog.String("key", a.redisKey),
						)
					}
					select {
					case errCh <- xerrors.Wrap(err, "keep_alive_failed"):
					default:
					}
					return
				}
			}
		}
	}()

	return errCh
}

// Stop 停止保活并释放资源
func (a *redisAllocator) Stop() {
	close(a.stopCh)

	if a.redisKey != "" {
		client := a.redis.GetClient()
		client.Del(context.Background(), a.redisKey)

		if a.logger != nil {
			a.logger.Info("worker id released",
				clog.Int64("worker_id", a.instanceID),
				clog.String("key", a.redisKey),
			)
		}
	}
}

// ========================================
// Etcd 实现
// ========================================

// etcdAllocator Etcd 实现的 WorkerID 分配器
type etcdAllocator struct {
	client *clientv3.Client
	cfg    *AllocatorConfig
	logger clog.Logger

	leaseID  clientv3.LeaseID
	workerID int64
	etcdKey  string
	stopCh   chan struct{}
}

// newEtcdAllocator 创建 Etcd 分配器
func newEtcdAllocator(cfg *AllocatorConfig, etcdConn connector.EtcdConnector, logger clog.Logger) (Allocator, error) {
	return &etcdAllocator{
		client: etcdConn.GetClient(),
		cfg:    cfg,
		logger: logger,
		stopCh: make(chan struct{}),
	}, nil
}

// Allocate 分配 WorkerID（使用随机起点遍历优化并发性能）
func (a *etcdAllocator) Allocate(ctx context.Context) (int64, error) {
	// 创建 Lease
	lease, err := a.client.Grant(ctx, int64(a.cfg.TTL))
	if err != nil {
		if a.logger != nil {
			a.logger.Error("etcd grant lease failed", clog.Error(err))
		}
		return 0, xerrors.Wrap(err, "etcd_grant_failed")
	}

	value := fmt.Sprintf("host:%d", time.Now().UnixNano())

	// 随机起点，减少并发冲突
	offset := rand.IntN(a.cfg.MaxID)

	// 从 offset 开始环形遍历，尝试抢占 WorkerID
	for i := 0; i < a.cfg.MaxID; i++ {
		id := (offset + i) % a.cfg.MaxID
		key := fmt.Sprintf("%s:%d", a.cfg.KeyPrefix, id)

		// 使用事务实现 CAS：如果 key 不存在（ModRevision == 0），则创建
		resp, err := a.client.Txn(ctx).
			If(clientv3.Compare(clientv3.ModRevision(key), "=", 0)).
			Then(clientv3.OpPut(key, value, clientv3.WithLease(lease.ID))).
			Commit()
		if err != nil {
			// 清理已创建的 Lease
			if _, revokeErr := a.client.Revoke(context.Background(), lease.ID); revokeErr != nil {
				if a.logger != nil {
					a.logger.Warn("etcd revoke lease failed during cleanup", clog.Error(revokeErr))
				}
			}
			if a.logger != nil {
				a.logger.Error("etcd txn failed",
					clog.Error(err),
					clog.String("key", key),
				)
			}
			return 0, xerrors.Wrap(err, "etcd_txn_failed")
		}

		if resp.Succeeded {
			a.leaseID = lease.ID
			a.workerID = int64(id)
			a.etcdKey = key

			if a.logger != nil {
				a.logger.Info("worker id allocated",
					clog.Int64("worker_id", int64(id)),
					clog.String("key", key),
					clog.Int64("lease_id", int64(lease.ID)),
				)
			}

			return int64(id), nil
		}
	}

	// 所有 ID 都被占用，清理 Lease
	if _, revokeErr := a.client.Revoke(context.Background(), lease.ID); revokeErr != nil {
		if a.logger != nil {
			a.logger.Warn("etcd revoke lease failed during cleanup", clog.Error(revokeErr))
		}
	}
	return 0, xerrors.WithCode(ErrWorkerIDExhausted, "no_available_worker_id")
}

// KeepAlive 保持租约
func (a *etcdAllocator) KeepAlive(ctx context.Context) <-chan error {
	errCh := make(chan error, 1)

	go func() {
		// 启动 KeepAlive
		kaCh, err := a.client.KeepAlive(ctx, a.leaseID)
		if err != nil {
			if a.logger != nil {
				a.logger.Error("etcd keep alive failed",
					clog.Error(err),
					clog.Int64("lease_id", int64(a.leaseID)),
				)
			}
			select {
			case errCh <- xerrors.Wrap(err, "keep_alive_failed"):
			default:
			}
			return
		}

		for {
			select {
			case <-a.stopCh:
				return
			case <-ctx.Done():
				return
			case ka, ok := <-kaCh:
				if !ok || ka == nil {
					// KeepAlive 通道关闭或返回 nil，表示租约已失效
					if a.logger != nil {
						a.logger.Error("lease expired",
							clog.Int64("lease_id", int64(a.leaseID)),
						)
					}
					select {
					case errCh <- xerrors.WithCode(ErrLeaseExpired, "lease_expired"):
					default:
					}
					return
				}
			}
		}
	}()

	return errCh
}

// Stop 停止保活并释放资源
func (a *etcdAllocator) Stop() {
	close(a.stopCh)

	if a.leaseID != 0 {
		// 撤销 Lease，关联的 key 会自动删除
		_, _ = a.client.Revoke(context.Background(), a.leaseID)

		if a.logger != nil {
			a.logger.Info("worker id released",
				clog.Int64("worker_id", a.workerID),
				clog.String("key", a.etcdKey),
				clog.Int64("lease_id", int64(a.leaseID)),
			)
		}
	}
}
