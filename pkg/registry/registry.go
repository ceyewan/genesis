package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/connector"
	"github.com/ceyewan/genesis/pkg/metrics"
	"github.com/ceyewan/genesis/pkg/xerrors"
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
		opt.logger = clog.Default()
	}

	r := &etcdRegistry{
		client:   client,
		cfg:      cfg,
		logger:   opt.logger,
		meter:    opt.meter,
		tracer:   opt.tracer,
		leases:   make(map[string]clientv3.LeaseID),
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

// Must 提供初始化版本，失败时 panic
// 仅在应用启动初始化时使用，避免在业务逻辑中使用
func Must(conn connector.EtcdConnector, cfg *Config, opts ...Option) Registry {
	reg, err := New(conn, cfg, opts...)
	if err != nil {
		panic(err)
	}
	return reg
}

// etcdRegistry 基于 Etcd 的服务注册发现实现
type etcdRegistry struct {
	client *clientv3.Client
	cfg    *Config
	logger clog.Logger
	meter  metrics.Meter
	tracer interface{} // 为未来预留

	// 后台任务管理
	leases   map[string]clientv3.LeaseID   // serviceID -> leaseID
	watchers map[string]context.CancelFunc // serviceName -> cancel
	cache    map[string][]*ServiceInstance // serviceName -> instances
	stopChan chan struct{}
	wg       sync.WaitGroup
	mu       sync.RWMutex

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
	if _, exists := r.leases[service.ID]; exists {
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
		r.client.Revoke(ctx, lease.ID)
		return xerrors.Wrap(err, "marshal service failed")
	}

	// 生成 key
	key := r.buildKey(service.Name, service.ID)

	// 写入 Etcd
	_, err = r.client.Put(ctx, key, string(value), clientv3.WithLease(lease.ID))
	if err != nil {
		r.client.Revoke(ctx, lease.ID)
		r.logger.Error("failed to put service",
			clog.String("key", key),
			clog.Error(err))
		return xerrors.Wrap(err, "put service failed")
	}

	// 保存 lease ID
	r.leases[service.ID] = lease.ID

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
	leaseID, exists := r.leases[serviceID]
	if !exists {
		r.mu.Unlock()
		return ErrServiceNotFound
	}
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

		watchCh := r.client.Watch(watchCtx, prefix, clientv3.WithPrefix())
		for {
			select {
			case <-watchCtx.Done():
				return
			case wresp := <-watchCh:
				if wresp.Err() != nil {
					r.logger.Error("watch error",
						clog.String("service_name", serviceName),
						clog.Error(wresp.Err()))
					continue
				}

				for _, ev := range wresp.Events {
					var event ServiceEvent
					var instance ServiceInstance

					switch ev.Type {
					case clientv3.EventTypePut:
						// PUT 事件：反序列化服务实例
						if err := json.Unmarshal(ev.Kv.Value, &instance); err != nil {
							r.logger.Warn("failed to unmarshal watch event",
								clog.Error(err))
							continue
						}
						event = ServiceEvent{
							Type:    EventTypePut,
							Service: &instance,
						}
						// 更新缓存
						if r.cfg.EnableCache {
							r.updateCache(serviceName, &instance, true)
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
						// 更新缓存
						if r.cfg.EnableCache {
							r.updateCache(serviceName, &instance, false)
						}
					}

					select {
					case eventCh <- event:
					case <-watchCtx.Done():
						return
					}
				}
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
	}
	opts = append(defaultOpts, opts...)

	conn, err := grpc.DialContext(ctx, target, opts...)
	if err != nil {
		r.logger.Error("failed to create grpc connection",
			clog.String("service_name", serviceName),
			clog.Error(err))
		return nil, xerrors.Wrap(err, "dial failed")
	}

	return conn, nil
}

// Start 启动后台任务
func (r *etcdRegistry) Start(ctx context.Context) error {
	r.logger.Info("registry started")
	return nil
}

// Stop 停止后台任务并清理资源
func (r *etcdRegistry) Stop(ctx context.Context) error {
	// 取消所有 watchers
	r.mu.Lock()
	for serviceName, cancel := range r.watchers {
		cancel()
		delete(r.watchers, serviceName)
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

	// 关闭 stopChan
	close(r.stopChan)

	// 等待所有 goroutine 结束
	r.wg.Wait()

	r.logger.Info("registry stopped")
	return nil
}

// Phase 返回启动阶段
func (r *etcdRegistry) Phase() int {
	return 20
}

// Close 关闭组件
func (r *etcdRegistry) Close() error {
	return r.Stop(context.Background())
}

// buildKey 构建存储键
func (r *etcdRegistry) buildKey(serviceName, serviceID string) string {
	return fmt.Sprintf("%s/%s/%s", r.cfg.Namespace, serviceName, serviceID)
}

// buildPrefix 构建前缀
func (r *etcdRegistry) buildPrefix(serviceName string) string {
	return fmt.Sprintf("%s/%s/", r.cfg.Namespace, serviceName)
}

// updateCache 更新缓存
func (r *etcdRegistry) updateCache(serviceName string, instance *ServiceInstance, isAdd bool) {
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
				instances[i] = instance // 更新
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