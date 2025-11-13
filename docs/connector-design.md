# 连接管理抽象层设计

## 设计目标
- 提供统一的连接管理接口，支持Redis和etcd
- 实现连接池复用，避免重复创建连接
- 支持连接健康检查和自动重连
- 提供线程安全的连接访问

## 核心接口设计

### 基础连接器接口
```go
// pkg/common/connector/connector.go
package connector

import (
    "context"
    "time"
)

// Connector 定义了所有连接器的统一接口
type Connector interface {
    // Connect 建立连接
    Connect(ctx context.Context) error
    
    // Disconnect 断开连接
    Disconnect(ctx context.Context) error
    
    // HealthCheck 检查连接健康状态
    HealthCheck(ctx context.Context) error
    
    // GetClient 返回底层客户端实例
    // 返回类型: *redis.Client 或 *etcd.Client
    GetClient() interface{}
    
    // IsConnected 返回当前连接状态
    IsConnected() bool
    
    // GetConfig 返回连接器配置
    GetConfig() Config
}

// Config 定义连接器基础配置
type Config struct {
    // 连接超时时间
    ConnectTimeout time.Duration `json:"connect_timeout" yaml:"connect_timeout"`
    
    // 健康检查间隔
    HealthCheckInterval time.Duration `json:"health_check_interval" yaml:"health_check_interval"`
    
    // 自动重连配置
    AutoReconnect     bool          `json:"auto_reconnect" yaml:"auto_reconnect"`
    ReconnectInterval time.Duration `json:"reconnect_interval" yaml:"reconnect_interval"`
    MaxReconnectAttempts int       `json:"max_reconnect_attempts" yaml:"max_reconnect_attempts"`
}

// Manager 连接管理器，负责管理所有连接器实例
type Manager interface {
    // GetConnector 获取指定类型的连接器
    GetConnector(connType Type) (Connector, error)
    
    // RegisterConnector 注册新的连接器
    RegisterConnector(connType Type, connector Connector) error
    
    // RemoveConnector 移除连接器
    RemoveConnector(connType Type) error
    
    // HealthCheck 检查所有连接器的健康状态
    HealthCheck(ctx context.Context) error
    
    // Close 关闭所有连接器
    Close(ctx context.Context) error
}

// Type 定义连接器类型
type Type string

const (
    TypeRedis Type = "redis"
    TypeEtcd  Type = "etcd"
)
```

### Redis连接器配置
```go
// pkg/redis/connector/config.go
package connector

import (
    "time"
    "github.com/ceyewan/genesis/pkg/common/connector"
)

// RedisConfig Redis连接器配置
type RedisConfig struct {
    connector.Config
    
    // Redis特定配置
    Addr         string   `json:"addr" yaml:"addr"`                   // 地址，如 "localhost:6379"
    Password     string   `json:"password" yaml:"password"`           // 密码
    DB           int      `json:"db" yaml:"db"`                       // 数据库编号
    MaxRetries   int      `json:"max_retries" yaml:"max_retries"`     // 最大重试次数
    PoolSize     int      `json:"pool_size" yaml:"pool_size"`         // 连接池大小
    MinIdleConns int      `json:"min_idle_conns" yaml:"min_idle_conns"` // 最小空闲连接数
    MaxIdleTime  time.Duration `json:"max_idle_time" yaml:"max_idle_time"` // 连接最大空闲时间
    MaxLifetime  time.Duration `json:"max_lifetime" yaml:"max_lifetime"`   // 连接最大生命周期
}
```

### etcd连接器配置
```go
// pkg/etcd/connector/config.go
package connector

import (
    "time"
    "github.com/ceyewan/genesis/pkg/common/connector"
)

// EtcdConfig etcd连接器配置
type EtcdConfig struct {
    connector.Config
    
    // etcd特定配置
    Endpoints   []string      `json:"endpoints" yaml:"endpoints"`     // 集群节点地址
    Username    string        `json:"username" yaml:"username"`       // 用户名
    Password    string        `json:"password" yaml:"password"`       // 密码
    DialTimeout time.Duration `json:"dial_timeout" yaml:"dial_timeout"` // 连接超时时间
    
    // TLS配置
    TLS TLSConfig `json:"tls" yaml:"tls"`
}

// TLSConfig TLS配置
type TLSConfig struct {
    Enabled            bool   `json:"enabled" yaml:"enabled"`
    CertFile           string `json:"cert_file" yaml:"cert_file"`
    KeyFile            string `json:"key_file" yaml:"key_file"`
    CAFile             string `json:"ca_file" yaml:"ca_file"`
    InsecureSkipVerify bool   `json:"insecure_skip_verify" yaml:"insecure_skip_verify"`
}
```

