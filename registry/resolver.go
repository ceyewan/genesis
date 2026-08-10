package registry

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/xerrors"

	"google.golang.org/grpc/resolver"
)

const resolverScheme = "etcd"

// defaultRegistry 全局默认 Registry 实例
//
// 设计说明：
// - 用于支持 gRPC 原生 Dial 方式（如 grpc.NewClient("etcd:///service")）
// - gRPC resolver.Builder 接口无法传入自定义参数，只能通过全局变量获取 registry
//
// 约束：
// - 进程内只允许一个 active registry 实例
var (
	defaultRegistryMu sync.RWMutex
	defaultRegistry   *etcdRegistry
)

func init() {
	resolver.Register(&etcdResolverBuilder{})
}

// setDefaultRegistry 设置全局默认 registry（仅首次有效）
func setDefaultRegistry(registry *etcdRegistry) error {
	if registry == nil {
		return nil
	}
	defaultRegistryMu.Lock()
	defer defaultRegistryMu.Unlock()
	if defaultRegistry != nil && !defaultRegistry.isClosed() {
		return ErrRegistryAlreadyInitialized
	}
	defaultRegistry = registry
	return nil
}

func getDefaultRegistry() *etcdRegistry {
	defaultRegistryMu.RLock()
	defer defaultRegistryMu.RUnlock()
	return defaultRegistry
}

func clearDefaultRegistry(registry *etcdRegistry) {
	if registry == nil {
		return
	}
	defaultRegistryMu.Lock()
	defer defaultRegistryMu.Unlock()
	if defaultRegistry == registry {
		defaultRegistry = nil
	}
}

// etcdResolverBuilder 实现 gRPC resolver.Builder 接口
type etcdResolverBuilder struct{}

// Build 创建 resolver
func (b *etcdResolverBuilder) Build(target resolver.Target, cc resolver.ClientConn, opts resolver.BuildOptions) (resolver.Resolver, error) {
	registry := getDefaultRegistry()
	if registry == nil {
		return nil, xerrors.New("registry not initialized")
	}

	serviceName := target.Endpoint()
	if serviceName == "" {
		serviceName = target.URL.Path
		serviceName = strings.TrimPrefix(serviceName, "/")
	}

	ctx, cancel := context.WithCancel(context.Background())
	r := &etcdResolver{
		registry:    registry,
		serviceName: serviceName,
		cc:          cc,
		ctx:         ctx,
		cancel:      cancel,
		localCache:  make(map[resolverCacheKey]resolver.Address),
	}

	// 启动 resolver
	r.workers.Go(func() {
		r.start()
	})

	return r, nil
}

// Scheme 返回 scheme
func (b *etcdResolverBuilder) Scheme() string {
	return resolverScheme
}

// etcdResolver 实现 gRPC resolver.Resolver 接口
// 使用本地缓存和增量更新机制，避免每次事件都全量拉取服务列表
type etcdResolver struct {
	registry    *etcdRegistry
	serviceName string
	cc          resolver.ClientConn
	ctx         context.Context
	cancel      context.CancelFunc
	localCache  map[resolverCacheKey]resolver.Address
	cacheMu     sync.RWMutex
	initialized bool
	lifecycleMu sync.Mutex
	closed      bool
	workers     sync.WaitGroup
}

// resolverCacheKey keeps instance identity separate from its endpoint. String
// concatenation makes prefix-based deletion ambiguous for otherwise valid IDs
// such as "api" and "api_blue".
type resolverCacheKey struct {
	instanceID string
	addr       string
}

// start 启动 resolver
func (r *etcdResolver) start() {
	// 监听服务变化
	eventCh, err := r.registry.Watch(r.ctx, r.serviceName)
	if err != nil {
		r.registry.logger.Error("failed to watch service for resolver",
			clog.String("service_name", r.serviceName),
			clog.Error(err))
		return
	}

	// 初始获取服务列表（全量初始化缓存）
	r.initializeCache(r.ctx)

	// 持续监听变化并增量更新
	for {
		select {
		case <-r.ctx.Done():
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
func (r *etcdResolver) initializeCache(ctx context.Context) {
	instances, err := r.registry.GetService(ctx, r.serviceName)
	if err != nil {
		if !xerrors.Is(err, context.Canceled) && !xerrors.Is(err, context.DeadlineExceeded) {
			r.registry.logger.Error("failed to initialize resolver cache",
				clog.String("service_name", r.serviceName),
				clog.Error(err))
		}
		return
	}

	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()

	// 清空并重建缓存
	r.localCache = make(map[resolverCacheKey]resolver.Address)
	for _, instance := range instances {
		for _, endpoint := range instance.Endpoints {
			addr := parseGRPCEndpoint(endpoint)
			if addr != "" {
				// 使用 instanceID 作为 key，一个实例可能有多个 endpoint
				key := resolverCacheKey{instanceID: instance.ID, addr: addr}
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
		// 服务注册或更新。PUT 表示该实例的完整当前状态，必须先移除旧地址，
		// 否则 endpoint 变化后旧地址会永久残留在 resolver cache 中。
		for key := range r.localCache {
			if key.instanceID == event.Service.ID {
				delete(r.localCache, key)
			}
		}
		for _, endpoint := range event.Service.Endpoints {
			addr := parseGRPCEndpoint(endpoint)
			if addr != "" {
				key := resolverCacheKey{instanceID: event.Service.ID, addr: addr}
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
			if key.instanceID == event.Service.ID {
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
	sort.Slice(addrs, func(i, j int) bool {
		if addrs[i].Addr == addrs[j].Addr {
			return addrs[i].ServerName < addrs[j].ServerName
		}
		return addrs[i].Addr < addrs[j].Addr
	})

	if len(addrs) == 0 {
		r.registry.logger.Warn("no available service instances in resolver cache",
			clog.String("service_name", r.serviceName))
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
	r.lifecycleMu.Lock()
	if r.closed {
		r.lifecycleMu.Unlock()
		return
	}
	r.workers.Add(1)
	r.lifecycleMu.Unlock()
	defer r.workers.Done()
	r.initializeCache(r.ctx)
}

// Close 关闭 resolver
func (r *etcdResolver) Close() {
	r.lifecycleMu.Lock()
	if !r.closed {
		r.closed = true
		r.cancel()
	}
	r.lifecycleMu.Unlock()
	r.workers.Wait()
}

// parseGRPCEndpoint 解析 gRPC endpoint 地址。
// 支持格式: grpc://host:port, host:port
func parseGRPCEndpoint(endpoint string) string {
	if !isValidGRPCEndpoint(endpoint) {
		return ""
	}
	return strings.TrimPrefix(endpoint, "grpc://")
}
