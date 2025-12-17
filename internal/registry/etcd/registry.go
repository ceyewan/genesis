package etcd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/connector"
	metrics "github.com/ceyewan/genesis/pkg/metrics"
	"github.com/ceyewan/genesis/pkg/registry/types"
	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/resolver"
)

// EtcdRegistry 基于 Etcd 的服务注册发现实现
type EtcdRegistry struct {
	client *clientv3.Client
	cfg    types.Config
	logger clog.Logger
	meter  metrics.Meter

	// 后台任务管理
	leases   map[string]clientv3.LeaseID         // serviceID -> leaseID
	watchers map[string]context.CancelFunc       // serviceName -> cancel
	cache    map[string][]*types.ServiceInstance // serviceName -> instances
	stopChan chan struct{}
	wg       sync.WaitGroup
	mu       sync.RWMutex

	// resolver builder
	resolverBuilder *etcdResolverBuilder
}

// New 创建 EtcdRegistry 实例
func New(
	conn connector.EtcdConnector,
	cfg types.Config,
	logger clog.Logger,
	meter metrics.Meter,
) (*EtcdRegistry, error) {
	if conn == nil {
		return nil, fmt.Errorf("etcd connector cannot be nil")
	}

	client := conn.GetClient()
	if client == nil {
		return nil, fmt.Errorf("etcd client cannot be nil")
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

	if logger == nil {
		logger = clog.Default()
	}

	r := &EtcdRegistry{
		client:   client,
		cfg:      cfg,
		logger:   logger,
		meter:    meter,
		leases:   make(map[string]clientv3.LeaseID),
		watchers: make(map[string]context.CancelFunc),
		cache:    make(map[string][]*types.ServiceInstance),
		stopChan: make(chan struct{}),
	}

	// 创建 resolver builder
	r.resolverBuilder = newEtcdResolverBuilder(r, cfg.Schema)

	// 注册 gRPC resolver
	resolver.Register(r.resolverBuilder)

	return r, nil
}

// Register 注册服务实例
func (r *EtcdRegistry) Register(ctx context.Context, service *types.ServiceInstance, ttl time.Duration) error {
	if service == nil || service.ID == "" || service.Name == "" {
		return types.ErrInvalidServiceInstance
	}

	if ttl == 0 {
		ttl = r.cfg.DefaultTTL
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// 检查是否已注册
	if _, exists := r.leases[service.ID]; exists {
		return types.ErrServiceAlreadyRegistered
	}

	// 创建租约
	lease, err := r.client.Grant(ctx, int64(ttl.Seconds()))
	if err != nil {
		r.logger.Error("failed to grant lease",
			clog.String("service_id", service.ID),
			clog.Error(err))
		return fmt.Errorf("grant lease failed: %w", err)
	}

	// 序列化服务实例
	value, err := json.Marshal(service)
	if err != nil {
		r.client.Revoke(ctx, lease.ID)
		return fmt.Errorf("marshal service failed: %w", err)
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
		return fmt.Errorf("put service failed: %w", err)
	}

	// 保存 lease ID
	r.leases[service.ID] = lease.ID

	// 启动 KeepAlive
	r.wg.Add(1)
	go r.keepAlive(service.ID, lease.ID)

	r.logger.Info("service registered",
		clog.String("service_id", service.ID),
		clog.String("service_name", service.Name),
		clog.Duration("ttl", ttl))

	return nil
}

// Deregister 注销服务实例
func (r *EtcdRegistry) Deregister(ctx context.Context, serviceID string) error {
	if serviceID == "" {
		return types.ErrInvalidServiceInstance
	}

	r.mu.Lock()
	leaseID, exists := r.leases[serviceID]
	if !exists {
		r.mu.Unlock()
		return types.ErrServiceNotFound
	}
	delete(r.leases, serviceID)
	r.mu.Unlock()

	// 撤销租约（会自动删除关联的 key）
	if _, err := r.client.Revoke(ctx, leaseID); err != nil {
		r.logger.Error("failed to revoke lease",
			clog.String("service_id", serviceID),
			clog.Error(err))
		return fmt.Errorf("revoke lease failed: %w", err)
	}

	r.logger.Info("service deregistered",
		clog.String("service_id", serviceID))

	return nil
}

// GetService 获取服务实例列表
func (r *EtcdRegistry) GetService(ctx context.Context, serviceName string) ([]*types.ServiceInstance, error) {
	if serviceName == "" {
		return nil, types.ErrInvalidServiceInstance
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

	// 从 Etcd 获取
	prefix := r.buildPrefix(serviceName)
	resp, err := r.client.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		r.logger.Error("failed to get service",
			clog.String("service_name", serviceName),
			clog.Error(err))
		return nil, fmt.Errorf("get service failed: %w", err)
	}

	var instances []*types.ServiceInstance
	for _, kv := range resp.Kvs {
		var instance types.ServiceInstance
		if err := json.Unmarshal(kv.Value, &instance); err != nil {
			r.logger.Warn("failed to unmarshal service instance",
				clog.String("key", string(kv.Key)),
				clog.Error(err))
			continue
		}
		instances = append(instances, &instance)
	}

	// 更新缓存
	if r.cfg.EnableCache {
		r.mu.Lock()
		r.cache[serviceName] = instances
		r.mu.Unlock()
	}

	return instances, nil
}

// Watch 监听服务实例变化
func (r *EtcdRegistry) Watch(ctx context.Context, serviceName string) (<-chan types.ServiceEvent, error) {
	if serviceName == "" {
		return nil, types.ErrInvalidServiceInstance
	}

	eventCh := make(chan types.ServiceEvent, 100)
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
		defer func() {
			r.mu.Lock()
			delete(r.watchers, serviceName)
			r.mu.Unlock()
		}()

		watchChan := r.client.Watch(watchCtx, prefix, clientv3.WithPrefix())
		for {
			select {
			case <-watchCtx.Done():
				return
			case <-r.stopChan:
				return
			case wresp, ok := <-watchChan:
				if !ok {
					return
				}
				if wresp.Err() != nil {
					r.logger.Error("watch error",
						clog.String("service_name", serviceName),
						clog.Error(wresp.Err()))
					continue
				}

				for _, ev := range wresp.Events {
					var event types.ServiceEvent
					var instance types.ServiceInstance

					switch ev.Type {
					case clientv3.EventTypePut:
						// PUT 事件：反序列化服务实例
						if err := json.Unmarshal(ev.Kv.Value, &instance); err != nil {
							r.logger.Warn("failed to unmarshal watch event",
								clog.Error(err))
							continue
						}
						event = types.ServiceEvent{
							Type:    types.EventTypePut,
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
						event = types.ServiceEvent{
							Type:    types.EventTypeDelete,
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
					case <-r.stopChan:
						return
					}
				}
			}
		}
	}()

	return eventCh, nil
}

// GetConnection 获取到指定服务的 gRPC 连接
func (r *EtcdRegistry) GetConnection(ctx context.Context, serviceName string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	target := fmt.Sprintf("%s:///%s", r.cfg.Schema, serviceName)

	// 添加默认选项
	defaultOpts := []grpc.DialOption{
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy":"round_robin"}`),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}

	// 合并选项
	allOpts := append(defaultOpts, opts...)

	conn, err := grpc.DialContext(ctx, target, allOpts...)
	if err != nil {
		r.logger.Error("failed to create grpc connection",
			clog.String("service_name", serviceName),
			clog.Error(err))
		return nil, fmt.Errorf("dial failed: %w", err)
	}

	return conn, nil
}

// Start 启动后台任务
func (r *EtcdRegistry) Start(ctx context.Context) error {
	r.logger.Info("starting registry service")
	return nil
}

// Stop 停止后台任务并清理资源
func (r *EtcdRegistry) Stop(ctx context.Context) error {
	r.logger.Info("stopping registry service")

	close(r.stopChan)

	// 停止所有 watchers
	r.mu.Lock()
	for _, cancel := range r.watchers {
		cancel()
	}
	r.watchers = make(map[string]context.CancelFunc)

	// 撤销所有 leases
	for serviceID, leaseID := range r.leases {
		if _, err := r.client.Revoke(ctx, leaseID); err != nil {
			r.logger.Error("failed to revoke lease on stop",
				clog.String("service_id", serviceID),
				clog.Error(err))
		}
	}
	r.leases = make(map[string]clientv3.LeaseID)
	r.mu.Unlock()

	r.wg.Wait()

	r.logger.Info("registry service stopped")
	return nil
}

// Phase 返回启动阶段
func (r *EtcdRegistry) Phase() int {
	return 20 // 与其他业务组件一致
}

// keepAlive 保持租约活跃
func (r *EtcdRegistry) keepAlive(serviceID string, leaseID clientv3.LeaseID) {
	defer r.wg.Done()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	keepAliveCh, err := r.client.KeepAlive(ctx, leaseID)
	if err != nil {
		r.logger.Error("failed to start keepalive",
			clog.String("service_id", serviceID),
			clog.Error(err))
		return
	}

	for {
		select {
		case <-r.stopChan:
			return
		case resp, ok := <-keepAliveCh:
			if !ok {
				r.logger.Warn("keepalive channel closed",
					clog.String("service_id", serviceID))
				return
			}
			if resp == nil {
				r.logger.Warn("keepalive response is nil",
					clog.String("service_id", serviceID))
				return
			}
		}
	}
}

// updateCache 更新缓存
func (r *EtcdRegistry) updateCache(serviceName string, instance *types.ServiceInstance, isAdd bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	instances, exists := r.cache[serviceName]
	if !exists {
		if isAdd {
			r.cache[serviceName] = []*types.ServiceInstance{instance}
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
		newInstances := make([]*types.ServiceInstance, 0, len(instances))
		for _, inst := range instances {
			if inst.ID != instance.ID {
				newInstances = append(newInstances, inst)
			}
		}
		r.cache[serviceName] = newInstances
	}
}

// buildKey 构建服务实例的完整 key
func (r *EtcdRegistry) buildKey(serviceName, instanceID string) string {
	return fmt.Sprintf("%s/%s/%s", r.cfg.Namespace, serviceName, instanceID)
}

// buildPrefix 构建服务的前缀
func (r *EtcdRegistry) buildPrefix(serviceName string) string {
	return fmt.Sprintf("%s/%s/", r.cfg.Namespace, serviceName)
}
