# etcd 分布式锁优化实现指南 v2.0

## 核心改进

基于对循环引用和配置混用问题的分析，本版本提供以下改进：

1. **消除循环引用**：清晰的包依赖层次
2. **配置分层**：连接配置与行为配置分离
3. **viper友好**：扁平化配置结构支持
4. **保持向后兼容**：现有代码无需修改

## 架构设计

### 包依赖层次

```
pkg/lock/simple/          (新API层)
  ↓
pkg/lock/                 (接口定义)
  ↓
internal/lock/            (实现层)
  ↓
internal/connector/       (连接管理层)
```

### 核心组件

#### 1. 连接管理层（internal/connector）

职责：
- 连接池管理
- 配置哈希和复用
- 线程安全

```go
// 连接配置（仅连接相关）
type ConnectionConfig struct {
    Backend   string        `json:"backend" yaml:"backend"`
    Endpoints []string      `json:"endpoints" yaml:"endpoints"`
    Username  string        `json:"username" yaml:"username"`
    Password  string        `json:"password" yaml:"password"`
    Timeout   time.Duration `json:"timeout" yaml:"timeout"`
}

// 连接管理器（无业务逻辑依赖）
type Manager struct {
    clients map[string]interface{} // 不同后端的连接池
    configs map[string]ConnectionConfig
    mu      sync.RWMutex
}
```

#### 2. 实现层（internal/lock）

职责：
- 锁的具体实现
- 仅依赖连接管理层获取客户端
- 不依赖任何pkg层

#### 3. 新API层（pkg/lock/simple）

职责：
- 一行初始化
- 配置转换
- 后端选择

## 配置设计

### viper友好的扁平结构

```yaml
# config.yaml
lock:
  backend: "etcd"                    # 后端类型
  endpoints: ["127.0.0.1:2379"]      # 连接地址
  username: ""                       # 认证用户
  password: ""                       # 认证密码
  timeout: "5s"                      # 连接超时
  
  # 锁行为配置
  ttl: "10s"                         # 锁超时时间
  retry_interval: "100ms"            # 重试间隔
  auto_renew: true                   # 自动续期
```

### 配置结构体

```go
// pkg/lock/simple/config.go
type Config struct {
    // 连接配置
    Backend   string        `mapstructure:"backend"`
    Endpoints []string      `mapstructure:"endpoints"`
    Username  string        `mapstructure:"username"`
    Password  string        `mapstructure:"password"`
    Timeout   time.Duration `mapstructure:"timeout"`
    
    // 行为配置
    TTL           time.Duration `mapstructure:"ttl"`
    RetryInterval time.Duration `mapstructure:"retry_interval"`
    AutoRenew     bool          `mapstructure:"auto_renew"`
}

// 默认值
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

## 实现方案

### 第一步：重构连接管理器

```go
// internal/connector/manager.go
package connector

import (
    "fmt"
    "sync"
    "time"
    
    "github.com/ceyewan/genesis/pkg/lock"  // 仅依赖接口
    clientv3 "go.etcd.io/etcd/client/v3"
)

// ConnectionConfig 连接配置（与业务无关）
type ConnectionConfig struct {
    Backend   string
    Endpoints []string
    Username  string
    Password  string
    Timeout   time.Duration
}

// Manager 连接管理器
type Manager struct {
    etcdClients map[string]*clientv3.Client
    configs     map[string]ConnectionConfig
    mu          sync.RWMutex
}

// GetEtcdClient 获取etcd客户端（根据配置哈希复用）
func (m *Manager) GetEtcdClient(config ConnectionConfig) (*clientv3.Client, error) {
    // 计算配置哈希
    hash := m.hashConfig(config)
    
    // 检查复用
    m.mu.RLock()
    if client, exists := m.etcdClients[hash]; exists {
        m.mu.RUnlock()
        return client, nil
    }
    m.mu.RUnlock()
    
    // 创建新连接
    m.mu.Lock()
    defer m.mu.Unlock()
    
    // 双重检查
    if client, exists := m.etcdClients[hash]; exists {
        return client, nil
    }
    
    // 创建客户端
    client, err := m.createEtcdClient(config)
    if err != nil {
        return nil, err
    }
    
    m.etcdClients[hash] = client
    m.configs[hash] = config
    
    return client, nil
}
```

### 第二步：创建新API层

```go
// pkg/lock/simple/simple.go
package simple

import (
    "fmt"
    "time"
    
    "github.com/ceyewan/genesis/internal/connector"
    internallock "github.com/ceyewan/genesis/internal/lock"
    "github.com/ceyewan/genesis/pkg/lock"
)

// Locker 简单的分布式锁接口
type Locker interface {
    lock.Locker // 嵌入标准接口
}

// New 一行初始化
func New(cfg *Config) (Locker, error) {
    if cfg == nil {
        cfg = DefaultConfig()
    }
    
    // 根据后端类型创建
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
    // 转换配置
    connConfig := connector.ConnectionConfig{
        Backend:   cfg.Backend,
        Endpoints: cfg.Endpoints,
        Username:  cfg.Username,
        Password:  cfg.Password,
        Timeout:   cfg.Timeout,
    }
    
    // 获取连接管理器
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
```

### 第三步：viper集成示例

```go
// examples/viper-usage/main.go
package main

import (
    "github.com/spf13/viper"
    "github.com/ceyewan/genesis/pkg/lock/simple"
)

func main() {
    // 读取配置
    viper.SetConfigName("config")
    viper.SetConfigType("yaml")
    viper.AddConfigPath(".")
    
    if err := viper.ReadInConfig(); err != nil {
        panic(err)
    }
    
    // 绑定配置前缀
    viper.SetEnvPrefix("APP")
    viper.AutomaticEnv()
    
    // 解析配置
    var cfg simple.Config
    if err := viper.Sub("lock").Unmarshal(&cfg); err != nil {
        panic(err)
    }
    
    // 创建锁
    locker, err := simple.New(&cfg)
    if err != nil {
        panic(err)
    }
    defer locker.Close()
    
    // 使用锁...
}
```

## 优势对比

### 原设计问题
1. **循环引用风险**：`pkg/lock/simple` 依赖 `internal/lock`，`internal/lock` 又可能回调
2. **配置混乱**：连接配置和行为配置混在一起
3. **viper不友好**：嵌套结构复杂

### 新设计优势
1. **清晰依赖**：单向依赖，无循环
2. **配置分层**：连接与行为分离
3. **viper友好**：扁平结构，支持环境变量
4. **向后兼容**：现有API完全不变

## 使用示例

### 基本使用
```go
import "github.com/ceyewan/genesis/pkg/lock/simple"

// 默认配置
locker, err := simple.New(nil)

// 自定义配置
locker, err := simple.New(&simple.Config{
    Backend:   "etcd",
    Endpoints: []string{"localhost:2379"},
    TTL:       15 * time.Second,
})
```

### 配置环境变量
```bash
export APP_LOCK_BACKEND=etcd
export APP_LOCK_ENDPOINTS=localhost:2379,localhost:2380
export APP_LOCK_TTL=15s
```

这个设计方案解决了你提出的两个核心问题，同时保持了简洁的API设计。