## 连接池管理策略

### 单例模式管理
```go
// pkg/common/connector/manager.go
package connector

import (
    "context"
    "fmt"
    "sync"
)

// DefaultManager 默认连接管理器实现
type DefaultManager struct {
    mu         sync.RWMutex
    connectors map[Type]Connector
    configs    map[Type]Config
}

// NewManager 创建新的连接管理器
func NewManager() Manager {
    return &DefaultManager{
        connectors: make(map[Type]Connector),
        configs:    make(map[Type]Config),
    }
}

// GetConnector 获取连接器
func (m *DefaultManager) GetConnector(connType Type) (Connector, error) {
    m.mu.RLock()
    defer m.mu.RUnlock()
    
    connector, exists := m.connectors[connType]
    if !exists {
        return nil, fmt.Errorf("connector type %s not found", connType)
    }
    
    return connector, nil
}

// RegisterConnector 注册连接器
func (m *DefaultManager) RegisterConnector(connType Type, connector Connector) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    
    if _, exists := m.connectors[connType]; exists {
        return fmt.Errorf("connector type %s already registered", connType)
    }
    
    m.connectors[connType] = connector
    m.configs[connType] = connector.GetConfig()
    
    return nil
}
```

## 健康检查机制

### 自动健康检查
```go
// 后台健康检查goroutine
func (m *DefaultManager) startHealthCheck(ctx context.Context) {
    ticker := time.NewTicker(m.healthCheckInterval)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            m.performHealthCheck(ctx)
        }
    }
}

func (m *DefaultManager) performHealthCheck(ctx context.Context) {
    m.mu.RLock()
    connectors := make([]Connector, 0, len(m.connectors))
    for _, conn := range m.connectors {
        connectors = append(connectors, conn)
    }
    m.mu.RUnlock()
    
    for _, conn := range connectors {
        if err := conn.HealthCheck(ctx); err != nil {
            // 记录错误日志，根据配置决定是否重连
            if config := conn.GetConfig(); config.AutoReconnect {
                m.handleReconnect(ctx, conn)
            }
        }
    }
}
```

## 使用示例

### 初始化连接管理器
```go
// 示例：初始化Redis和etcd连接
func InitConnectors(ctx context.Context) (connector.Manager, error) {
    manager := connector.NewManager()
    
    // 初始化Redis连接器
    redisConfig := &redis.ConnectorConfig{
        Config: connector.Config{
            ConnectTimeout:      5 * time.Second,
            HealthCheckInterval: 30 * time.Second,
            AutoReconnect:       true,
            ReconnectInterval:   5 * time.Second,
            MaxReconnectAttempts: 3,
        },
        Addr:         "localhost:6379",
        Password:     "",
        DB:           0,
        PoolSize:     10,
        MinIdleConns: 2,
    }
    
    redisConnector, err := redis.NewConnector(redisConfig)
    if err != nil {
        return nil, fmt.Errorf("failed to create redis connector: %w", err)
    }
    
    if err := manager.RegisterConnector(connector.TypeRedis, redisConnector); err != nil {
        return nil, fmt.Errorf("failed to register redis connector: %w", err)
    }
    
    // 初始化etcd连接器
    etcdConfig := &etcd.ConnectorConfig{
        Config: connector.Config{
            ConnectTimeout:      5 * time.Second,
            HealthCheckInterval: 30 * time.Second,
            AutoReconnect:       true,
            ReconnectInterval:   5 * time.Second,
            MaxReconnectAttempts: 3,
        },
        Endpoints:   []string{"localhost:2379"},
        DialTimeout: 3 * time.Second,
    }
    
    etcdConnector, err := etcd.NewConnector(etcdConfig)
    if err != nil {
        return nil, fmt.Errorf("failed to create etcd connector: %w", err)
    }
    
    if err := manager.RegisterConnector(connector.TypeEtcd, etcdConnector); err != nil {
        return nil, fmt.Errorf("failed to register etcd connector: %w", err)
    }
    
    return manager, nil
}
```

### 在组件中使用连接器
```go
// 在分布式锁实现中使用
type RedisLock struct {
    connector connector.Connector
    client    *redis.Client
}

func NewRedisLock(manager connector.Manager) (*RedisLock, error) {
    conn, err := manager.GetConnector(connector.TypeRedis)
    if err != nil {
        return nil, err
    }
    
    client := conn.GetClient().(*redis.Client)
    
    return &RedisLock{
        connector: conn,
        client:    client,
    }, nil
}
```

这个连接管理抽象层设计提供了统一的接口来管理不同类型的连接，支持连接池复用、健康检查和自动重连，为上层组件提供了稳定可靠的连接服务。