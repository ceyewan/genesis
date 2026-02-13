// Package connector 为 Genesis 框架提供统一的连接管理能力。
//
// 核心特性：
//   - 统一抽象：通过 Connector 接口提供一致的连接管理 API
//   - 类型安全：通过 TypedConnector[T] 泛型接口确保编译时类型检查
//   - 多数据源支持：Redis、MySQL、SQLite、Etcd、NATS、Kafka
//   - 健康检查：定期检查连接状态，支持自动故障恢复
//   - 并发安全：所有公开方法均为并发安全，支持多协程同时访问
//   - 资源管理：遵循"谁创建，谁负责释放"原则，Close() 应在应用层调用
//
// 设计理念：
//   - 接口优先：定义清晰的接口契约，实现细节可替换
//   - 显式依赖注入：通过构造函数注入依赖，避免全局状态
//   - 幂等连接：Connect() 方法可安全重复调用
//   - 延迟连接：NewXXX() 创建连接器但不立即建立连接，Connect() 时才连接
//
// 基本使用：
//
//	cfg := &connector.RedisConfig{
//		Addr:     "127.0.0.1:6379",
//		Password: "",
//		DB:       0,
//	}
//	conn, err := connector.NewRedis(cfg, connector.WithLogger(logger))
//	if err != nil {
//		panic(err)
//	}
//	defer conn.Close()
//
//	// 建立连接（幂等，可多次调用）
//	if err := conn.Connect(ctx); err != nil {
//		panic(err)
//	}
//
//	// 获取类型安全的客户端
//	client := conn.GetClient()
//	result, err := client.Get(ctx, "key").Result()
//
// 资源所有权：
//
//	Connector 拥有底层连接的生命周期，应通过 defer 确保 Close() 被调用。
//	Component（如 cache、dlock）仅借用 Connector，不应调用 Close()。
//	应用层应按照 LIFO 顺序释放资源：先关闭依赖 Connector 的组件，再关闭 Connector。
package connector

import (
	"context"

	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"github.com/twmb/franz-go/pkg/kgo"
	clientv3 "go.etcd.io/etcd/client/v3"
	"gorm.io/gorm"
)

// =============================================================================
// 基础接口
// =============================================================================

// Connector 定义所有连接器的通用行为。
//
// 所有连接器必须实现此接口，确保一致的连接管理体验。
// 接口方法均为并发安全，可从多个协程同时调用。
type Connector interface {
	// Connect 建立连接。
	//
	// 此方法是幂等的，可安全多次调用。首次调用时建立连接，
	// 后续调用直接返回 nil。连接过程阻塞直到成功或失败。
	//
	// 返回错误：
	//   - ErrConnection: 连接建立失败
	//   - ErrConfig: 配置无效
	Connect(ctx context.Context) error

	// Close 关闭连接并释放资源。
	//
	// 此方法是幂等的，可安全多次调用。关闭后，
	// GetClient() 将返回 nil，HealthCheck() 将返回 ErrClientNil。
	//
	// 重要：应在应用层通过 defer 确保调用，遵循"谁创建，谁负责释放"原则。
	Close() error

	// HealthCheck 检查连接健康状态。
	//
	// 通过发送测试请求验证连接可用性。此方法会更新内部健康状态缓存，
	// 可通过 IsHealthy() 快速读取。
	//
	// 返回错误：
	//   - ErrClientNil: 客户端未初始化或已关闭
	//   - ErrHealthCheck: 健康检查失败
	HealthCheck(ctx context.Context) error

	// IsHealthy 返回缓存的健康状态。
	//
	// 此方法无阻塞，直接返回最后一次 HealthCheck() 的结果。
	// 对于实时健康检查，应使用 HealthCheck() 方法。
	IsHealthy() bool

	// Name 返回连接实例名称。
	//
	// 名称用于日志记录和指标标识，应在配置中唯一标识此连接器实例。
	Name() string
}

// =============================================================================
// 泛型接口
// =============================================================================

// TypedConnector 提供类型安全的客户端访问。
//
// 此接口组合了 Connector 基础接口，并添加了 GetClient() 方法
// 用于获取特定类型的客户端。所有具体连接器接口都应基于此定义。
//
// 类型参数 T 是客户端类型，如 *redis.Client、*gorm.DB 等。
type TypedConnector[T any] interface {
	Connector

	// GetClient 返回底层客户端实例。
	//
	// 调用者应通过此客户端执行实际的数据操作。
	// 注意：在 Connect() 之前或 Close() 之后调用可能返回 nil。
	GetClient() T
}

// =============================================================================
// 具体连接器接口
// =============================================================================

// RedisConnector Redis 连接器接口。
//
// 提供对 Redis 服务器的连接管理，支持连接池、Pipeline、事务等特性。
type RedisConnector interface {
	TypedConnector[*redis.Client]
}

// MySQLConnector MySQL 连接器接口。
//
// 提供对 MySQL 数据库的连接管理，基于 GORM ORM 框架。
// 支持连接池、预处理缓存、自动重连等特性。
type MySQLConnector interface {
	TypedConnector[*gorm.DB]
}

// PostgreSQLConnector PostgreSQL 连接器接口。
//
// 提供对 PostgreSQL 数据库的连接管理，基于 GORM ORM 框架。
// 支持高级数据类型（JSONB、ARRAY、GIS）、复杂查询、全文搜索等企业级特性。
type PostgreSQLConnector interface {
	TypedConnector[*gorm.DB]
}

// SQLiteConnector SQLite 连接器接口。
//
// 提供对 SQLite 数据库的连接管理，基于 GORM ORM 框架。
// 支持内存数据库和文件数据库，适合测试和嵌入式场景。
type SQLiteConnector interface {
	TypedConnector[*gorm.DB]
}

// EtcdConnector Etcd 连接器接口。
//
// 提供对 Etcd 键值存储的连接管理，支持分布式锁、配置中心、服务发现等场景。
type EtcdConnector interface {
	TypedConnector[*clientv3.Client]
}

// NATSConnector NATS 连接器接口。
//
// 提供对 NATS 消息系统的连接管理，支持发布订阅、请求响应、消息队列等模式。
// 内置自动重连机制，网络故障时会自动尝试恢复连接。
type NATSConnector interface {
	TypedConnector[*nats.Conn]
}

// KafkaConnector Kafka 连接器接口。
//
// 提供对 Kafka 消息队列的连接管理，支持高吞吐的消息生产和消费。
// 基于 franz-go 客户端，提供现代的 Kafka 消费者组 API。
type KafkaConnector interface {
	TypedConnector[*kgo.Client]
}
