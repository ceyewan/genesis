// Package registry 提供了基于 Etcd 的服务注册发现组件，支持 gRPC 集成和客户端负载均衡。
//
// registry 组件是 Genesis 治理层的核心组件，它在 Etcd 连接器的基础上提供了：
// - 服务注册与发现能力
// - 实时服务变化监听
// - 本地缓存机制提升性能
// - gRPC Resolver 集成，支持 `etcd://<service_name>` 解析
// - 自动租约续约和优雅下线
// - 与 L0 基础组件（日志、指标、错误）的深度集成
//
// ## 基本使用
//
//	etcdConn, _ := connector.NewEtcd(&cfg.Etcd, connector.WithLogger(logger))
//	defer etcdConn.Close()
//	etcdConn.Connect(ctx)
//
//	reg, _ := registry.New(etcdConn, &registry.Config{
//		Namespace:       "/genesis/services",
//		DefaultTTL:      30 * time.Second,
//		EnableCache:     true,
//		CacheExpiration: 10 * time.Second,
//	}, registry.WithLogger(logger))
//	defer reg.Close()
//
//	// 注册服务
//	service := &registry.ServiceInstance{
//		ID:        "user-service-001",
//		Name:      "user-service",
//		Endpoints: []string{"grpc://127.0.0.1:8080"},
//	}
//	err := reg.Register(ctx, service, 30*time.Second)
//
//	// 服务发现
//	instances, err := reg.GetService(ctx, "user-service")
//
//	// gRPC 集成
//	conn, err := reg.GetConnection(ctx, "user-service")
//	defer conn.Close()
//	client := pb.NewUserServiceClient(conn)
//
// ## Etcd 存储结构
//
// 服务实例在 Etcd 中的存储采用层级结构：
//
//	<namespace>/<service_name>/<instance_id> -> JSON(ServiceInstance)
//
// 例如：
// - `/genesis/services/user-service/uuid-1234-5678`
// - `/genesis/services/order-service/uuid-abcd-efgh`
//
// ## gRPC 集成
//
// Registry 组件实现了 gRPC resolver.Builder 接口，支持原生 gRPC 服务发现：
//
//	// 方式一：使用 GetConnection（推荐）
//	conn, err := reg.GetConnection(ctx, "user-service")
//
//	// 方式二：使用原生 gRPC Dial
//	conn, err := grpc.Dial(
//		"etcd:///user-service",
//		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy":"round_robin"}`),
//		grpc.WithTransportCredentials(insecure.NewCredentials()),
//	)
//
// ## 设计原则
//
// - **借用模型**：registry 组件借用 Etcd 连接器的连接，不负责连接的生命周期
// - **显式依赖**：通过构造函数显式注入连接器和选项
// - **gRPC 原生支持**：深度集成 gRPC 生态，提供开箱即用的服务发现
// - **高性能**：本地缓存 + 实时监听，减少对注册中心的直接请求
// - **可观测性**：集成 clog 和 metrics，提供完整的日志和指标能力
package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/xerrors"

	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/resolver"
)

// New 创建 Registry 实例（基于 Etcd）
// 这是标准的工厂函数，支持在不依赖 Container 的情况下独立实例化
//
// 参数:
//   - conn: Etcd 连接器
//   - cfg: Registry 配置
//   - opts: 可选参数 (Logger, Meter, Tracer)
//
// 使用示例:
//
//	etcdConn, _ := connector.NewEtcd(etcdConfig)
//	registry, _ := registry.New(etcdConn, &registry.Config{
//	    Namespace: "/genesis/services",
//	}, registry.WithLogger(logger))
func New(conn connector.EtcdConnector, cfg *Config, opts ...Option) (Registry, error) {
	if conn == nil {
		return nil, xerrors.New("etcd connector is required")
	}
	if cfg == nil {
		cfg = &Config{} // 使用默认配置
	}

	// 应用选项
	opt := defaultOptions()
	for _, o := range opts {
		o(opt)
	}

	client := conn.GetClient()
	if client == nil {
		return nil, xerrors.New("etcd client cannot be nil")
	}

	// 设置默认值
	if cfg.Namespace == "" {
		cfg.Namespace = "/genesis/services"
	}
	if cfg.Schema == "" {
		cfg.Schema = "etcd"
	}
	if cfg.DefaultTTL == 0 {
		cfg.DefaultTTL = 30 * time.Second
	}
	if cfg.RetryInterval == 0 {
		cfg.RetryInterval = 1 * time.Second
	}
	if cfg.CacheExpiration == 0 {
		cfg.CacheExpiration = 10 * time.Second
	}

	if opt.logger == nil {
		opt.logger, _ = clog.New(&clog.Config{
			Level:  "info",
			Format: "console",
			Output: "stdout",
		})
	}

	r := &etcdRegistry{
		client:   client,
		cfg:      cfg,
		logger:   opt.logger,
		meter:    opt.meter,
		leases:   make(map[string]clientv3.LeaseID),
		keepAlives: make(map[string]*leaseKeepAlive),
		watchers: make(map[string]context.CancelFunc),
		cache:    make(map[string][]*ServiceInstance),
		stopChan: make(chan struct{}),
	}

	// 创建 resolver builder
	r.resolverBuilder = newEtcdResolverBuilder(r, cfg.Schema)

	// 注册 gRPC resolver
	resolver.Register(r.resolverBuilder)

	return r, nil
}

