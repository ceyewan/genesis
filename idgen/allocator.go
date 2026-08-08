package idgen

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
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

	// KeepAlive 启动后台保活并返回错误通道
	// 保活失败时会向返回的通道发送错误
	KeepAlive(ctx context.Context) <-chan error

	// Stop 停止保活、等待后台任务并释放租约。释放失败会返回错误；
	// 方法可并发重复调用，所有调用者观察同一个最终结果。
	Stop() error
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
//	    Driver: idgen.DriverRedis,
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

	config := *cfg
	config.setDefaults()
	if err := config.validate(); err != nil {
		return nil, err
	}

	// 应用选项
	opt := options{}
	for _, o := range opts {
		o(&opt)
	}

	switch config.Driver {
	case DriverRedis:
		if opt.RedisConnector == nil {
			return nil, xerrors.WithCode(ErrConnectorNil, "redis_connector_required")
		}
		return newRedisAllocator(&config, opt.RedisConnector, opt.Logger)

	case DriverEtcd:
		if opt.EtcdConnector == nil {
			return nil, xerrors.WithCode(ErrConnectorNil, "etcd_connector_required")
		}
		return newEtcdAllocator(&config, opt.EtcdConnector, opt.Logger)

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

	mu            sync.Mutex
	stopOnce      sync.Once
	stopDone      chan struct{}
	stopErr       error
	keepAlive     bool
	wg            sync.WaitGroup
	lifecycleCtx  context.Context
	lifecycleStop context.CancelFunc
	instanceID    int64
	instanceValue string
	redisKey      string
	stopCh        chan struct{}
}

// newRedisAllocator 创建 Redis 分配器
func newRedisAllocator(cfg *AllocatorConfig, redis connector.RedisConnector, logger clog.Logger) (Allocator, error) {
	if logger == nil {
		logger = clog.Discard()
	}
	lifecycleCtx, lifecycleStop := context.WithCancel(context.Background())

	return &redisAllocator{
		redis:         redis,
		cfg:           cfg,
		logger:        logger.With(clog.String("component", "allocator")),
		stopCh:        make(chan struct{}),
		stopDone:      make(chan struct{}),
		lifecycleCtx:  lifecycleCtx,
		lifecycleStop: lifecycleStop,
	}, nil
}

// Allocate 分配 WorkerID（使用随机起点遍历优化并发性能）
func (a *redisAllocator) Allocate(ctx context.Context) (int64, error) {
	a.mu.Lock()
	select {
	case <-a.stopCh:
		a.mu.Unlock()
		return 0, ErrAllocatorStopped
	default:
	}
	if a.redisKey != "" {
		defer a.mu.Unlock()
		return 0, xerrors.WithCode(ErrAlreadyAllocated, "worker_id_already_allocated")
	}
	defer a.mu.Unlock()

	if a.redis == nil {
		return 0, xerrors.WithCode(ErrConnectorNil, "redis_connector_required")
	}

	client := a.redis.GetClient()
	if client == nil {
		return 0, xerrors.WithCode(ErrConnectorNil, "redis_client_required")
	}

	// 随机起点，减少并发冲突
	offset := rand.Int64N(int64(a.cfg.MaxID))

	// Lua 脚本：从 offset 开始环形遍历，原子分配 WorkerID
	script := `
		local prefix = KEYS[1]
		local value = ARGV[1]
		local ttl_ms = tonumber(ARGV[2])
		local max_id = tonumber(ARGV[3])
		local offset = tonumber(ARGV[4])

		-- 从 offset 开始环形遍历
		for i = 0, max_id - 1 do
			local id = (offset + i) % max_id
			local key = prefix .. ":" .. id
			if redis.call("SET", key, value, "NX", "PX", ttl_ms) then
				return id
			end
		end
		return -1
	`

	ttl := a.cfg.TTL.Milliseconds()
	value := fmt.Sprintf("instance:%d:%d", time.Now().UnixNano(), rand.Uint64())
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
	a.instanceValue = value
	a.redisKey = fmt.Sprintf("%s:%d", a.cfg.KeyPrefix, id)

	a.logger.Info("worker id allocated",
		clog.Int64("worker_id", id),
		clog.String("key", a.redisKey),
	)

	return id, nil
}

