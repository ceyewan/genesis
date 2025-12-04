// pkg/connector/interface.go
package connector

import (
	"context"

	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	clientv3 "go.etcd.io/etcd/client/v3"
	"gorm.io/gorm"
)

// Connector 基础连接器接口
type Connector interface {
	// Connect 建立连接，应幂等且并发安全
	Connect(ctx context.Context) error
	// Close 关闭连接，释放资源
	Close() error
	// HealthCheck 检查连接健康状态
	HealthCheck(ctx context.Context) error
	// IsHealthy 返回缓存的健康状态标志
	IsHealthy() bool
	// Name 返回连接实例名称
	Name() string
}

// TypedConnector 泛型接口，提供类型安全的客户端访问
type TypedConnector[T any] interface {
	Connector
	GetClient() T
}

// Configurable 定义了配置验证的能力
type Configurable interface {
	Validate() error
}

// RedisConnector Redis 连接器接口
type RedisConnector interface {
	TypedConnector[*redis.Client]
	Configurable
}

// MySQLConnector MySQL 连接器接口
type MySQLConnector interface {
	TypedConnector[*gorm.DB]
	Configurable
}

// EtcdConnector Etcd 连接器接口
type EtcdConnector interface {
	TypedConnector[*clientv3.Client]
	Configurable
}

// NATSConnector NATS 连接器接口
type NATSConnector interface {
	TypedConnector[*nats.Conn]
	Configurable
}
