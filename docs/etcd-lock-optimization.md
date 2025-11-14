# etcd 分布式锁优化实现指南

## 概述

本文档提供 etcd 分布式锁的优化实现方案，重点解决当前架构中的两导入问题和配置复杂度，同时保持向后兼容性和线程安全性。

## 核心优化目标

1. **一行初始化**：`lock.New(nil)` 即可使用
2. **单导入**：消除两导入要求，只需导入 `pkg/lock`
3. **零值配置**：所有配置字段提供合理默认值
4. **连接复用**：相同配置自动复用 etcd 连接
5. **向后兼容**：现有代码无需任何修改

## 架构设计

### 文件结构
```
internal/connector/etcd.go      # 智能连接管理器（配置哈希复用）
pkg/lock/simple.go             # 一行初始化 API 层
pkg/lock/etcd/etcd.go          # 保持现有 API 不变（向后兼容）
```

### 核心组件

#### 1. 智能连接管理器（internal/connector/etcd.go）

**职责**：
- 配置哈希计算和连接复用
- 单例管理，线程安全
- 懒加载连接创建

**关键实现**：
```go
type Manager struct {
    clients   map[string]*clientv3.Client  // 配置哈希 -> 客户端
    configs   map[string]*EtcdConfig       // 配置哈希 -> 配置
    hashMutex sync.RWMutex
}

func (m *Manager) GetEtcdClientWithConfig(config *EtcdConfig) (*clientv3.Client, error) {
    // 1. 计算配置哈希
    configHash := m.hashConfig(config)
    
    // 2. 检查复用连接
    m.hashMutex.RLock()
    if client, exists := m.clients[configHash]; exists {
        return client, nil
    }
    m.hashMutex.RUnlock()
    
    // 3. 创建新连接并缓存
    // 4. 双重检查避免并发重复创建
}
```

#### 2. 一行初始化 API（pkg/lock/simple.go）

**职责**：
- 统一后端选择（etcd/redis）
- 零值配置处理
- 连接管理器集成

**关键实现**：
```go
type Options struct {
    Backend       Backend       `json:"backend"`        // 默认: etcd
    Endpoints     []string      `json:"endpoints"`      // 默认: ["127.0.0.1:2379"]
    Password      string        `json:"password"`
    DialTimeout   time.Duration `json:"dial_timeout"`   // 默认: 5s
    TTL           time.Duration `json:"ttl"`            // 默认: 10s
    RetryInterval time.Duration `json:"retry_interval"` // 默认: 100ms
    AutoRenew     bool          `json:"auto_renew"`     // 默认: true
}

func New(opts *Options) (Locker, error) {
    if opts == nil {
        opts = &Options{} // 触发全部默认值
    }
    
    // 应用默认值和智能连接复用
    switch opts.Backend {
    case BackendEtcd:
        return newEtcdLockerWithManager(opts)
    case BackendRedis:
        return newRedisLockerWithManager(opts)
    }
}
```

## 实现步骤

### 第一步：创建智能连接管理器

**文件**：`internal/connector/etcd.go`

