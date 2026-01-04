# registry - Genesis 服务注册发现组件

[![Go Reference](https://pkg.go.dev/badge/github.com/ceyewan/genesis/registry.svg)](https://pkg.go.dev/github.com/ceyewan/genesis/registry)

`registry` 是 Genesis 治理层的核心组件，提供基于 Etcd 的服务注册与发现能力，深度集成 gRPC Resolver 实现客户端负载均衡。

## 特性

- **所属层级**：L3 (Governance) — 流量治理，提供服务注册发现能力
- **核心职责**：在 Etcd 连接器的基础上提供统一的服务注册与发现语义
- **设计原则**：
    - **借用模型**：借用 Etcd 连接器的连接，不负责连接的生命周期
    - **gRPC 原生支持**：实现 gRPC resolver.Builder 接口，支持 `etcd://<service_name>` 解析
    - **实时监听**：通过 Etcd Watch 机制实时感知服务变化
    - **自动续约**：Lease 机制确保服务可用性，自动处理续租
    - **优雅下线**：Close() 方法自动撤销租约，停止监听器
    - **可观测性**：集成 clog 和 metrics，提供完整的日志和指标能力

## 目录结构（完全扁平化设计）

```text
registry/                  # 公开 API + 实现（完全扁平化）
├── README.md              # 本文档
├── registry.go            # Registry 接口和 Etcd 实现，New 构造函数
├── interface.go           # Registry 接口定义
├── config.go              # 配置结构：Config
├── service.go             # 服务模型：ServiceInstance、ServiceEvent
├── options.go             # 函数式选项：Option、WithLogger/WithMeter
├── errors.go              # 错误定义
├── resolver.go            # gRPC Resolver 实现
└── *_test.go              # 测试文件
```

**设计原则**：完全扁平化设计，所有公开 API 和实现都在根目录

## 快速开始

```go
import "github.com/ceyewan/genesis/registry"
```

### 基础使用

```go
// 1. 创建连接器
etcdConn, _ := connector.NewEtcd(&cfg.Etcd, connector.WithLogger(logger))
defer etcdConn.Close()
etcdConn.Connect(ctx)

// 2. 创建注册组件
reg, _ := registry.New(etcdConn, &registry.Config{
    Namespace:  "/genesis/services",
    DefaultTTL: 30 * time.Second,
}, registry.WithLogger(logger))
defer reg.Close()

// 3. 注册服务
service := &registry.ServiceInstance{
    ID:        "user-service-001",
    Name:      "user-service",
    Version:   "1.0.0",
    Endpoints: []string{"grpc://127.0.0.1:9001"},
}
err := reg.Register(ctx, service, 30*time.Second)

// 4. 服务发现
instances, err := reg.GetService(ctx, "user-service")
```

## 核心接口

### Registry 接口

```go
type Registry interface {
    // --- 服务注册 ---
    Register(ctx context.Context, service *ServiceInstance, ttl time.Duration) error
    Deregister(ctx context.Context, serviceID string) error

    // --- 服务发现 ---
    GetService(ctx context.Context, serviceName string) ([]*ServiceInstance, error)
    Watch(ctx context.Context, serviceName string) (<-chan ServiceEvent, error)

    // --- gRPC 集成 ---
    GetConnection(ctx context.Context, serviceName string, opts ...grpc.DialOption) (*grpc.ClientConn, error)

    // --- 资源管理 ---
    Close() error
}
```

### 服务模型

```go
// ServiceInstance 代表一个服务实例
type ServiceInstance struct {
    ID        string            `json:"id"`        // 唯一实例 ID
    Name      string            `json:"name"`      // 服务名称
    Version   string            `json:"version"`   // 版本号
    Metadata  map[string]string `json:"metadata"`  // 元数据
    Endpoints []string          `json:"endpoints"` // 服务地址列表
}

// ServiceEvent 服务变化事件
type ServiceEvent struct {
    Type    EventType        // 事件类型 (PUT/DELETE)
    Service *ServiceInstance // 服务实例信息
}
```

## 配置设计

### Config 结构

```go
type Config struct {
    // Namespace: Etcd Key 前缀，默认 "/genesis/services"
    Namespace string `json:"namespace" yaml:"namespace"`

    // DefaultTTL: 默认服务注册租约时长，默认 30s
    DefaultTTL time.Duration `json:"default_ttl" yaml:"default_ttl"`

    // RetryInterval: 重连/重试间隔，默认 1s
    RetryInterval time.Duration `json:"retry_interval" yaml:"retry_interval"`
}
```

说明：gRPC resolver 的 scheme 固定为 `etcd`，无需额外配置。

## 使用模式

### 1. 服务注册

```go
// 定义服务实例
service := &registry.ServiceInstance{
    ID:        "user-service-001",
    Name:      "user-service",
    Version:   "1.0.0",
    Endpoints: []string{"grpc://192.168.1.100:8080"},
    Metadata: map[string]string{
        "region": "us-west-1",
        "zone":   "zone-a",
        "weight": "100",
    },
}

// 注册服务，指定 30s TTL
err := reg.Register(ctx, service, 30*time.Second)
if err != nil {
    logger.Error("failed to register service", clog.Error(err))
    return
}

// 优雅下线时注销
defer reg.Deregister(ctx, service.ID)
```

### 2. 服务发现

```go
// 获取服务实例列表
instances, err := reg.GetService(ctx, "user-service")
if err != nil {
    logger.Error("failed to get service", clog.Error(err))
    return
}

logger.Info("found service instances", clog.Int("count", len(instances)))
for _, instance := range instances {
    logger.Info("service instance",
        clog.String("id", instance.ID),
        clog.String("version", instance.Version),
        clog.Any("endpoints", instance.Endpoints))
}
```

### 3. gRPC 集成（方式一：GetConnection）

```go
import "google.golang.org/grpc/credentials/insecure"

// 必须传入 grpc.WithTransportCredentials() 或其他凭证选项
conn, err := reg.GetConnection(ctx, "user-service",
    grpc.WithTransportCredentials(insecure.NewCredentials()),
)
if err != nil {
    logger.Error("failed to get connection", clog.Error(err))
    return
}
defer conn.Close()

// 使用连接调用 gRPC 服务
client := pb.NewUserServiceClient(conn)
resp, err := client.GetUser(ctx, &pb.GetUserRequest{ID: "123"})
```

### 4. gRPC 集成（方式二：原生 gRPC Dial）

```go
import (
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
)

// Registry 初始化时已自动注册 gRPC Resolver Builder
// 使用标准 gRPC Dial
conn, err := grpc.NewClient(
    "etcd:///user-service",
    grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy":"round_robin"}`),
    grpc.WithTransportCredentials(insecure.NewCredentials()),
)
if err != nil {
    logger.Error("failed to dial", clog.Error(err))
    return
}
defer conn.Close()