// leaseKeepAlive 租约保活信息
type leaseKeepAlive struct {
	leaseID     clientv3.LeaseID
	keepAliveCh <-chan *clientv3.LeaseKeepAliveResponse
	cancel      context.CancelFunc
	serviceID   string
	serviceName string
}

// etcdRegistry 基于 Etcd 的服务注册发现实现
type etcdRegistry struct {
	client *clientv3.Client
	cfg    *Config
	logger clog.Logger
	meter  metrics.Meter

	// 后台任务管理
	leases      map[string]clientv3.LeaseID   // serviceID -> leaseID (废弃，保留用于兼容)
	keepAlives  map[string]*leaseKeepAlive    // serviceID -> keepAlive info
	watchers    map[string]context.CancelFunc // serviceName -> cancel
	cache       map[string][]*ServiceInstance // serviceName -> instances
	stopChan    chan struct{}
	wg          sync.WaitGroup
	mu          sync.RWMutex

	// resolver builder
	resolverBuilder *etcdResolverBuilder
}

// Register 注册服务实例
func (r *etcdRegistry) Register(ctx context.Context, service *ServiceInstance, ttl time.Duration) error {
	if service == nil || service.ID == "" || service.Name == "" {
		return ErrInvalidServiceInstance
	}

	if ttl == 0 {
		ttl = r.cfg.DefaultTTL
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// 检查是否已注册
	if _, exists := r.keepAlives[service.ID]; exists {
		return ErrServiceAlreadyRegistered
	}

	// 创建租约
	lease, err := r.client.Grant(ctx, int64(ttl.Seconds()))
	if err != nil {
		r.logger.Error("failed to grant lease",
			clog.String("service_id", service.ID),
			clog.Error(err))
		return xerrors.Wrap(err, "grant lease failed")
	}

	// 序列化服务实例
	value, err := json.Marshal(service)
	if err != nil {
		if _, revokeErr := r.client.Revoke(ctx, lease.ID); revokeErr != nil {
			r.logger.Error("failed to revoke lease",
				clog.String("leaseID", fmt.Sprintf("%d", lease.ID)),
				clog.Error(revokeErr))
		}
		return xerrors.Wrap(err, "marshal service failed")
	}

	// 生成 key
	key := r.buildKey(service.Name, service.ID)

	// 写入 Etcd
	_, err = r.client.Put(ctx, key, string(value), clientv3.WithLease(lease.ID))
	if err != nil {
		if _, revokeErr := r.client.Revoke(ctx, lease.ID); revokeErr != nil {
			r.logger.Error("failed to revoke lease",
				clog.String("leaseID", fmt.Sprintf("%d", lease.ID)),
				clog.Error(revokeErr))
		}
		r.logger.Error("failed to put service",
			clog.String("key", key),
			clog.Error(err))
		return xerrors.Wrap(err, "put service failed")
	}

	// 启动 KeepAlive 后台协程
	keepAliveCtx, keepAliveCancel := context.WithCancel(ctx)
	keepAliveCh, err := r.client.KeepAlive(keepAliveCtx, lease.ID)
	if err != nil {
		keepAliveCancel()
		if _, revokeErr := r.client.Revoke(ctx, lease.ID); revokeErr != nil {
			r.logger.Error("failed to revoke lease",
				clog.String("leaseID", fmt.Sprintf("%d", lease.ID)),
				clog.Error(revokeErr))
		}
		return xerrors.Wrap(err, "keepalive failed")
	}

	// 保存 keepAlive 信息
	ka := &leaseKeepAlive{
		leaseID:     lease.ID,
		keepAliveCh: keepAliveCh,
		cancel:      keepAliveCancel,
		serviceID:   service.ID,
		serviceName: service.Name,
	}
	r.keepAlives[service.ID] = ka
	// 兼容旧代码，同时更新 leases map
	r.leases[service.ID] = lease.ID

	// 启动 KeepAlive 监控协程
	r.wg.Add(1)
	go r.monitorKeepAlive(ka)

	r.logger.Info("service registered",
		clog.String("service_id", service.ID),
		clog.String("service_name", service.Name),
		clog.Duration("ttl", ttl))

	return nil
}

// Deregister 注销服务实例
func (r *etcdRegistry) Deregister(ctx context.Context, serviceID string) error {
	if serviceID == "" {
		return ErrInvalidServiceInstance
	}

	r.mu.Lock()
	ka, exists := r.keepAlives[serviceID]
	if !exists {
		r.mu.Unlock()
		return ErrServiceNotFound
	}
	leaseID := ka.leaseID
	// 取消 KeepAlive 协程
	ka.cancel()
	delete(r.keepAlives, serviceID)
	delete(r.leases, serviceID)
	r.mu.Unlock()

	// 撤销租约（会自动删除关联的 key）
	if _, err := r.client.Revoke(ctx, leaseID); err != nil {
		r.logger.Error("failed to revoke lease",
			clog.String("service_id", serviceID),
			clog.Error(err))
		return xerrors.Wrap(err, "revoke lease failed")
	}

	r.logger.Info("service deregistered",
		clog.String("service_id", serviceID))

	return nil
}

// GetService 获取服务实例列表
func (r *etcdRegistry) GetService(ctx context.Context, serviceName string) ([]*ServiceInstance, error) {
	if serviceName == "" {
		return nil, ErrInvalidServiceInstance
	}

	// 如果启用缓存，先尝试从缓存获取
	if r.cfg.EnableCache {
		r.mu.RLock()
		instances, exists := r.cache[serviceName]
		r.mu.RUnlock()
		if exists && len(instances) > 0 {
			return instances, nil
		}
	}

	// 从 Etcd 查询
	prefix := r.buildPrefix(serviceName)
	resp, err := r.client.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		r.logger.Error("failed to get service",
			clog.String("service_name", serviceName),
			clog.Error(err))
		return nil, xerrors.Wrap(err, "get service failed")
	}

	var instances []*ServiceInstance
	for _, kv := range resp.Kvs {
		var instance ServiceInstance
		if err := json.Unmarshal(kv.Value, &instance); err != nil {
			r.logger.Warn("failed to unmarshal service instance",
				clog.String("key", string(kv.Key)),
				clog.Error(err))
			continue
		}
		instances = append(instances, &instance)
	}

	// 更新缓存
	if r.cfg.EnableCache && len(instances) > 0 {
		r.mu.Lock()
		r.cache[serviceName] = instances
		r.mu.Unlock()
	}

	return instances, nil
}

