# etcd分布式锁优化实现建议

## 核心问题总结

基于对现有代码的分析，你的担忧完全正确：

### 1. 循环引用风险
- **原设计缺陷**：`pkg/lock/simple` 依赖 `internal/lock`，`internal/lock` 又依赖 `pkg/lock` 接口
- **架构层次混乱**：内部包和外部包相互依赖，违反分层原则

### 2. 配置混用问题
- **职责不清**：连接配置与锁行为配置混在一起
- **viper不友好**：嵌套结构复杂，不支持环境变量
- **扩展困难**：新增配置字段需要修改多个结构体

## 具体实现建议

### 建议1：重构连接管理器

**修改文件**：`internal/connector/etcd.go`

```go
package connector

import (
    "crypto/sha256"
    "encoding/hex"
    "fmt"
    "sync"
    "time"
    
    clientv3 "go.etcd.io/etcd/client/v3"
)

// ConnectionConfig 连接配置（仅连接相关，与业务无关）
type ConnectionConfig struct {
    Backend   string        `json:"backend" yaml:"backend" mapstructure:"backend"`
    Endpoints []string      `json:"endpoints" yaml:"endpoints" mapstructure:"endpoints"`
    Username  string        `json:"username" yaml:"username" mapstructure:"username"`
    Password  string        `json:"password" yaml:"password" mapstructure:"password"`
    Timeout   time.Duration `json:"timeout" yaml:"timeout" mapstructure:"timeout"`
}

// Manager 连接管理器（支持多后端，配置哈希复用）
type Manager struct {
    etcdClients map[string]*clientv3.Client  // 配置哈希 -> etcd客户端
    configs     map[string]ConnectionConfig  // 配置哈希 -> 配置
    mu          sync.RWMutex
}

var (
    globalManager *Manager
    managerOnce   sync.Once
)

// GetManager 获取全局连接管理器（单例）
func GetManager() *Manager {
    managerOnce.Do(func() {
        globalManager = &Manager{
            etcdClients: make(map[string]*clientv3.Client),
            configs:     make(map[string]ConnectionConfig),
        }
    })
    return globalManager
}

// GetEtcdClient 根据配置获取etcd客户端（自动复用）
func (m *Manager) GetEtcdClient(config ConnectionConfig) (*clientv3.Client, error) {
    // 应用默认值
    if config.Backend == "" {
        config.Backend = "etcd"
    }
    if len(config.Endpoints) == 0 {
        config.Endpoints = []string{"127.0.0.1:2379"}
    }
    if config.Timeout == 0 {
        config.Timeout = 5 * time.Second
    }

    // 计算配置哈希
    configHash := m.hashConfig(config)
    
    // 检查是否已有相同配置的客户端
    m.mu.RLock()
    if client, exists := m.etcdClients[configHash]; exists {
        m.mu.RUnlock()
        return client, nil
    }
    m.mu.RUnlock()
    
    // 创建新客户端
    m.mu.Lock()
    defer m.mu.Unlock()
    
    // 双重检查，避免并发重复创建
    if client, exists := m.etcdClients[configHash]; exists {
        return client, nil
    }
    
    // 创建新客户端
    client, err := m.createEtcdClient(config)
    if err != nil {
        return nil, err
    }
    
    // 缓存客户端
    m.etcdClients[configHash] = client
    m.configs[configHash] = config
    
    return client, nil
}

// hashConfig 计算配置哈希（用于连接复用判断）
func (m *Manager) hashConfig(config ConnectionConfig) string {
    h := sha256.New()
    
    // 关键配置字段参与哈希
    h.Write([]byte(config.Backend))
    for _, endpoint := range config.Endpoints {
        h.Write([]byte(endpoint))
    }
    h.Write([]byte(config.Username))
    h.Write([]byte(config.Password))
    h.Write([]byte(config.Timeout.String()))
    
    return hex.EncodeToString(h.Sum(nil))
}

// createEtcdClient 创建etcd客户端
func (m *Manager) createEtcdClient(config ConnectionConfig) (*clientv3.Client, error) {
    clientConfig := clientv3.Config{
        Endpoints:   config.Endpoints,
        Username:    config.Username,
        Password:    config.Password,
        DialTimeout: config.Timeout,
    }
    
    return clientv3.New(clientConfig)
}
```

### 建议2：扩展内部锁实现

**修改文件**：`internal/lock/etcd.go`

在文件末尾添加：

```go
// NewEtcdLockerWithClient 使用现有客户端创建锁（支持连接复用）
func NewEtcdLockerWithClient(client *clientv3.Client, opts *lock.LockOptions) (*EtcdLocker, error) {
    if opts == nil {
        opts = lock.DefaultLockOptions()
    }
    
    // 创建session，用于租约管理
    session, err := concurrency.NewSession(client, concurrency.WithTTL(int(opts.TTL.Seconds())))
    if err != nil {
        return nil, fmt.Errorf("failed to create session: %w", err)
    }
    
    return &EtcdLocker{
        client:  client,
        session: session,
        options: opts,
        locks:   make(map[string]*concurrency.Mutex),
    }, nil
}
```

### 建议3：创建新的简单API层

**创建目录**：`pkg/lock/simple/`

**创建文件**：`pkg/lock/simple/config.go`

