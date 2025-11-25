# Registry 服务注册与发现设计文档

## 1. 目标与原则

`registry` 组件旨在为 Genesis 框架提供标准化的服务注册与发现能力，是微服务架构中的核心基础设施。初期版本基于 Etcd 实现，并深度集成 gRPC Resolver，实现客户端侧负载均衡。

**核心设计原则：**

1. **接口抽象 (Abstraction):** 定义通用的 `Registry` 接口，业务代码不感知底层实现（Etcd/Consul/Nacos）。
2. **强依赖 Connector (Connector Dependency):** 复用 `pkg/connector` 管理的 Etcd 连接，不自行创建 Client，确保连接池管理和配置的一致性。
3. **gRPC 原生支持 (Native gRPC Support):** 实现 gRPC `resolver.Builder` 接口，支持标准的 `etcd://<authority>/<service_name>` 解析方式，无缝对接 gRPC 负载均衡。
4. **本地缓存与高性能 (Local Cache & Performance):** 服务发现层内置本地缓存机制，减少对注册中心的直接请求压力，提升系统吞吐量。
5. **生命周期管理 (Lifecycle Management):** 实现标准的 `Lifecycle` 接口，自动管理 Lease 续租（KeepAlive）和服务注销，确保服务下线时能够快速从注册中心移除。

## 2. 项目结构

遵循框架整体的分层设计，API 与实现分离：

```text
genesis/
├── pkg/
│   └── registry/               # 公开 API 入口
│       ├── registry.go         # 工厂函数 (New) + 类型导出
│       ├── options.go          # Option 定义 (WithLogger, WithMeter, WithTracer)
│       └── types/              # 类型定义
│           ├── interface.go    # Registry 接口
│           ├── service.go      # ServiceInstance 结构定义
│           ├── config.go       # 配置定义
│           └── errors.go       # 错误定义
├── internal/
│   └── registry/               # 内部实现
│       └── etcd/               # Etcd 具体实现
│           ├── registry.go     # 注册逻辑 (Lease, Put)
│           ├── discovery.go    # 发现逻辑 (Get, Watch, Cache)
│           ├── resolver.go     # gRPC Resolver Builder 实现
│           └── watcher.go      # 监听器实现
└── ...
```

## 3. 核心 API 设计

核心定义位于 `pkg/registry/types/`。

### 3.1. ServiceInstance 模型

定义服务的元数据模型，兼容 gRPC 属性。

```go
// pkg/registry/types/service.go

package types

// ServiceInstance 代表一个服务实例
type ServiceInstance struct {
    ID        string            `json:"id"`        // 唯一实例 ID (通常是 UUID)
    Name      string            `json:"name"`      // 服务名称 (如 user-service)
    Version   string            `json:"version"`   // 版本号
    Metadata  map[string]string `json:"metadata"`  // 元数据 (Region, Zone, Weight, Group 等)
    Endpoints []string          `json:"endpoints"` // 服务地址列表 (如 grpc://192.168.1.10:9090)
}
```

### 3.2. 核心接口

```go
// pkg/registry/types/interface.go

package types

import (
    "context"
    "time"
    "google.golang.org/grpc"
)

// Registry 服务注册与发现接口
type Registry interface {
    // --- 服务注册 ---
    
    // Register 注册服务实例
    // ctx: 上下文
    // service: 服务实例信息
    // ttl: 租约有效期 (例如 10s)，超时后若无续约服务将自动下线
    Register(ctx context.Context, service *ServiceInstance, ttl time.Duration) error

    // Deregister 注销服务实例
    // serviceID: 服务实例 ID
    Deregister(ctx context.Context, serviceID string) error

    // --- 服务发现 ---
    
    // GetService 获取服务实例列表
    // 优先读取本地缓存，缓存未命中或过期时查询注册中心
    GetService(ctx context.Context, serviceName string) ([]*ServiceInstance, error)

    // Watch 监听服务实例变化
    // 返回一个事件通道，接收服务变化事件 (PUT/DELETE)
    Watch(ctx context.Context, serviceName string) (<-chan ServiceEvent, error)

    // --- gRPC 集成 ---
    
    // GetConnection 获取到指定服务的 gRPC 连接
    // 内部封装了 Resolver 和 Balancer 的配置，提供开箱即用的连接对象
    // 支持自动服务发现和客户端负载均衡
    GetConnection(ctx context.Context, serviceName string, opts ...grpc.DialOption) (*grpc.ClientConn, error)

    // --- 生命周期管理 ---
    
    // Start 启动后台任务 (Lease KeepAlive、Watch 监听等)
    Start(ctx context.Context) error
    
    // Stop 停止后台任务并清理资源
    Stop(ctx context.Context) error
    
    // Phase 返回启动阶段 (建议 20，与其他业务组件一致)
    Phase() int
}

// ServiceEvent 服务变化事件
type ServiceEvent struct {
    Type    EventType        // 事件类型 (PUT/DELETE)
    Service *ServiceInstance // 服务实例信息
}

// EventType 事件类型
type EventType string

const (
    EventTypePut    EventType = "PUT"    // 服务注册或更新
    EventTypeDelete EventType = "DELETE" // 服务注销
)
```