// Watch 监听服务实例变化
// 支持自动重连：当 watch channel 关闭或发生错误时，会自动重连
// 使用 WithRev 从上次处理的位置继续监听，避免事件丢失
func (r *etcdRegistry) Watch(ctx context.Context, serviceName string) (<-chan ServiceEvent, error) {
	if serviceName == "" {
		return nil, ErrInvalidServiceInstance
	}

	eventCh := make(chan ServiceEvent, 100)
	prefix := r.buildPrefix(serviceName)

	watchCtx, cancel := context.WithCancel(ctx)

	// 保存 cancel 函数
	r.mu.Lock()
	r.watchers[serviceName] = cancel
	r.mu.Unlock()

	// 启动 watch goroutine
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer close(eventCh)

		var lastRev int64 = 0
		retryInterval := r.cfg.RetryInterval
		if retryInterval == 0 {
			retryInterval = 1 * time.Second
		}

		// 外层循环：处理重连
		for {
			// 构建 watch 选项
			watchOpts := []clientv3.OpOption{clientv3.WithPrefix()}
			if lastRev > 0 {
				// 从上次处理的 revision 之后开始监听
				watchOpts = append(watchOpts, clientv3.WithRev(lastRev+1))
			}

			// 创建 watcher
			watchCh := r.client.Watch(watchCtx, prefix, watchOpts...)

			r.logger.Debug("watch started",
				clog.String("service_name", serviceName),
				clog.Int64("from_revision", lastRev+1))

			// 内层循环：处理 watch 事件
			for watchCh != nil {
				select {
				case <-watchCtx.Done():
					r.logger.Debug("watch stopped by context",
						clog.String("service_name", serviceName))
					return

				case wresp, ok := <-watchCh:
					if !ok {
						// watch channel 关闭，需要重连
						r.logger.Warn("watch channel closed, will retry",
							clog.String("service_name", serviceName),
							clog.Duration("retry_after", retryInterval))
						watchCh = nil
						// 继续外层循环进行重连
						goto reconnect
					}

					if wresp.Err() != nil {
						r.logger.Error("watch error, will retry",
							clog.String("service_name", serviceName),
							clog.Error(wresp.Err()),
							clog.Duration("retry_after", retryInterval))
						watchCh = nil
						// 继续外层循环进行重连
						goto reconnect
					}

					// 处理事件
					for _, ev := range wresp.Events {
						// 更新最后处理的 revision
						if ev.Kv.ModRevision > lastRev {
							lastRev = ev.Kv.ModRevision
						}

						var event ServiceEvent
						var instance ServiceInstance

						switch ev.Type {
						case clientv3.EventTypePut:
							// PUT 事件：反序列化服务实例
							if err := json.Unmarshal(ev.Kv.Value, &instance); err != nil {
								r.logger.Warn("failed to unmarshal watch event",
									clog.String("key", string(ev.Kv.Key)),
									clog.Error(err))
								continue
							}
							event = ServiceEvent{
								Type:    EventTypePut,
								Service: &instance,
							}
							// 更新缓存（带 revision 版本控制）
							if r.cfg.EnableCache {
								r.updateCache(serviceName, &instance, true, ev.Kv.ModRevision)
							}

						case clientv3.EventTypeDelete:
							// DELETE 事件：从 key 中提取服务 ID
							// Key 格式: /namespace/service_name/instance_id
							keyParts := strings.Split(string(ev.Kv.Key), "/")
							if len(keyParts) > 0 {
								instance.ID = keyParts[len(keyParts)-1]
								instance.Name = serviceName
							}
							event = ServiceEvent{
								Type:    EventTypeDelete,
								Service: &instance,
							}
							// 更新缓存（带 revision 版本控制）
							if r.cfg.EnableCache {
								r.updateCache(serviceName, &instance, false, ev.Kv.ModRevision)
							}
						}

						// 发送事件
						select {
						case eventCh <- event:
						case <-watchCtx.Done():
							return
						}
					}
				}
			}

		reconnect:
			// 检查是否应该退出
			select {
			case <-watchCtx.Done():
				return
			default:
				// 等待后重连（指数退避）
				r.logger.Info("retrying watch",
					clog.String("service_name", serviceName),
					clog.Duration("after", retryInterval))
				time.Sleep(retryInterval)
			}
		}
	}()

	return eventCh, nil
}