// KeepAlive 启动后台保活并返回错误通道。
func (a *redisAllocator) KeepAlive(ctx context.Context) <-chan error {
	errCh := make(chan error, 1)
	fail := func(err error) <-chan error {
		errCh <- err
		close(errCh)
		return errCh
	}

	if a.redis == nil {
		return fail(xerrors.WithCode(ErrConnectorNil, "redis_connector_required"))
	}

	client := a.redis.GetClient()
	if client == nil {
		return fail(xerrors.WithCode(ErrConnectorNil, "redis_client_required"))
	}

	a.mu.Lock()
	select {
	case <-a.stopCh:
		a.mu.Unlock()
		return fail(ErrAllocatorStopped)
	default:
	}
	redisKey := a.redisKey
	instanceValue := a.instanceValue
	if redisKey == "" || instanceValue == "" {
		a.mu.Unlock()
		return fail(xerrors.WithCode(ErrInvalidInput, "allocate_must_be_called_first"))
	}
	if a.keepAlive {
		a.mu.Unlock()
		return fail(ErrKeepAliveStarted)
	}
	a.keepAlive = true
	a.wg.Add(1)
	a.mu.Unlock()

	go func() {
		defer a.wg.Done()
		defer close(errCh)
		keepAliveCtx, cancel := context.WithCancel(ctx)
		stopAfter := context.AfterFunc(a.lifecycleCtx, cancel)
		defer stopAfter()
		defer cancel()

		ticker := time.NewTicker(a.cfg.TTL / 3)
		defer ticker.Stop()

		script := `
			local key = KEYS[1]
			local expected = ARGV[1]
			local ttl_ms = tonumber(ARGV[2])

			local current = redis.call("GET", key)
			if not current or current ~= expected then
				return 0
			end

			redis.call("PEXPIRE", key, ttl_ms)
			return 1
		`

		for {
			select {
			case <-keepAliveCtx.Done():
				return
			case <-ticker.C:
				result, err := client.Eval(
					keepAliveCtx,
					script,
					[]string{redisKey},
					instanceValue,
					a.cfg.TTL.Milliseconds(),
				).Result()
				if err != nil {
					a.logger.Error("keep alive failed",
						clog.Error(err),
						clog.String("key", redisKey),
					)
					select {
					case errCh <- xerrors.Wrap(err, "keep_alive_failed"):
					default:
					}
					return
				}

				ok, okType := result.(int64)
				if !okType || ok != 1 {
					a.logger.Error("worker id ownership lost",
						clog.String("key", redisKey),
					)
					select {
					case errCh <- xerrors.WithCode(ErrLeaseExpired, "worker_id_ownership_lost"):
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
func (a *redisAllocator) Stop() error {
	a.stopOnce.Do(func() {
		close(a.stopCh)
		a.lifecycleStop()
		a.wg.Wait()
		defer close(a.stopDone)

		if a.redis == nil {
			return
		}

		client := a.redis.GetClient()
		if client == nil {
			return
		}

		a.mu.Lock()
		redisKey := a.redisKey
		instanceValue := a.instanceValue
		instanceID := a.instanceID
		a.mu.Unlock()
		if redisKey == "" || instanceValue == "" {
			return
		}

		script := `
			local key = KEYS[1]
			local expected = ARGV[1]

			local current = redis.call("GET", key)
			if current and current == expected then
				return redis.call("DEL", key)
			end
			return 0
		`

		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		result, err := client.Eval(releaseCtx, script, []string{redisKey}, instanceValue).Int64()
		if err != nil {
			a.stopErr = xerrors.Wrap(err, "release worker id")
			return
		}
		if result == 0 {
			a.stopErr = xerrors.Wrap(ErrLeaseExpired, "worker id ownership lost before Stop")
			return
		}
		a.logger.Info("worker id released",
			clog.Int64("worker_id", instanceID),
			clog.String("key", redisKey),
		)
	})
	<-a.stopDone
	return a.stopErr
}

// ========================================
// Etcd 实现
// ========================================

// etcdAllocator Etcd 实现的 WorkerID 分配器
type etcdAllocator struct {
	client *clientv3.Client
	cfg    *AllocatorConfig
	logger clog.Logger

	mu            sync.Mutex
	stopOnce      sync.Once
	stopDone      chan struct{}
	stopErr       error
	keepAlive     bool
	wg            sync.WaitGroup
	lifecycleCtx  context.Context
	lifecycleStop context.CancelFunc
	leaseID       clientv3.LeaseID
	workerID      int64
	etcdKey       string
	stopCh        chan struct{}
}

// newEtcdAllocator 创建 Etcd 分配器
func newEtcdAllocator(cfg *AllocatorConfig, etcdConn connector.EtcdConnector, logger clog.Logger) (Allocator, error) {
	if logger == nil {
		logger = clog.Discard()
	}
	lifecycleCtx, lifecycleStop := context.WithCancel(context.Background())

	return &etcdAllocator{
		client:        etcdConn.GetClient(),
		cfg:           cfg,
		logger:        logger.With(clog.String("component", "allocator")),
		stopCh:        make(chan struct{}),
		stopDone:      make(chan struct{}),
		lifecycleCtx:  lifecycleCtx,
		lifecycleStop: lifecycleStop,
	}, nil
}

// Allocate 分配 WorkerID（使用随机起点遍历优化并发性能）
func (a *etcdAllocator) Allocate(ctx context.Context) (int64, error) {
	a.mu.Lock()
	select {
	case <-a.stopCh:
		a.mu.Unlock()
		return 0, ErrAllocatorStopped
	default:
	}
	if a.leaseID != 0 || a.etcdKey != "" {
		defer a.mu.Unlock()
		return 0, xerrors.WithCode(ErrAlreadyAllocated, "worker_id_already_allocated")
	}
	defer a.mu.Unlock()

	if a.client == nil {
		return 0, xerrors.WithCode(ErrConnectorNil, "etcd_client_required")
	}

	// 创建 Lease
	leaseTTL := int64((a.cfg.TTL + time.Second - 1) / time.Second)
	lease, err := a.client.Grant(ctx, leaseTTL)
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
			if revokeErr := a.revokeLease(lease.ID); revokeErr != nil {
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

			a.logger.Info("worker id allocated",
				clog.Int64("worker_id", int64(id)),
				clog.String("key", key),
				clog.Int64("lease_id", int64(lease.ID)),
			)

			return int64(id), nil
		}
	}

	// 所有 ID 都被占用，清理 Lease
	if revokeErr := a.revokeLease(lease.ID); revokeErr != nil {
		if a.logger != nil {
			a.logger.Warn("etcd revoke lease failed during cleanup", clog.Error(revokeErr))
		}
	}
	return 0, xerrors.WithCode(ErrWorkerIDExhausted, "no_available_worker_id")
}

// KeepAlive 启动后台保活并返回错误通道。
func (a *etcdAllocator) KeepAlive(ctx context.Context) <-chan error {
	errCh := make(chan error, 1)
	fail := func(err error) <-chan error {
		errCh <- err
		close(errCh)
		return errCh
	}

	if a.client == nil {
		return fail(xerrors.WithCode(ErrConnectorNil, "etcd_client_required"))
	}

	a.mu.Lock()
	select {
	case <-a.stopCh:
		a.mu.Unlock()
		return fail(ErrAllocatorStopped)
	default:
	}
	leaseID := a.leaseID
	if leaseID == 0 {
		a.mu.Unlock()
		return fail(xerrors.WithCode(ErrInvalidInput, "allocate_must_be_called_first"))
	}
	if a.keepAlive {
		a.mu.Unlock()
		return fail(ErrKeepAliveStarted)
	}
	a.keepAlive = true
	a.wg.Add(1)
	a.mu.Unlock()

	go func() {
		defer a.wg.Done()
		defer close(errCh)
		keepAliveCtx, cancel := context.WithCancel(ctx)
		stopAfter := context.AfterFunc(a.lifecycleCtx, cancel)
		defer stopAfter()
		defer cancel()

		// 启动 KeepAlive
		kaCh, err := a.client.KeepAlive(keepAliveCtx, leaseID)
		if err != nil {
			a.logger.Error("etcd keep alive failed",
				clog.Error(err),
				clog.Int64("lease_id", int64(leaseID)),
			)
			select {
			case errCh <- xerrors.Wrap(err, "keep_alive_failed"):
			default:
			}
			return
		}

		for {
			select {
			case <-keepAliveCtx.Done():
				return
			case ka, ok := <-kaCh:
				if !ok || ka == nil {
					// KeepAlive 通道关闭或返回 nil，表示租约已失效
					a.logger.Error("lease expired",
						clog.Int64("lease_id", int64(leaseID)),
					)
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
func (a *etcdAllocator) Stop() error {
	a.stopOnce.Do(func() {
		close(a.stopCh)
		a.lifecycleStop()
		a.wg.Wait()
		defer close(a.stopDone)

		if a.client == nil {
			return
		}

		a.mu.Lock()
		leaseID := a.leaseID
		workerID := a.workerID
		etcdKey := a.etcdKey
		a.mu.Unlock()
		if leaseID == 0 {
			return
		}

		// 撤销 Lease，关联的 key 会自动删除
		if err := a.revokeLease(leaseID); err != nil {
			a.stopErr = xerrors.Wrap(err, "revoke allocator lease")
			return
		}
		a.logger.Info("worker id released",
			clog.Int64("worker_id", workerID),
			clog.String("key", etcdKey),
			clog.Int64("lease_id", int64(leaseID)),
		)
	})
	<-a.stopDone
	return a.stopErr
}

func (a *etcdAllocator) revokeLease(leaseID clientv3.LeaseID) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := a.client.Revoke(ctx, leaseID)
	return err
}