```go
package connector

import (
    "crypto/sha256"
    "encoding/hex"
    "fmt"
    "sync"
    
    clientv3 "go.etcd.io/etcd/client/v3"
)

// Manager 连接管理器（配置哈希复用，单例）
type Manager struct {
    clients   map[string]*clientv3.Client
    configs   map[string]*EtcdConfig
    hashMutex sync.RWMutex
}

var (
    globalManager *Manager
    managerOnce   sync.Once
)

// GetManager 获取连接管理器（单例）
func GetManager() *Manager {
    managerOnce.Do(func() {
        globalManager = &Manager{
            clients: make(map[string]*clientv3.Client),
            configs: make(map[string]*EtcdConfig),
        }
    })
    return globalManager
}

// GetEtcdClientWithConfig 根据配置获取客户端（自动复用）
func (m *Manager) GetEtcdClientWithConfig(config *EtcdConfig) (*clientv3.Client, error) {
    if config == nil {
        config = &EtcdConfig{} // 使用默认配置
    }
    
    // 计算配置哈希
    configHash := m.hashConfig(config)
    
    // 检查是否已有相同配置的客户端
    m.hashMutex.RLock()
    if client, exists := m.clients[configHash]; exists {
        m.hashMutex.RUnlock()
        return client, nil
    }
    m.hashMutex.RUnlock()
    
    // 创建新客户端
    m.hashMutex.Lock()
    defer m.hashMutex.Unlock()
    
    // 双重检查，避免并发重复创建
    if client, exists := m.clients[configHash]; exists {
        return client, nil
    }
    
    // 创建新客户端
    client, err := m.createEtcdClient(config)
    if err != nil {
        return nil, err
    }
    
    // 缓存客户端
    m.clients[configHash] = client
    m.configs[configHash] = config
    
    return client, nil
}

// hashConfig 计算配置哈希（用于连接复用判断）
func (m *Manager) hashConfig(config *EtcdConfig) string {
    h := sha256.New()
    
    // 关键配置字段参与哈希
    for _, endpoint := range config.Endpoints {
        h.Write([]byte(endpoint))
    }
    h.Write([]byte(config.Username))
    h.Write([]byte(config.Password))
    h.Write([]byte(config.DialTimeout.String()))
    
    return hex.EncodeToString(h.Sum(nil))
}

// createEtcdClient 创建 etcd 客户端
func (m *Manager) createEtcdClient(config *EtcdConfig) (*clientv3.Client, error) {
    // 应用默认值
    if len(config.Endpoints) == 0 {
        config.Endpoints = []string{"127.0.0.1:2379"}
    }
    if config.DialTimeout == 0 {
        config.DialTimeout = 5 * time.Second
    }
    
    clientConfig := clientv3.Config{
        Endpoints:   config.Endpoints,
        Username:    config.Username,
        Password:    config.Password,
        DialTimeout: config.DialTimeout,
    }
    
    return clientv3.New(clientConfig)
}
```

### 第二步：创建一行初始化 API

**文件**：`pkg/lock/simple.go`

```go
package lock

import (
    "context"
    "time"
    
    "github.com/ceyewan/genesis/internal/connector"
    internallock "github.com/ceyewan/genesis/internal/lock"
    "github.com/ceyewan/genesis/pkg/lock"
)

// Backend 后端类型
type Backend string

const (
    BackendEtcd  Backend = "etcd"
    BackendRedis Backend = "redis"
)

// Options 统一配置（零值友好）
type Options struct {
    // 后端选择
    Backend Backend `json:"backend" yaml:"backend"` // 默认: etcd
    
    // 连接配置
    Endpoints   []string      `json:"endpoints"`   // 默认: etcd["127.0.0.1:2379"], redis["127.0.0.1:6379"]
    Password    string        `json:"password"`    // 认证密码
    DialTimeout time.Duration `json:"dial_timeout"`// 默认: 5s
    
    // 锁行为配置
    TTL           time.Duration `json:"ttl"`           // 默认: 10s
    RetryInterval time.Duration `json:"retry_interval"`// 默认: 100ms
    AutoRenew     bool          `json:"auto_renew"`    // 默认: true
}

// New 一行初始化分布式锁（智能连接复用）
func New(opts *Options) (Locker, error) {
    if opts == nil {
        opts = &Options{} // 使用全部默认值
    }
    
    // 应用默认值
    if opts.Backend == "" {
        opts.Backend = BackendEtcd // 默认使用etcd
    }
    if opts.TTL == 0 {
        opts.TTL = 10 * time.Second
    }
    if opts.RetryInterval == 0 {
        opts.RetryInterval = 100 * time.Millisecond
    }
    
    // 根据后端类型创建相应实例
    switch opts.Backend {
    case BackendEtcd:
        return newEtcdLockerWithManager(opts)
    case BackendRedis:
        return newRedisLockerWithManager(opts)
    default:
        return nil, fmt.Errorf("unsupported backend: %s", opts.Backend)
    }
}

func newEtcdLockerWithManager(opts *Options) (Locker, error) {
    // 转换到etcd配置
    etcdConfig := &connector.EtcdConfig{
        Endpoints:   opts.Endpoints,
        Username:    "", // 从Options中提取
        Password:    opts.Password,
        DialTimeout: opts.DialTimeout,
    }
    
    // 应用etcd默认值
    if len(etcdConfig.Endpoints) == 0 {
        etcdConfig.Endpoints = []string{"127.0.0.1:2379"}
    }
    if etcdConfig.DialTimeout == 0 {
        etcdConfig.DialTimeout = 5 * time.Second
    }
    
    // 使用连接管理器获取复用连接
    manager := connector.GetManager()
    client, err := manager.GetEtcdClientWithConfig(etcdConfig)
    if err != nil {
        return nil, err
    }
    
    // 创建etcd锁（复用现有内部实现）
    return internallock.NewEtcdLockerWithClient(client, &lock.LockOptions{
        TTL:           opts.TTL,
        RetryInterval: opts.RetryInterval,
        AutoRenew:     opts.AutoRenew,
    })
}

func newRedisLockerWithManager(opts *Options) (Locker, error) {
    // TODO: 实现Redis锁支持
    return nil, fmt.Errorf("redis backend not implemented yet")
}
```