// GetConnection 获取到指定服务的 gRPC 连接
func (r *etcdRegistry) GetConnection(ctx context.Context, serviceName string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	// 使用 resolver builder 创建连接
	target := fmt.Sprintf("%s:///%s", r.cfg.Schema, serviceName)

	// 合并默认选项
	defaultOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy":"round_robin"}`), // 使用轮询负载均衡
	}
	opts = append(defaultOpts, opts...)

	conn, err := grpc.NewClient(target, opts...)
	if err != nil {
		r.logger.Error("failed to create grpc connection",
			clog.String("service_name", serviceName),
			clog.Error(err))
		return nil, xerrors.Wrap(err, "dial failed")
	}

	return conn, nil
}

// Close 停止后台任务并清理资源（撤销租约、停止监听）
// 此方法是幂等的，可以安全地多次调用
func (r *etcdRegistry) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 使用 sync.Once 确保 stopChan 只关闭一次
	r.mu.Lock()
	select {
	case <-r.stopChan:
		// 已经关闭过，直接返回
		r.mu.Unlock()
		return nil
	default:
		// 第一次关闭，继续执行
	}
	close(r.stopChan)
	r.mu.Unlock()

	// 取消所有 watchers
	r.mu.Lock()
	for serviceName, cancelFunc := range r.watchers {
		cancelFunc()
		delete(r.watchers, serviceName)
	}
	// 取消所有 KeepAlive 协程
	for serviceID, ka := range r.keepAlives {
		ka.cancel()
		delete(r.keepAlives, serviceID)
		delete(r.leases, serviceID)
	}
	r.mu.Unlock()

	// 撤销所有租约
	r.mu.Lock()
	for serviceID, leaseID := range r.leases {
		if _, err := r.client.Revoke(ctx, leaseID); err != nil {
			r.logger.Warn("failed to revoke lease during shutdown",
				clog.String("service_id", serviceID),
				clog.Error(err))
		}
	}
	r.leases = make(map[string]clientv3.LeaseID)
	r.mu.Unlock()

	// 等待所有 goroutine 结束
	r.wg.Wait()

	r.logger.Info("registry stopped")
	return nil
}