### 3.3. 配置 (Config)

```go
// pkg/registry/types/config.go

package types

import "time"

type Config struct {
    // Namespace Etcd Key 前缀，默认 "/genesis/services"
    Namespace string `yaml:"namespace" json:"namespace"`

    // Schema 注册到 gRPC resolver 的 schema，默认 "etcd"
    Schema string `yaml:"schema" json:"schema"`

    // DefaultTTL 默认服务注册租约时长，默认 30s
    DefaultTTL time.Duration `yaml:"default_ttl" json:"default_ttl"`

    // RetryInterval 重连/重试间隔，默认 1s
    RetryInterval time.Duration `yaml:"retry_interval" json:"retry_interval"`

    // EnableCache 是否启用本地服务发现缓存，默认 true
    EnableCache bool `yaml:"enable_cache" json:"enable_cache"`

    // CacheExpiration 本地缓存过期时间，默认 10s
    CacheExpiration time.Duration `yaml:"cache_expiration" json:"cache_expiration"`
}
```

### 3.4. 组件初始化选项 (Option)

遵循组件规范，使用 Option 模式注入可观测性依赖。

```go
// pkg/registry/options.go

package registry

import (
    "github.com/ceyewan/genesis/pkg/clog"
    "github.com/ceyewan/genesis/pkg/telemetry/types"
)

// Option 组件初始化选项函数
type Option func(*options)

// options 选项结构
type options struct {
    logger clog.Logger
    meter  types.Meter
    tracer types.Tracer
}

// WithLogger 注入日志记录器
// 组件内部会自动追加 "registry" namespace
func WithLogger(l clog.Logger) Option {
    return func(o *options) {
        if l != nil {
            o.logger = l.WithNamespace("registry")
        }
    }
}

// WithMeter 注入指标 Meter
func WithMeter(m types.Meter) Option {
    return func(o *options) {
        o.meter = m
    }
}

// WithTracer 注入 Tracer
func WithTracer(t types.Tracer) Option {
    return func(o *options) {
        o.tracer = t
    }
}
```

## 4. Etcd 实现设计

### 4.1. 存储 Schema

Etcd Key 采用层级结构设计，便于 Watch 前缀：

```text
<namespace>/<service_name>/<instance_id> -> JSON(ServiceInstance)
```

例如：`/genesis/services/user-service/uuid-1234-5678`

### 4.2. 注册流程 (Register)

1. **Grant Lease:** 使用传入的 `ttl` 创建 Lease。
2. **KeepAlive:** 启动后台 Goroutine 对该 Lease ID 进行自动续约。
3. **Put:** 将 `ServiceInstance` 序列化为 JSON，调用 Etcd Put 操作，并绑定该 Lease。
4. **Deregister:** 调用 Delete 删除 Key，并 Revoke Lease。
5. **异常处理:** 如果 KeepAlive 通道关闭，尝试重连并重新注册。

### 4.3. 发现流程与本地缓存 (Discovery & Local Cache)

1. **Watch:** 启动 `clientv3.Watch` 监听 `<namespace>/<service_name>/` 前缀。
2. **Local Cache:**
    * Discovery 内部维护 `map[string][]*ServiceInstance` 缓存。
    * Watch 事件（PUT/DELETE）实时更新本地缓存。
    * `GetService` 直接返回缓存数据，实现高性能读取。
    * 支持通过配置启用/禁用缓存。
3. **GetConnection:**
    * 内部构建 gRPC Target: `etcd:///<service_name>` (假设 scheme 配置为 etcd)。
    * 调用 `grpc.Dial`，并注入默认的 Load Balancing Config (如 round_robin)。

### 4.4. 生命周期管理

```go
// internal/registry/etcd/registry.go

type EtcdRegistry struct {
    client    *clientv3.Client
    cfg       types.Config
    logger    clog.Logger
    meter     telemetrytypes.Meter
    tracer    telemetrytypes.Tracer
    
    // 后台任务管理
    leases    map[string]clientv3.LeaseID  // serviceID -> leaseID
    watchers  map[string]context.CancelFunc // serviceName -> cancel
    cache     map[string][]*types.ServiceInstance // 本地缓存
    stopChan  chan struct{}
    wg        sync.WaitGroup
    mu        sync.RWMutex
}

func (r *EtcdRegistry) Start(ctx context.Context) error {
    r.logger.Info("starting registry service")
    // 启动已注册服务的 lease keepalive
    // 初始化本地缓存
    return nil
}

func (r *EtcdRegistry) Stop(ctx context.Context) error {
    r.logger.Info("stopping registry service")
    close(r.stopChan)
    
    // 停止所有 watchers
    r.mu.Lock()
    for _, cancel := range r.watchers {
        cancel()
    }
    r.watchers = nil
    
    // 撤销所有 leases
    for _, leaseID := range r.leases {
        r.client.Revoke(ctx, leaseID)
    }
    r.leases = nil
    r.mu.Unlock()
    
    r.wg.Wait()
    return nil
}

func (r *EtcdRegistry) Phase() int {
    return 20 // 与其他业务组件一致
}
```

