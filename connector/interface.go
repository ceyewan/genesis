// Package connector 为 Genesis 框架提供统一的连接管理能力。
// 支持多种数据源的连接池管理、健康检查和故障恢复。
//
// 特性：
//   - 统一接口：提供 Connector、TypedConnector 泛型接口
//   - 多数据源支持：Redis、MySQL、Etcd、NATS
//   - 连接池管理：自动管理连接生命周期
//   - 健康检查：定期检查连接状态，自动故障恢复
//   - 配置驱动：基于配置文件的连接参数管理
//   - 指标集成：集成 OpenTelemetry 指标收集
//
// 基本使用：
//
//	// 创建 Redis 连接器
//	cfg := &connector.RedisConfig{
//		Addr:     "127.0.0.1:6379",
//		Password: "",
//		DB:       0,
//	}
//	redisConn, err := connector.NewRedis(cfg,
//		connector.WithLogger(logger),
//		connector.WithMeter(meter),
//	)
//	if err != nil {
//		panic(err)
//	}
//	defer redisConn.Close()
//
//	// 建立连接
//	if err := redisConn.Connect(ctx); err != nil {
//		panic(err)
//	}
//
//	// 获取客户端
//	client := redisConn.GetClient()
//	result, err := client.Get(ctx, "key").Result()
//
// 设计理念：
// connector 遵循"接口优先"的设计原则，提供统一的连接管理抽象。
// 通过泛型接口确保类型安全，同时支持不同数据源的特殊配置需求。
// 采用显式依赖注入，确保组件间的松耦合和可测试性。
package connector

import (
	"context"

	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"github.com/twmb/franz-go/pkg/kgo"
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

// DatabaseConnector 数据库连接器接口（MySQL 和 SQLite 共用）
type DatabaseConnector interface {
	Connector
	GetClient() *gorm.DB
}

// RedisConnector Redis 连接器接口
type RedisConnector interface {
	TypedConnector[*redis.Client]
}

// MySQLConnector MySQL 连接器接口
type MySQLConnector interface {
	DatabaseConnector
}

// SQLiteConnector SQLite 连接器接口
type SQLiteConnector interface {
	DatabaseConnector
}

// EtcdConnector Etcd 连接器接口
type EtcdConnector interface {
	TypedConnector[*clientv3.Client]
}

// NATSConnector NATS 连接器接口
type NATSConnector interface {
	TypedConnector[*nats.Conn]
}

// KafkaConnector Kafka 连接器接口
type KafkaConnector interface {
	TypedConnector[*kgo.Client]
	Config() *KafkaConfig
}