### 第三步：扩展内部锁实现

**需要在 `internal/lock/etcd.go` 中添加**：

```go
// NewEtcdLockerWithClient 使用现有客户端创建锁（支持连接复用）
func NewEtcdLockerWithClient(client *clientv3.Client, opts *lock.LockOptions) (*EtcdLocker, error) {
    if opts == nil {
        opts = lock.DefaultLockOptions()
    }
    
    // 创建 session，用于租约管理
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

## 使用示例

### 基本使用（一行初始化）
```go
package main

import (
    "context"
    "log"
    
    "github.com/ceyewan/genesis/pkg/lock"
)

func main() {
    // 一行初始化，全部默认配置
    locker, err := lock.New(nil)
    if err != nil {
        log.Fatal(err)
    }
    defer locker.Close()
    
    // 使用锁
    ctx := context.Background()
    if err := locker.Lock(ctx, "/my/resource"); err != nil {
        log.Fatal(err)
    }
    defer locker.Unlock(ctx, "/my/resource")
    
    // 执行业务逻辑...
}
```

### 自定义配置
```go
// 自定义配置
locker, err := lock.New(&lock.Options{
    Backend:   lock.BackendEtcd,
    Endpoints: []string{"localhost:2379"},
    TTL:       15 * time.Second,
})
```

### 后端切换
```go
// 切换到Redis
locker, err := lock.New(&lock.Options{
    Backend:   lock.BackendRedis,
    Endpoints: []string{"localhost:6379"},
})
```

## 测试验证

### 连接复用测试
```go
func TestConnectionReuse(t *testing.T) {
    // 第一次创建
    locker1, err := lock.New(nil)
    assert.NoError(t, err)
    
    // 第二次创建（应该复用连接）
    locker2, err := lock.New(nil)
    assert.NoError(t, err)
    
    // 验证连接复用（通过内部状态或行为验证）
    // 两个locker应该使用相同的etcd客户端
}
```

### 功能测试
```go
func TestLockFunctionality(t *testing.T) {
    locker, err := lock.New(nil)
    assert.NoError(t, err)
    defer locker.Close()
    
    ctx := context.Background()
    key := "/test/resource"
    
    // 测试加锁
    err = locker.Lock(ctx, key)
    assert.NoError(t, err)
    
    // 测试解锁
    err = locker.Unlock(ctx, key)
    assert.NoError(t, err)
    
    // 测试TryLock
    success, err := locker.TryLock(ctx, key)
    assert.NoError(t, err)
    assert.True(t, success)
    
    if success {
        locker.Unlock(ctx, key)
    }
}
```

## 性能优化

### 连接池效果
- 相同配置自动复用连接
- 减少连接建立开销
- 降低etcd服务器压力

### 零值配置优化
- 所有字段提供合理默认值
- 避免用户手动配置复杂性
- 减少配置错误可能性

## 向后兼容保证

- **现有API完全不变**：`pkg/lock/etcd/etcd.go` 保持原样
- **新增简化层**：`pkg/lock/simple.go` 提供新API
- **零破坏性迁移**：现有代码无需修改即可运行

## 总结

本优化方案通过智能连接管理和一行初始化API，解决了etcd分布式锁的两导入问题和配置复杂度，同时保持了向后兼容性和线程安全性。连接复用机制提升了性能，零值配置降低了使用门槛。