## 5. gRPC Resolver 集成

为了让 gRPC 客户端能自动发现服务，组件将实现 `google.golang.org/grpc/resolver`。

### 5.1. 原理

gRPC Resolver 机制允许自定义 naming system。我们将实现一个 Builder，注册 scheme 为 `etcd`。

当用户调用 `grpc.Dial("etcd:///user-service", ...)` 时：

1. gRPC 解析出 Scheme 为 `etcd`，Endpoint 为 `user-service`。
2. 调用我们注册的 Builder 的 `Build` 方法。
3. Builder 内部调用 `Watch("user-service")` 获取事件通道。
4. 启动 Goroutine 循环监听事件通道。
5. 当收到事件时，将服务列表转换为 `[]resolver.Address` 并调用 `ClientConn.UpdateState` 更新 gRPC 内部的连接列表。

### 5.2. 负载均衡

Resolver 只负责更新地址列表。负载均衡由 gRPC 客户端的 Balancer 实现（默认配置为 `round_robin`）。

## 6. 工厂函数设计

### 6.1. 独立模式工厂函数

```go
// pkg/registry/registry.go

package registry

import (
    "github.com/ceyewan/genesis/pkg/connector"
    "github.com/ceyewan/genesis/pkg/registry/types"
    internalregistry "github.com/ceyewan/genesis/internal/registry/etcd"
)

// New 创建 Registry 实例（基于 Etcd）
// conn: Etcd 连接器
// cfg: 组件配置
// opts: 可选参数 (Logger, Meter, Tracer)
func New(conn connector.EtcdConnector, cfg types.Config, opts ...Option) (types.Registry, error) {
    // 应用选项
    opt := defaultOptions()
    for _, o := range opts {
        o(&opt)
    }
    
    // 调用内部实现
    return internalregistry.New(conn, cfg, opt.logger, opt.meter, opt.tracer)
}

func defaultOptions() *options {
    return &options{
        logger: clog.Default(), // 默认 Logger
    }
}
```

### 6.2. 容器模式集成

在 Container 中自动初始化 Registry 组件。

```go
// pkg/container/container.go

func (c *Container) initRegistry(cfg *types.Config) error {
    // 获取 Etcd 连接器
    etcdConn, err := c.GetEtcdConnector(cfg.Etcd)
    if err != nil {
        return err
    }

    // 创建 Registry 实例
    reg, err := registry.New(etcdConn, cfg.Registry,
        registry.WithLogger(c.Logger),
        registry.WithMeter(c.Meter),
        registry.WithTracer(c.Tracer),
    )
    if err != nil {
        return err
    }

    c.Registry = reg
    
    // 注册为 Lifecycle 对象
    c.registerLifecycle(reg)
    
    return nil
}
```

## 7. 使用示例

### 7.1. 独立模式

```go
package main

import (
    "context"
    "time"

    "github.com/ceyewan/genesis/pkg/clog"
    "github.com/ceyewan/genesis/pkg/connector"
    "github.com/ceyewan/genesis/pkg/registry"
    "github.com/ceyewan/genesis/pkg/registry/types"
)

func main() {
    // 创建 Logger
    logger := clog.New(&clog.Config{
        Level:  "info",
        Format: "json",
    })

    // 创建 Etcd 连接器
    etcdConn, _ := connector.NewEtcd(&connector.EtcdConfig{
        Endpoints: []string{"localhost:2379"},
    })

    // 创建 Registry 实例
    reg, _ := registry.New(etcdConn, types.Config{
        Namespace:       "/genesis/services",
        Schema:          "etcd",
        DefaultTTL:      30 * time.Second,
        RetryInterval:   1 * time.Second,
        EnableCache:     true,
        CacheExpiration: 10 * time.Second,
    }, registry.WithLogger(logger))

    // 启动 Registry
    ctx := context.Background()
    reg.Start(ctx)
    defer reg.Stop(ctx)

    // 定义服务实例
    svc := &types.ServiceInstance{
        ID:        "user-service-1",
        Name:      "user-service",
        Endpoints: []string{"grpc://192.168.1.100:8080"},
        Version:   "1.0.0",
        Metadata: map[string]string{
            "region": "us-west-1",
        },
    }

    // 注册服务，指定 30s TTL
    if err := reg.Register(ctx, svc, 30*time.Second); err != nil {
        logger.Error("failed to register", clog.Error(err))
        return
    }
    defer reg.Deregister(ctx, svc.ID)

    // 服务保持运行...
}
```

