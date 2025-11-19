// pkg/connector/interface.go
package connector

import (
	"context"

	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	clientv3 "go.etcd.io/etcd/client/v3"
	"gorm.io/gorm"
)

// Lifecycle 定义了可由容器管理生命周期的对象的行为
type Lifecycle interface {
	// Start 启动服务，Phase 越小越先启动
	Start(ctx context.Context) error
	// Stop 关闭服务，按启动的逆序调用
	Stop(ctx context.Context) error
	// Phase 返回启动阶段，用于排序
	Phase() int
}

// Connector 是所有连接器的基础接口
type Connector interface {
	Lifecycle // 继承生命周期接口
	// Connect 负责建立连接，应幂等且并发安全
	Connect(ctx context.Context) error
	// Close 负责关闭连接，释放资源
	Close() error
	// HealthCheck 检查连接的健康状态
	HealthCheck(ctx context.Context) error
	// IsHealthy 返回一个缓存的、快速的健康状态标志
	IsHealthy() bool
	// Name 返回此连接实例的唯一名称（如：mysql.primary）
	Name() string
}

// TypedConnector 是一个泛型接口，用于提供类型安全的客户端访问
type TypedConnector[T any] interface {
	Connector
	GetClient() T
}

// Configurable 定义了配置验证的能力
type Configurable interface {
	Validate() error
}

// Reloadable 定义了热重载配置的能力 (可选实现)
type Reloadable interface {
	Reload(ctx context.Context, newConfig Configurable) error
}

// MySQLConnector 是 MySQL 连接器的具体接口定义
type MySQLConnector interface {
	TypedConnector[*gorm.DB]
	Configurable
}

// RedisConnector 是 Redis 连接器的具体接口定义
type RedisConnector interface {
	TypedConnector[*redis.Client]
	Configurable
}

// EtcdConnector 是 Etcd 连接器的具体接口定义
type EtcdConnector interface {
	TypedConnector[*clientv3.Client]
	Configurable
}

// NATSConnector 是 NATS 连接器的具体接口定义
type NATSConnector interface {
	TypedConnector[*nats.Conn]
	Configurable
}
