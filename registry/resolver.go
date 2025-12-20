package registry

import (
	"context"
	"strings"

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
type etcdResolver struct {
	registry    *etcdRegistry
	serviceName string
	cc          resolver.ClientConn
	closeCh     chan struct{}
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

	// 初始获取服务列表
	r.updateAddresses()

	// 持续监听变化
	for {
		select {
		case <-r.closeCh:
			return
		case event, ok := <-eventCh:
			if !ok {
				return
			}
			// 收到事件后更新地址列表
			_ = event // 忽略具体事件，直接重新获取完整列表
			r.updateAddresses()
		}
	}
}

// updateAddresses 更新地址列表
func (r *etcdResolver) updateAddresses() {
	ctx := context.Background()
	instances, err := r.registry.GetService(ctx, r.serviceName)
	if err != nil {
		r.registry.logger.Error("failed to get service for resolver",
			clog.String("service_name", r.serviceName),
			clog.Error(err))
		return
	}

	var addrs []resolver.Address
	for _, instance := range instances {
		for _, endpoint := range instance.Endpoints {
			// 解析 endpoint (格式: grpc://host:port 或 host:port)
			addr := parseEndpoint(endpoint)
			if addr != "" {
				addrs = append(addrs, resolver.Address{
					Addr:       addr,
					ServerName: instance.Name,
					Attributes: nil,
				})
			}
		}
	}

	// 更新 gRPC 连接状态
	// 如果地址列表为空，不更新状态（可能是服务全部下线或程序退出）
	if len(addrs) == 0 {
		r.registry.logger.Warn("no available service instances",
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
func (r *etcdResolver) ResolveNow(opts resolver.ResolveNowOptions) {
	r.updateAddresses()
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

// serviceInstanceToAddresses 将服务实例转换为 gRPC 地址列表
func serviceInstanceToAddresses(instances []*ServiceInstance) []resolver.Address {
	var addrs []resolver.Address
	for _, instance := range instances {
		for _, endpoint := range instance.Endpoints {
			addr := parseEndpoint(endpoint)
			if addr != "" {
				addrs = append(addrs, resolver.Address{
					Addr:       addr,
					ServerName: instance.Name,
					Attributes: nil,
				})
			}
		}
	}
	return addrs
}