### 7.2. 容器模式

```go
package main

import (
    "context"
    "time"

    "github.com/ceyewan/genesis/pkg/config"
    "github.com/ceyewan/genesis/pkg/container"
)

func main() {
    // 加载配置
    cfgMgr := config.NewManager(config.WithPaths("./config"))
    _ = cfgMgr.Load(context.Background())
    
    var appCfg AppConfig
    _ = cfgMgr.Unmarshal(&appCfg)

    // 创建 Container
    app, _ := container.New(&appCfg, container.WithConfigManager(cfgMgr))
    defer app.Close()

    ctx := context.Background()

    // 定义服务实例
    svc := &types.ServiceInstance{
        ID:        "user-service-1",
        Name:      "user-service",
        Endpoints: []string{"grpc://192.168.1.100:8080"},
    }

    // 注册服务
    if err := app.Registry.Register(ctx, svc, 30*time.Second); err != nil {
        app.Logger.Error("failed to register", clog.Error(err))
        return
    }
    defer app.Registry.Deregister(ctx, svc.ID)

    // 服务保持运行...
}
```

### 7.3. 客户端发现 (方式一: GetConnection)

```go
// 直接获取连接，开箱即用
conn, err := reg.GetConnection(ctx, "user-service")
if err != nil {
    panic(err)
}
defer conn.Close()

// 使用 conn 调用 gRPC 服务
client := pb.NewUserServiceClient(conn)
resp, err := client.GetUser(ctx, &pb.GetUserRequest{ID: "123"})
```

### 7.4. 客户端发现 (方式二: 原生 gRPC Dial)

```go
import (
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
)

// Registry 初始化时已自动注册 gRPC Resolver Builder

// 使用标准 gRPC Dial
conn, err := grpc.Dial(
    "etcd:///user-service",
    grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy":"round_robin"}`),
    grpc.WithTransportCredentials(insecure.NewCredentials()),
)
if err != nil {
    panic(err)
}
defer conn.Close()

// 使用连接
client := pb.NewUserServiceClient(conn)
```

### 7.5. 监听服务变化

```go
// 监听服务变化
eventCh, err := reg.Watch(ctx, "user-service")
if err != nil {
    logger.Error("failed to watch", clog.Error(err))
    return
}

// 处理事件
go func() {
    for event := range eventCh {
        switch event.Type {
        case types.EventTypePut:
            logger.Info("service registered/updated",
                clog.String("service_id", event.Service.ID),
                clog.String("endpoints", strings.Join(event.Service.Endpoints, ",")))
        case types.EventTypeDelete:
            logger.Info("service deregistered",
                clog.String("service_id", event.Service.ID))
        }
    }
}()
```

## 8. 与现有组件的一致性对比

| 维度 | cache | dlock | registry |
|------|-------|-------|----------|
| **接口数量** | 1 个 | 1 个 | 1 个 ✅ |
| **工厂函数** | `New(conn, cfg, opts)` | `NewRedis(conn, cfg, opts)` | `New(conn, cfg, opts)` ✅ |
| **Lifecycle** | `Close()` | 无 | 完整 `Start/Stop/Phase` ✅ |
| **配置完整性** | ✅ | ✅ | ✅ 包含 TTL/Retry/Cache |
| **Option 模式** | ✅ | ✅ | ✅ |
| **Logger Namespace** | ✅ 自动派生 | ✅ 自动派生 | ✅ 自动派生 |
| **依赖注入** | Connector | Connector | Connector ✅ |

## 9. 总结

本设计文档完整定义了 Genesis Registry 组件的架构、接口和实现方案：

**核心特性：**

1. ✅ **统一接口** - 单一 `Registry` 接口，职责清晰
2. ✅ **gRPC 原生支持** - `GetConnection` 方法提供开箱即用的服务发现
3. ✅ **完整生命周期** - 实现 `Lifecycle` 接口，支持优雅启停
4. ✅ **本地缓存** - 可配置的服务发现缓存，提升性能
5. ✅ **自动续约** - Lease KeepAlive 机制确保服务可用性
6. ✅ **实时监听** - Watch 机制实时感知服务变化
7. ✅ **标准化** - 遵循 Genesis 组件开发规范

**与框架集成：**

* 依赖注入：通过 `connector.EtcdConnector` 获取连接
* 可观测性：通过 Option 注入 Logger/Meter/Tracer
* 生命周期：由 Container 统一管理启停顺序
* 配置管理：使用结构化配置，支持热更新