// 使用连接
client := pb.NewUserServiceClient(conn)
```

### 5. 监听服务变化

```go
// 监听服务变化
eventCh, err := reg.Watch(ctx, "user-service")
if err != nil {
    logger.Error("failed to watch service", clog.Error(err))
    return
}

// 处理事件
go func() {
    for event := range eventCh {
        switch event.Type {
        case registry.EventTypePut:
            logger.Info("service registered/updated",
                clog.String("service_id", event.Service.ID),
                clog.Any("endpoints", event.Service.Endpoints))
        case registry.EventTypeDelete:
            logger.Info("service deregistered",
                clog.String("service_id", event.Service.ID))
        }
    }
}()
```

## 函数式选项

```go
// WithLogger 注入日志记录器
reg, err := registry.New(etcdConn, cfg, registry.WithLogger(logger))

// WithMeter 注入指标收集器
reg, err := registry.New(etcdConn, cfg, registry.WithMeter(meter))

// 组合使用
reg, err := registry.New(etcdConn, cfg,
    registry.WithLogger(logger),
    registry.WithMeter(meter))
```

## Etcd 存储结构

服务实例在 Etcd 中的存储采用层级结构：

```
<namespace>/<service_name>/<instance_id> -> JSON(ServiceInstance)
```

例如：

- `/genesis/services/user-service/uuid-1234-5678`
- `/genesis/services/order-service/uuid-abcd-efgh`

这种设计便于：

- 使用前缀 Watch 监听特定服务的变化
- 层次化的命名空间管理
- 清晰的服务组织结构

## 负载均衡

### gRPC 集成原理

1. **Resolver 注册**：Registry 初始化时自动注册 `etcd://` scheme 的 resolver
2. **服务发现**：Resolver 通过 Watch 机制获取服务实例列表
3. **连接更新**：当服务实例发生变化时，Resolver 自动更新 gRPC 连接池
4. **负载均衡**：gRPC Balancer 在更新的连接池中进行负载分发（默认 round_robin）

