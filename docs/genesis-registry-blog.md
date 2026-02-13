# Genesis Registry：服务注册发现与 gRPC 解析器核心原理

Genesis `registry` 是治理层（L3）的服务注册发现组件，基于 Etcd 提供服务注册、实例注册、变更订阅与租约管理能力，并内置 gRPC resolver 让客户端可以直接使用 `etcd:///service-name` 做负载均衡的 gRPC 连接。它的核心目标是在保持服务实例生命周期有生有死的前提下，提供统一的服务发现机制，屏蔽底层 Etcd 的复杂性。

---

## 0 摘要

`registry` 组件对外提供 Registry 接口，包含 Register、Deregister、GetService、Watch 四个核心方法。Register 用于注册服务实例并绑定租约，Deregister 用于注销实例并释放租约，GetService 用于获取服务实例列表，Watch 用于订阅服务变更事件。配置驱动支持 etcd 和 consul 预留，可通过配置切换后端实现。组件内置了 gRPC resolver，客户端可直接使用 `etcd:///service-name` 格式地址进行 gRPC 调用，自动完成服务发现和负载均衡。租约管理通过 Lease 机制保证实例生命周期，支持租约自动续期和主动释放。事件订阅模式支持全量和增量两种方式，适配不同消费场景。组件遵循 Genesis 显式依赖注入规范，支持 WithLogger 和 WithMeter 注入。

---

## 1 背景：为什么需要服务注册发现

在微服务或云原生环境中，服务实例动态变化是常态。新实例启动时需要注册到服务中心，下线时需要注销。调用方需要实时获取可用的服务实例列表，并监听服务变更。如果没有统一的注册发现机制，每个服务都要自行实现服务发现，导致重复代码和维护成本上升。

Etcd 是 CNCF 最常用的服务注册中心，但在服务发现之外还提供了租约、KV 存储等能力。Genesis registry 组件的设计不是简单封装 Etcd 客户端，而是提供一套统一的服务发现抽象，让业务代码无需关心底层实现细节。这种抽象让业务可以在不同环境下切换注册中心，而无需修改业务代码。

---

## 2 核心设计：统一抽象与配置驱动

### 2.1 接口抽象

`registry` 组件对外提供极简的接口设计，隐藏了底层 Etcd 的复杂交互：

```go
type Registry interface {
    // Register 注册服务实例
    // ttl: 租约有效期，超时后若无续约服务将自动下线
    Register(ctx context.Context, service *ServiceInstance, ttl time.Duration) error

    // Deregister 注销服务实例
    Deregister(ctx context.Context, serviceID string) error

    // GetService 获取服务实例列表
    GetService(ctx context.Context, serviceName string) ([]*ServiceInstance, error)

    // Watch 监听服务实例变化
    // 返回事件通道，实时接收服务上下线事件
    Watch(ctx context.Context, serviceName string) (<-chan ServiceEvent, error)

    // GetConnection 获取到指定服务的 gRPC 连接
    // 内置 Resolver 和 Balancer，提供开箱即用的负载均衡连接
    GetConnection(ctx context.Context, serviceName string, opts ...grpc.DialOption) (*grpc.ClientConn, error)

    // Close 停止后台任务并清理资源
    Close() error
}
```

Registry 接口设计非常克制，仅包含核心的服务管理方法。这种设计让调用方可以用最少的 API 完成服务发现和管理。组件通过 Config 的 Driver 字段选择后端实现，默认使用 etcd 驱动，支持 consul 作为预留扩展。这种配置驱动的方式让业务可以在不同环境下切换注册中心，而无需修改业务代码。

服务实例的生命周期通过租约机制管理。注册时指定 TTL，租约即将过期时 Etcd 会自动删除对应 key。服务实例需要通过定期续约保持租约有效，或者主动释放后重新注册。这种设计避免了僵尸实例占用资源的问题，确保服务实例的生命周期受控。

组件内置了一个 gRPC resolver，将 Etcd 的服务发现能力转化为 gRPC 连接。客户端只需要使用 `etcd:///service-name` 格式的地址，resolver 会自动解析并建立 gRPC 连接，返回一个已经做好负载均衡的客户端连接。这层抽象让业务代码完全不需要处理服务发现的细节。

---

## 3 注册模型：Etcd Key 组织结构

Etcd 的 Key 采用层级化命名空间组织，格式为 `/<namespace>/<service>/<instance_id>`。例如一个名为 `user` 的服务部署 3 个实例，Etcd 中的 Key 分别为 `/genesis/user/0`、`/genesis/user/1`、`/genesis/user/2`。这种设计让服务发现能够精确控制到每个实例的生命周期。

ServiceInstance 包含服务实例的完整信息。Name 是服务名，Namespace 是命名空间，ID 是实例唯一标识，Endpoints 包含该实例的所有访问地址。Metadata 是服务的自定义元数据字典。LeaseID 是租约标识，用于后续续约或释放。CreateRevision 和 ModRevision 分别记录创建版本号和修改版本号，用于实现乐观并发控制。