// buildKey 构建存储键
func (r *etcdRegistry) buildKey(serviceName, serviceID string) string {
	return fmt.Sprintf("%s/%s/%s", r.cfg.Namespace, serviceName, serviceID)
}

// monitorKeepAlive 监控租约续约
// 该协程会持续监听 KeepAlive 响应，当 channel 关闭时表示租约失效或网络中断
func (r *etcdRegistry) monitorKeepAlive(ka *leaseKeepAlive) {
	defer r.wg.Done()

	serviceID := ka.serviceID
	serviceName := ka.serviceName
	leaseID := ka.leaseID

	r.logger.Debug("keepalive monitor started",
		clog.String("service_id", serviceID),
		clog.Int64("lease_id", int64(leaseID)))

	for {
		select {
		case <-r.stopChan:
			r.logger.Debug("keepalive monitor stopped by stopChan",
				clog.String("service_id", serviceID))
			return

		case kaResp, ok := <-ka.keepAliveCh:
			if !ok {
				// KeepAlive channel 关闭，表示租约失效或 Etcd 连接断开
				r.logger.Error("keepalive channel closed, lease expired or connection lost",
					clog.String("service_id", serviceID),
					clog.String("service_name", serviceName),
					clog.Int64("lease_id", int64(leaseID)))

				// 从 keepAlives map 中移除
				r.mu.Lock()
				delete(r.keepAlives, serviceID)
				delete(r.leases, serviceID)
				r.mu.Unlock()

				// 注意：此处不尝试重新注册，因为：
				// 1. 如果是租约 TTL 过期，说明服务进程可能已异常退出
				// 2. 如果是网络中断，Etcd 客户端会在重连后自动恢复
				// 3. 重新注册可能导致"僵尸实例"问题
				// 用户可以通过监控此日志来触发告警
				return
			}

			// 记录续约成功（仅 Debug 级别，避免日志过多）
			r.logger.Debug("keepalive renewed",
				clog.String("service_id", serviceID),
				clog.String("service_name", serviceName),
				clog.Int64("lease_id", int64(kaResp.ID)),
				clog.Int64("ttl", kaResp.TTL))
		}
	}
}

// buildPrefix 构建前缀
func (r *etcdRegistry) buildPrefix(serviceName string) string {
	return fmt.Sprintf("%s/%s/", r.cfg.Namespace, serviceName)
}

// updateCache 更新缓存（带版本控制）
// revision: Etcd ModRevision，用于判断事件的新旧，避免旧数据覆盖新数据
func (r *etcdRegistry) updateCache(serviceName string, instance *ServiceInstance, isAdd bool, revision int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	instances, exists := r.cache[serviceName]
	if !exists {
		if isAdd {
			r.cache[serviceName] = []*ServiceInstance{instance}
		}
		return
	}

	if isAdd {
		// 检查是否已存在
		for i, inst := range instances {
			if inst.ID == instance.ID {
				// 如果已有实例，直接更新（Etcd 已保证 revision 顺序）
				instances[i] = instance
				return
			}
		}
		r.cache[serviceName] = append(instances, instance)
	} else {
		// 删除
		newInstances := make([]*ServiceInstance, 0, len(instances))
		for _, inst := range instances {
			if inst.ID != instance.ID {
				newInstances = append(newInstances, inst)
			}
		}
		r.cache[serviceName] = newInstances
	}
}