### 配置负载均衡

```go
// 在 grpc.NewClient 中指定负载均衡策略
conn, err := grpc.NewClient(
    "etcd:///user-service",
    grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy":"round_robin"}`),
    grpc.WithTransportCredentials(insecure.NewCredentials()),
)
```

支持的负载均衡策略：

- `round_robin`：轮询（默认）
- `pick_first`：选择第一个
- 自定义策略

## 资源所有权模型

Registry 组件采用**借用模型 (Borrowing Model)**：

1. **连接器 (Owner)**：拥有底层 Etcd 连接，负责连接池管理
2. **Registry 组件 (Borrower)**：借用连接器中的客户端，不拥有其生命周期
3. **生命周期控制**：使用 `defer` 确保关闭顺序与创建顺序相反（LIFO）

```go
// ✅ 正确示例
etcdConn, _ := connector.NewEtcd(&cfg.Etcd, connector.WithLogger(logger))
defer etcdConn.Close() // 应用结束时关闭底层连接
etcdConn.Connect(ctx)

reg, _ := registry.New(etcdConn, &registry.Config{}, registry.WithLogger(logger))
defer reg.Close()     // 撤销租约、停止监听器
```

## 与其他组件配合

```go
func main() {
    ctx := context.Background()
    logger := clog.Must(&clog.Config{Level: "info"})

    // 1. 创建连接器
    etcdConn, _ := connector.NewEtcd(&cfg.Etcd, connector.WithLogger(logger))
    defer etcdConn.Close()
    etcdConn.Connect(ctx)

    // 2. 创建注册组件
    reg, _ := registry.New(etcdConn, &registry.Config{}, registry.WithLogger(logger))
    defer reg.Close()

    // 3. 注册当前服务
    service := &registry.ServiceInstance{
        ID:        "my-service-001",
        Name:      "my-service",
        Endpoints: []string{"grpc://127.0.0.1:8080"},
    }
    err := reg.Register(ctx, service, 30*time.Second)
    if err != nil {
        logger.Error("failed to register", clog.Error(err))
        return
    }
    defer reg.Deregister(ctx, service.ID)

    // 4. 调用其他服务
    userConn, _ := reg.GetConnection(ctx, "user-service",
        grpc.WithTransportCredentials(insecure.NewCredentials()),
    )
    defer userConn.Close()

    userClient := pb.NewUserServiceClient(userConn)
    user, err := userClient.GetUser(ctx, &pb.GetUserRequest{ID: "123"})
}
```

### 6. StreamManager（每实例一条流）

当需要为每个实例维护一条双向流时，使用 StreamManager 自动管理连接、流和实例上下线：

```go
manager, err := registry.NewStreamManager(reg, registry.StreamManagerConfig{
    ServiceName: "stream-service",
    DialOptions: []grpc.DialOption{
        grpc.WithTransportCredentials(insecure.NewCredentials()),
    },
    Factory: func(ctx context.Context, conn *grpc.ClientConn, instance *registry.ServiceInstance) (grpc.ClientStream, error) {
        client := pb.NewTestServiceClient(conn)
        return client.StreamCall(ctx)
    },
})
if err != nil {
    logger.Error("failed to create stream manager", clog.Error(err))
    return
}
defer manager.Stop(ctx)

if err := manager.Start(ctx); err != nil {
    logger.Error("failed to start stream manager", clog.Error(err))
    return
}