```go
package simple

import (
    "time"
)

// Config 统一配置（viper友好，扁平结构）
type Config struct {
    // 连接配置
    Backend   string        `mapstructure:"backend"`
    Endpoints []string      `mapstructure:"endpoints"`
    Username  string        `mapstructure:"username"`
    Password  string        `mapstructure:"password"`
    Timeout   time.Duration `mapstructure:"timeout"`
    
    // 锁行为配置
    TTL           time.Duration `mapstructure:"ttl"`
    RetryInterval time.Duration `mapstructure:"retry_interval"`
    AutoRenew     bool          `mapstructure:"auto_renew"`
}

// DefaultConfig 默认配置
func DefaultConfig() *Config {
    return &Config{
        Backend:       "etcd",
        Endpoints:     []string{"127.0.0.1:2379"},
        Timeout:       5 * time.Second,
        TTL:           10 * time.Second,
        RetryInterval: 100 * time.Millisecond,
        AutoRenew:     true,
    }
}
```

**创建文件**：`pkg/lock/simple/simple.go`

```go
package simple

import (
    "fmt"
    
    "github.com/ceyewan/genesis/internal/connector"
    internallock "github.com/ceyewan/genesis/internal/lock"
    "github.com/ceyewan/genesis/pkg/lock"
)

// Locker 分布式锁接口（嵌入标准接口）
type Locker interface {
    lock.Locker
}

// New 一行初始化分布式锁
func New(cfg *Config) (Locker, error) {
    if cfg == nil {
        cfg = DefaultConfig()
    }
    
    // 根据后端类型创建相应实例
    switch cfg.Backend {
    case "etcd":
        return newEtcdLocker(cfg)
    case "redis":
        return newRedisLocker(cfg)
    default:
        return nil, fmt.Errorf("unsupported backend: %s", cfg.Backend)
    }
}

func newEtcdLocker(cfg *Config) (Locker, error) {
    // 转换到连接配置
    connConfig := connector.ConnectionConfig{
        Backend:   cfg.Backend,
        Endpoints: cfg.Endpoints,
        Username:  cfg.Username,
        Password:  cfg.Password,
        Timeout:   cfg.Timeout,
    }
    
    // 使用连接管理器获取复用连接
    manager := connector.GetManager()
    client, err := manager.GetEtcdClient(connConfig)
    if err != nil {
        return nil, fmt.Errorf("failed to get etcd client: %w", err)
    }
    
    // 创建锁选项
    opts := &lock.LockOptions{
        TTL:           cfg.TTL,
        RetryInterval: cfg.RetryInterval,
        AutoRenew:     cfg.AutoRenew,
    }
    
    // 使用内部实现创建锁
    return internallock.NewEtcdLockerWithClient(client, opts)
}

func newRedisLocker(cfg *Config) (Locker, error) {
    // TODO: 实现Redis支持
    return nil, fmt.Errorf("redis backend not implemented yet")
}
```

### 建议4：viper集成示例

**创建文件**：`examples/simple-usage/main.go`

```go
package main

import (
    "context"
    "log"
    "time"
    
    "github.com/spf13/viper"
    "github.com/ceyewan/genesis/pkg/lock/simple"
)

func main() {
    // 方法1：使用默认配置
    locker, err := simple.New(nil)
    if err != nil {
        log.Fatal(err)
    }
    defer locker.Close()
    
    // 方法2：使用viper读取配置
    viper.SetConfigName("config")
    viper.SetConfigType("yaml")
    viper.AddConfigPath(".")
    
    // 设置默认值
    viper.SetDefault("lock.backend", "etcd")
    viper.SetDefault("lock.endpoints", []string{"127.0.0.1:2379"})
    viper.SetDefault("lock.ttl", "10s")
    
    // 读取配置
    if err := viper.ReadInConfig(); err != nil {
        log.Printf("Warning: no config file found, using defaults")
    }
    
    // 绑定环境变量
    viper.SetEnvPrefix("APP")
    viper.AutomaticEnv()
    
    // 解析配置
    var cfg simple.Config
    if err := viper.Sub("lock").Unmarshal(&cfg); err != nil {
        log.Fatal(err)
    }
    
    locker, err = simple.New(&cfg)
    if err != nil {
        log.Fatal(err)
    }
    defer locker.Close()
    
    // 使用锁
    ctx := context.Background()
    key := "/my/resource"
    
    if err := locker.Lock(ctx, key); err != nil {
        log.Fatal(err)
    }
    defer locker.Unlock(ctx, key)
    
    // 执行业务逻辑...
    time.Sleep(5 * time.Second)
}
```

### 建议5：配置文件示例

**创建文件**：`examples/simple-usage/config.yaml`

```yaml
lock:
  backend: "etcd"
  endpoints: 
    - "127.0.0.1:2379"
    - "127.0.0.1:2380"
  username: ""
  password: ""
  timeout: "5s"
  
  ttl: "10s"
  retry_interval: "100ms"
  auto_renew: true
```

## 优势总结

### 1. 消除循环引用
- **清晰层次**：`pkg/lock/simple` → `internal/lock` → `internal/connector`
- **单向依赖**：无反向依赖，架构清晰

### 2. 配置分离
- **职责清晰**：连接配置 vs 行为配置
- **viper友好**：扁平结构，支持环境变量
- **扩展容易**：新增字段只需修改一处

### 3. 向后兼容
- **现有API不变**：`pkg/lock/etcd/etcd.go` 保持原样
- **渐进迁移**：可以逐步迁移到新API

### 4. 性能优化
- **连接复用**：相同配置自动复用连接
- **懒加载**：首次使用时才创建连接
- **线程安全**：使用读写锁优化并发性能

## 测试验证

建议编写以下测试：

1. **连接复用测试**：验证相同配置只创建一个连接
2. **并发安全测试**：验证多线程环境下的正确性
3. **配置解析测试**：验证viper集成的正确性
4. **向后兼容测试**：验证现有代码无需修改

这个方案完全解决了你提出的两个核心问题，同时保持了代码的简洁性和可维护性。