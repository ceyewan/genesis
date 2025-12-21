package registry

import (
	"context"
	"time"

	"google.golang.org/grpc"
)

// Registry 服务注册与发现接口
type Registry interface {
	// --- 服务注册 ---

	// Register 注册服务实例
	// ctx: 上下文
	// service: 服务实例信息
	// ttl: 租约有效期 (例如 10s)，超时后若无续约服务将自动下线
	Register(ctx context.Context, service *ServiceInstance, ttl time.Duration) error

	// Deregister 注销服务实例
	// serviceID: 服务实例 ID
	Deregister(ctx context.Context, serviceID string) error

	// --- 服务发现 ---

	// GetService 获取服务实例列表
	// 优先读取本地缓存，缓存未命中或过期时查询注册中心
	GetService(ctx context.Context, serviceName string) ([]*ServiceInstance, error)

	// Watch 监听服务实例变化
	// 返回一个事件通道，接收服务变化事件 (PUT/DELETE)
	Watch(ctx context.Context, serviceName string) (<-chan ServiceEvent, error)

	// --- gRPC 集成 ---

	// GetConnection 获取到指定服务的 gRPC 连接
	// 内部封装了 Resolver 和 Balancer 的配置，提供开箱即用的连接对象
	// 支持自动服务发现和客户端负载均衡
	GetConnection(ctx context.Context, serviceName string, opts ...grpc.DialOption) (*grpc.ClientConn, error)

	// --- 资源管理 ---

	// Close 停止后台任务并清理资源（撤销租约、停止监听）
	Close() error
}