// 获取当前流快照（instanceID -> stream）
streams := manager.Streams()
```

## 最佳实践

1. **服务命名**：使用有意义的服务名，如 `user-service`、`order-service`
2. **实例 ID**：使用 UUID 或包含主机信息的唯一标识符
3. **TTL 设置**：根据服务特点设置合理的 TTL，建议 30s-60s
4. **元数据**：在 Metadata 中存储 region、zone、version 等有用信息
5. **错误处理**：使用 `xerrors.Wrapf()` 包装错误，保留错误链
6. **优雅下线**：确保在应用退出时调用 `Deregister` 或依赖 `Close()` 自动处理
7. **监控**：通过 `WithLogger` 和 `WithMeter` 注入可观测性组件

## 完整示例

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/ceyewan/genesis/registry"
    "github.com/ceyewan/genesis/clog"
    "github.com/ceyewan/genesis/connector"
    "google.golang.org/grpc"
    pb "github.com/your-org/your-proto"
)

func main() {
    ctx := context.Background()
    logger := clog.Must(&clog.Config{Level: "info"})

    // 1. 创建 Etcd 连接器
    etcdConn, err := connector.NewEtcd(&connector.EtcdConfig{
        Endpoints:   []string{"localhost:2379"},
        DialTimeout: 5 * time.Second,
    }, connector.WithLogger(logger))
    if err != nil {
        panic(err)
    }
    defer etcdConn.Close()

    // 2. 连接到 Etcd
    if err := etcdConn.Connect(ctx); err != nil {
        panic(err)
    }

    // 3. 创建 Registry 实例
reg, err := registry.New(etcdConn, &registry.Config{
        Namespace:  "/genesis/services",
        DefaultTTL: 30 * time.Second,
    }, registry.WithLogger(logger))
    if err != nil {
        panic(err)
    }
    defer reg.Close()

    // 4. 注册当前服务
    service := &registry.ServiceInstance{
        ID:      fmt.Sprintf("order-service-%s", getPodID()),
        Name:    "order-service",
        Version: "1.0.0",
        Endpoints: []string{
            "grpc://127.0.0.1:8080",
        },
        Metadata: map[string]string{
            "region":    "us-west-1",
            "zone":      "zone-a",
            "weight":    "100",
            "commit":    getGitCommit(),
        },
    }

    err = reg.Register(ctx, service, 30*time.Second)
    if err != nil {
        panic(err)
    }
    logger.Info("service registered successfully",
        clog.String("service_id", service.ID),
        clog.Any("endpoints", service.Endpoints))

    // 5. 监听其他服务变化
    go watchUserService(reg, logger)

    // 6. 调用其他服务
    callUserService(reg, logger)

    // 7. 保持服务运行
    logger.Info("service is running...")
    select {}
}

func watchUserService(reg registry.Registry, logger clog.Logger) {
    ctx := context.Background()
    eventCh, err := reg.Watch(ctx, "user-service")
    if err != nil {
        logger.Error("failed to watch user service", clog.Error(err))
        return
    }

    for event := range eventCh {
        switch event.Type {
        case registry.EventTypePut:
            logger.Info("user service registered/updated",
                clog.String("service_id", event.Service.ID),
                clog.String("version", event.Service.Version),
                clog.Any("endpoints", event.Service.Endpoints))
        case registry.EventTypeDelete:
            logger.Info("user service deregistered",
                clog.String("service_id", event.Service.ID))
        }
    }
}

func callUserService(reg registry.Registry, logger clog.Logger) {
    ctx := context.Background()

    // 方式一：使用 GetConnection
    conn, err := reg.GetConnection(ctx, "user-service",
        grpc.WithTransportCredentials(insecure.NewCredentials()),
    )
    if err != nil {
        logger.Error("failed to get user service connection", clog.Error(err))
        return
    }
    defer conn.Close()

    client := pb.NewUserServiceClient(conn)
    resp, err := client.GetUser(ctx, &pb.GetUserRequest{ID: "123"})
    if err != nil {
        logger.Error("failed to call user service", clog.Error(err))
        return
    }

    logger.Info("user service call successful",
        clog.String("user_id", resp.User.ID),
        clog.String("user_name", resp.User.Name))
}
```
