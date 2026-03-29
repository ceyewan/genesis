package registry

import (
	"context"
	"time"

	"google.golang.org/grpc"
)

// Registry 定义服务注册、服务发现和 gRPC 集成的核心能力。
type Registry interface {
	// --- 服务注册 ---

	// Register 注册服务实例。
	//
	// service.Endpoints 必须全部是 gRPC 地址，只接受 `grpc://host:port` 或 `host:port`。
	// ttl 为 0 时使用 Config.DefaultTTL；ttl 大于 0 时必须至少为 1 秒。
	Register(ctx context.Context, service *ServiceInstance, ttl time.Duration) error

	// Deregister 注销服务实例。
	Deregister(ctx context.Context, serviceID string) error

	// --- 服务发现 ---

	// GetService 获取服务实例列表。
	GetService(ctx context.Context, serviceName string) ([]*ServiceInstance, error)

	// Watch 监听服务实例变化。
	//
	// 返回的通道会发送 PUT / DELETE 事件。发生 Etcd compaction 时，registry 会回到最新快照，
	// 基于快照与本地已知状态做 diff，并补发必要事件。
	Watch(ctx context.Context, serviceName string) (<-chan ServiceEvent, error)

	// --- gRPC 集成 ---

	// GetConnection 获取指定服务的 gRPC 连接。
	//
	// 它内部封装了 resolver，并使用 gRPC 默认的 `pick_first` 负载均衡策略。只有当 ctx 带有 deadline 时，
	// 方法才会主动等待连接进入 Ready；否则仅返回已绑定 resolver 的 ClientConn。
	GetConnection(ctx context.Context, serviceName string, opts ...grpc.DialOption) (*grpc.ClientConn, error)

	// --- 资源管理 ---

	// Close 停止后台任务并清理资源。
	//
	// Close 会停止 keepalive / watch，并尽力撤销当前 registry 创建的 lease。撤销失败会返回错误。
	Close() error
}
