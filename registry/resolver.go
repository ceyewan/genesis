package registry

import (
	"context"
	"strings"
	"sync"

	"github.com/ceyewan/genesis/clog"

	"google.golang.org/grpc/resolver"
)

// etcdResolverBuilder 实现 gRPC resolver.Builder 接口
type etcdResolverBuilder struct {
	registry *etcdRegistry
	scheme   string
}

// newEtcdResolverBuilder 创建 resolver builder
func newEtcdResolverBuilder(registry *etcdRegistry, scheme string) *etcdResolverBuilder {
	return &etcdResolverBuilder{
		registry: registry,
		scheme:   scheme,
	}
}

// Build 创建 resolver
func (b *etcdResolverBuilder) Build(target resolver.Target, cc resolver.ClientConn, opts resolver.BuildOptions) (resolver.Resolver, error) {
	serviceName := target.Endpoint()
	if serviceName == "" {
		serviceName = target.URL.Path
		serviceName = strings.TrimPrefix(serviceName, "/")
	}

	r := &etcdResolver{
		registry:    b.registry,
		serviceName: serviceName,
		cc:          cc,
		closeCh:     make(chan struct{}),
		localCache:  make(map[string]resolver.Address),
	}

	// 启动 resolver
	go r.start()

	return r, nil
}

// Scheme 返回 scheme
func (b *etcdResolverBuilder) Scheme() string {
	return b.scheme
}

// etcdResolver 实现 gRPC resolver.Resolver 接口
// 使用本地缓存和增量更新机制，避免每次事件都全量拉取服务列表
type etcdResolver struct {
	registry    *etcdRegistry
	serviceName string
	cc          resolver.ClientConn
	closeCh     chan struct{}
	localCache  map[string]resolver.Address // instanceID -> Address
	cacheMu     sync.RWMutex
	initialized bool
}

// start 启动 resolver
func (r *etcdResolver) start() {
	ctx := context.Background()

	// 监听服务变化
	eventCh, err := r.registry.Watch(ctx, r.serviceName)
	if err != nil {
		r.registry.logger.Error("failed to watch service for resolver",
			clog.String("service_name", r.serviceName),
			clog.Error(err))
		return
	}

	// 初始获取服务列表（全量初始化缓存）
	r.initializeCache()

	// 持续监听变化并增量更新
	for {
		select {
		case <-r.closeCh:
			return
		case event, ok := <-eventCh:
			if !ok {
				return
			}
			// 根据事件增量更新本地缓存
			r.handleEvent(event)
		}
	}
}

// initializeCache 初始化本地缓存（全量拉取一次）
func (r *etcdResolver) initializeCache() {
	ctx := context.Background()
	instances, err := r.registry.GetService(ctx, r.serviceName)
	if err != nil {
		r.registry.logger.Error("failed to initialize resolver cache",
			clog.String("service_name", r.serviceName),
			clog.Error(err))
		return
	}

	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()

	// 清空并重建缓存
	r.localCache = make(map[string]resolver.Address)
	for _, instance := range instances {
		for _, endpoint := range instance.Endpoints {
			addr := parseEndpoint(endpoint)
			if addr != "" {
				// 使用 instanceID 作为 key，一个实例可能有多个 endpoint
				key := instance.ID + "_" + addr
				r.localCache[key] = resolver.Address{
					Addr:       addr,
					ServerName: instance.Name,
					Attributes: nil,
				}
			}
		}
	}

	r.initialized = true
	r.pushStateLocked()

	r.registry.logger.Debug("resolver cache initialized",
		clog.String("service_name", r.serviceName),
		clog.Int("count", len(r.localCache)))
}

// handleEvent 处理服务变化事件，增量更新本地缓存
func (r *etcdResolver) handleEvent(event ServiceEvent) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()

	if !r.initialized {
		// 如果尚未初始化，忽略事件等待初始化完成
		return
	}

	switch event.Type {
	case EventTypePut:
		// 服务注册或更新
		for _, endpoint := range event.Service.Endpoints {
			addr := parseEndpoint(endpoint)
			if addr != "" {
				key := event.Service.ID + "_" + addr
				r.localCache[key] = resolver.Address{
					Addr:       addr,
					ServerName: event.Service.Name,
					Attributes: nil,
				}
			}
		}
		r.registry.logger.Debug("resolver cache updated (PUT)",
			clog.String("service_name", r.serviceName),
			clog.String("instance_id", event.Service.ID))

	case EventTypeDelete:
		// 服务注销，删除该实例的所有 endpoint
		deleted := 0
		for key := range r.localCache {
			if strings.HasPrefix(key, event.Service.ID+"_") {
				delete(r.localCache, key)
				deleted++
			}
		}
		r.registry.logger.Debug("resolver cache updated (DELETE)",
			clog.String("service_name", r.serviceName),
			clog.String("instance_id", event.Service.ID),
			clog.Int("deleted", deleted))
	}

	// 推送最新状态到 gRPC
	r.pushStateLocked()
}

// pushStateLocked 推送当前状态到 gRPC（调用前必须持有 cacheMu 锁）
func (r *etcdResolver) pushStateLocked() {
	addrs := make([]resolver.Address, 0, len(r.localCache))
	for _, addr := range r.localCache {
		addrs = append(addrs, addr)
	}

	// 如果地址列表为空，不更新状态（避免导致连接完全中断）
	// 这是 gRPC resolver 的常见做法：保留旧状态直到有新地址可用
	if len(addrs) == 0 {
		r.registry.logger.Warn("no available service instances in resolver cache",
			clog.String("service_name", r.serviceName))
		return
	}

	state := resolver.State{
		Addresses: addrs,
	}

	if err := r.cc.UpdateState(state); err != nil {
		r.registry.logger.Error("failed to update resolver state",
			clog.String("service_name", r.serviceName),
			clog.Error(err))
	}
}

// ResolveNow 立即重新解析（gRPC 可能会调用此方法）
// 此方法采用全量刷新，作为兜底机制
func (r *etcdResolver) ResolveNow(opts resolver.ResolveNowOptions) {
	r.initializeCache()
}

// Close 关闭 resolver
func (r *etcdResolver) Close() {
	close(r.closeCh)
}

// parseEndpoint 解析 endpoint 地址
// 支持格式: grpc://host:port, http://host:port, host:port
func parseEndpoint(endpoint string) string {
	// 移除协议前缀
	endpoint = strings.TrimPrefix(endpoint, "grpc://")
	endpoint = strings.TrimPrefix(endpoint, "http://")
	endpoint = strings.TrimPrefix(endpoint, "https://")

	return endpoint
}