---

## 4 注册流程：从申请到就绪

服务注册的入口是 Register 方法。调用方需要提供服务名、实例 ID、监听地址、元数据等完整信息。组件首先校验配置的有效性，然后调用 Register 方法向 Etcd 注册服务实例。组件不会直接创建服务实例，而是先创建 Lease 租约并绑定到服务实例。租约记录了服务实例的唯一标识，续期时可以自动延长或释放。这种设计确保了服务实例的生命周期受控，避免租约过期导致实例意外下线。

注册前组件会进行多轮校验。首先检查 Key 是否已被占用，如果存在说明实例正在运行，返回错误。然后检查服务定义是否与已有冲突，类型和端口是否匹配。最后校验监听地址的可连通性，确保服务能够正常响应 gRPC 请求。

---

## 5 订阅机制：全量与增量

Watch 方法返回的事件通道包含服务的完整状态变更。当触发事件时，组件会先从本地缓存中获取当前服务实例列表，然后对比新状态，只推送真正发生变化的部分。全量订阅适合首次连接或需要完整状态的场景。

对于已建立连接的调用方，全量订阅会带来大量事件。Watch 方法支持通过 startRevision 参数指定起始版本号，只推送该版本之后的变化。增量订阅减少了事件数量，降低了处理压力。

---

## 6 Resolver：从服务发现到 gRPC 连接

组件内置的 gRPC resolver 实现了服务发现到 gRPC 连接的转换。它解析 `etcd:///service-name` 格式的地址，提取 hostname 和 port，然后创建 gRPC 客户端连接。Resolver 还支持连接池和负载均衡，提高连接的复用性和稳定性。

Resolver 维护了一个连接池，避免每次调用都创建新连接。连接池会根据订阅事件动态更新，当服务实例下线时会自动移除失效连接。负载均衡策略采用轮询方式，在多个健康实例间均匀分配流量，避免单点过载。

---

## 7 实战落地

### 7.1 初始化

Registry 组件依赖 Connector 提供底层连接，遵循"依赖注入"模式：

```go
// 1. 初始化 Etcd 连接器
etcdConn, _ := connector.NewEtcd(&cfg.Etcd, connector.WithLogger(logger))
defer etcdConn.Close()
etcdConn.Connect(ctx)

// 2. 初始化 Registry
reg, _ := registry.New(etcdConn, &registry.Config{
    Namespace:  "/genesis/services",
    DefaultTTL: 30 * time.Second,
}, registry.WithLogger(logger))
defer reg.Close()
```

### 7.2 服务注册（服务端）

```go
service := &registry.ServiceInstance{
    ID:        "user-service-001",
    Name:      "user-service",
    Endpoints: []string{"grpc://127.0.0.1:8080"},
    Metadata:  map[string]string{"version": "v1.0"},
}

// 注册并保持 30s 租约
err := reg.Register(ctx, service, 30*time.Second)
```

### 7.3 服务发现与调用（客户端）

推荐使用 `GetConnection` 直接获取 gRPC 连接，它会自动处理服务发现和负载均衡：

```go
// 直接获取连接（内置了 resolver）
conn, err := reg.GetConnection(ctx, "user-service")
defer conn.Close()

// 创建客户端 stub
client := pb.NewUserServiceClient(conn)
resp, err := client.GetUser(ctx, &pb.IdRequest{Id: 1})
```

---

## 8 设计权衡与最佳实践

组件采用单 active registry 模式，进程内只维护一个 active registry 实例。关闭后再调用 New 会重新创建，返回 ErrRegistryAlreadyInitialized 错误。这种设计简化了资源管理，避免多实例间的状态不一致问题。

Resolver 内置了本地缓存，避免每次解析地址都请求 etcd。缓存有 TTL，过期后会重新获取。Watch 事件会主动更新缓存，保证地址信息的及时性。Resolver 支持多种地址格式，包括 `grpc://host:port`、`http://host:port` 等。解析后会验证连通性，只有可用的地址才会被 gRPC 客户端使用。

通过 Option 可以配置连接池大小、最大连接数、空闲超时等参数。合理的连接池配置可以平衡性能和资源消耗，适应不同业务场景。

---

## 9 总结

`registry` 组件的核心价值在于三个方面。首先提供统一的服务注册发现抽象，屏蔽 Etcd 的复杂性，让业务代码无需关心底层实现。其次通过内置的 gRPC resolver 实现 Etcd 地址到 gRPC 连接的自动转换，提供开箱即用的负载均衡能力。最后通过租约机制和生命周期管理，保证服务实例的受控运行。

这种设计让业务开发者可以专注于业务逻辑，而把服务发现和连接管理的复杂性交给基础设施层组件处理。
