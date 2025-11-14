# etcd 分布式锁优化实现指南 v3.0

## 核心改进

基于Go普遍规范，重新设计API：
- **New函数签名**：`New(config *Config, option *Option) (Locker, error)`
- **Config vs Option**：遵循Go标准库的设计模式
- **零值友好**：两个参数都可以为nil

## Go规范参考

参考标准库的设计模式：
- `database/sql.Open(driverName string, dataSourceName string)` - 连接参数分开
- `http.NewRequest(method, url string, body io.Reader)` - 必需参数直接传
- `grpc.Dial(target string, opts ...DialOption)` - 可选参数用option模式

## Config vs Option 职责划分

### Config（连接配置）- 必需参数
- **Backend**：后端类型（etcd/redis）
- **Endpoints**：连接地址
- **Username/Password**：认证信息
- **Timeout**：连接超时

### Option（行为配置）- 可选参数
- **TTL**：锁超时时间
- **RetryInterval**：重试间隔
- **AutoRenew**：自动续期
- **MaxRetries**：最大重试次数

## 架构设计

### 清晰的依赖层次
```
pkg/lock/simple/          (新API层)
  ↓
pkg/lock/                 (接口定义)
  ↓
internal/lock/            (实现层)
  ↓
internal/connector/       (连接管理层)
```

## 具体实现

### 1. Config 设计（连接相关）

```go
// pkg/lock/simple/config.go
package simple

import "time"

// Config 连接配置（必需参数）
type Config struct {
    Backend  string   // 后端类型: etcd, redis
    Endpoints []string // 连接地址
    Username string   // 认证用户（可选）
    Password string   // 认证密码（可选）
    Timeout  time.Duration // 连接超时（可选，默认5s）
}

// DefaultConfig 默认连接配置
func DefaultConfig() *Config {
    return &Config{
        Backend:   "etcd",
        Endpoints: []string{"127.0.0.1:2379"},
        Timeout:   5 * time.Second,
    }
}
```

### 2. Option 设计（行为相关）

```go
// pkg/lock/simple/option.go
package simple

import "time"

// Option 锁行为配置（可选参数）
type Option struct {
    TTL           time.Duration // 锁超时时间（默认10s）
    RetryInterval time.Duration // 重试间隔（默认100ms）
    AutoRenew     bool          // 自动续期（默认true）
    MaxRetries    int           // 最大重试次数（默认0，表示无限重试）
}

// DefaultOption 默认行为配置
func DefaultOption() *Option {
    return &Option{
        TTL:           10 * time.Second,
        RetryInterval: 100 * time.Millisecond,
        AutoRenew:     true,
        MaxRetries:    0,
    }
}

// WithTTL 函数式选项模式（可选）
func WithTTL(ttl time.Duration) func(*Option) {
    return func(o *Option) {
        o.TTL = ttl
    }
}

// WithRetryInterval 函数式选项模式（可选）
func WithRetryInterval(interval time.Duration) func(*Option) {
    return func(o *Option) {
        o.RetryInterval = interval
    }
}
```

### 3. New 函数实现

```go
// pkg/lock/simple/simple.go
package simple

import (
    "fmt"
    
    "github.com/ceyewan/genesis/internal/connector"
    internallock "github.com/ceyewan/genesis/internal/lock"
    "github.com/ceyewan/genesis/pkg/lock"
)

// Locker 分布式锁接口
type Locker interface {
    lock.Locker
}

// New 创建分布式锁（Go规范：config必需，option可选）
func New(config *Config, option *Option) (Locker, error) {
    // 处理nil参数
    if config == nil {
        config = DefaultConfig()
    }
    if option == nil {
        option = DefaultOption()
    }
    
    // 验证必需参数
    if config.Backend == "" {
        return nil, fmt.Errorf("backend is required")
    }
    if len(config.Endpoints) == 0 {
        return nil, fmt.Errorf("endpoints is required")
    }
    
    // 根据后端类型创建
    switch config.Backend {
    case "etcd":
        return newEtcdLocker(config, option)
    case "redis":
        return newRedisLocker(config, option)
    default:
        return nil, fmt.Errorf("unsupported backend: %s", config.Backend)
    }
}

func newEtcdLocker(config *Config, option *Option) (Locker, error) {
    // 转换到连接配置
    connConfig := connector.ConnectionConfig{
        Backend:   config.Backend,
        Endpoints: config.Endpoints,
        Username:  config.Username,
        Password:  config.Password,
        Timeout:   config.Timeout,
    }
    
    // 应用默认值
    if connConfig.Timeout == 0 {
        connConfig.Timeout = 5 * time.Second
    }
    
    // 获取连接管理器
    manager := connector.GetManager()
    client, err := manager.GetEtcdClient(connConfig)
    if err != nil {
        return nil, fmt.Errorf("failed to get etcd client: %w", err)
    }
    
    // 创建锁选项
    opts := &lock.LockOptions{
        TTL:           option.TTL,
        RetryInterval: option.RetryInterval,
        AutoRenew:     option.AutoRenew,
    }
    
    // 使用内部实现创建锁
    return internallock.NewEtcdLockerWithClient(client, opts)
}
```

## 使用示例

### 基本使用（全部默认）
```go
// 全部使用默认配置
locker, err := simple.New(nil, nil)
if err != nil {
    log.Fatal(err)
}
defer locker.Close()
```

### 自定义连接配置
```go
// 自定义连接，行为用默认
locker, err := simple.New(&simple.Config{
    Backend:   "etcd",
    Endpoints: []string{"localhost:2379", "localhost:2380"},
    Username:  "user",
    Password:  "pass",
    Timeout:   10 * time.Second,
}, nil)
```

### 自定义行为配置
```go
// 默认连接，自定义行为
locker, err := simple.New(nil, &simple.Option{
    TTL:           30 * time.Second,
    RetryInterval: 500 * time.Millisecond,
    AutoRenew:     false,
    MaxRetries:    3,
})
```

### 两者都自定义
```go
// 完全自定义
locker, err := simple.New(
    &simple.Config{
        Backend:   "etcd",
        Endpoints: []string{"remote:2379"},
        Timeout:   15 * time.Second,
    },
    &simple.Option{
        TTL:           60 * time.Second,
        RetryInterval: 1 * time.Second,
        AutoRenew:     true,
    },
)
```

## 与标准库对比

| 场景 | 标准库示例 | 我们的设计 |
|------|------------|------------|
| 数据库连接 | `sql.Open(driver, dsn)` | `simple.New(config, option)` |
| HTTP请求 | `http.NewRequest(method, url, body)` | `simple.New(config, option)` |
| gRPC连接 | `grpc.Dial(target, opts...)` | `simple.New(config, option)` |
| 必需参数 | driver, dsn | config.Backend, config.Endpoints |
| 可选参数 | 可变参数 | option结构体 |

## 向后兼容保证

1. **现有API完全不变**：`pkg/lock/etcd/etcd.go` 保持原样
2. **新增简化层**：`pkg/lock/simple` 提供新API
3. **零破坏性**：现有代码无需任何修改

## 优势总结

1. **符合Go规范**：config必需，option可选
2. **职责清晰**：连接配置 vs 行为配置
3. **零值友好**：两个参数都可以为nil
4. **扩展容易**：新增配置只需修改对应结构体
5. **使用简单**：一行初始化，支持渐进自定义

这个设计完全遵循Go标准库的设计哲学，既保持了简洁性，又提供了充分的灵活性。