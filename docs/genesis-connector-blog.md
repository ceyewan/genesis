# Genesis Connector：微服务外部连接管理的设计与实现

Genesis `connector` 是基础设施层（L1）的核心组件，负责管理与外部服务（MySQL、PostgreSQL、SQLite、Redis、Etcd、NATS、Kafka）的原始连接。它通过封装复杂的连接细节、提供健康检查、生命周期管理以及与 L0 组件（日志、错误）的深度集成，为上层组件提供稳定、类型安全的连接能力。

---

## 0 摘要

- **统一抽象**：`connector` 把外部连接建模为连接器（Connector）接口，隐藏底层客户端的实现差异，提供统一的初始化、连接、健康检查与关闭语义。
- **资源所有权**：遵循“谁创建，谁释放”的原则，连接器拥有底层连接的生命周期所有权，上层组件仅借用客户端，避免资源泄露与双重关闭问题。
- **Fail-fast**：连接器的两阶段初始化（New 与 Connect）让配置验证与 I/O 操作分离，在应用启动阶段实现 Fail-fast，同时在系统就绪后再建立实际连接。
- **高效探活**：健康检查的主动探测（HealthCheck）与状态缓存（IsHealthy）分离，避免高频率健康检查对下游服务造成压力，同时支持调用方的快速状态查询。
- **类型安全**：泛型接口 `TypedConnector[T]` 提供类型安全的客户端访问，避免运行时类型断言。

---

## 1 背景：微服务外部连接要解决的"真实问题"

在微服务场景中，一个服务通常需要与多个外部系统交互：

- **存储层**：MySQL、PostgreSQL、Redis、MongoDB
- **协调层**：Etcd、Consul
- **消息层**：Kafka、NATS、RabbitMQ
- **缓存层**：Redis、Memcached

这些系统的客户端库差异巨大：

- Redis 使用 `go-redis/redis` 通过 NewClient 初始化并隐式连接。
- MySQL 和 PostgreSQL 使用 GORM 通过 Open 初始化。
- Etcd 使用 `etcd/clientv3` 连接时阻塞。
- Kafka 使用 `franz-go` 的 NewClient 创建。

如果直接在业务代码中使用这些客户端，会面临以下问题：

- **初始化模式不统一**：有的需要显式 Connect，有的在 New 时就建立连接。
- **健康检查方式各异**：有的提供 Ping，有的需要发送测试请求或连接时阻塞。
- **资源管理复杂**：连接池超时、空闲连接清理、优雅关闭等逻辑分散。
- **错误处理不一致**：不同客户端的错误类型不同，难以统一处理。
- **可观测性缺失**：缺少统一的日志命名空间、指标埋点、链路追踪。

结论是需要一个统一的抽象层来管理外部连接，封装差异，提供一致的语义。这正是 `connector` 组件要做的事。

---

## 2 核心设计：两阶段初始化与资源所有权

### 2.1 两阶段初始化：New 与 Connect 分离

`connector` 的所有连接器都遵循两阶段初始化模式：

1.  **New 阶段**：创建连接器实例，仅验证配置的正确性，不执行任何 I/O 操作。
2.  **Connect 阶段**：调用 Connect 方法建立实际的连接。

这种设计的收益是多方面的：

- **Fail-fast**：让配置错误在启动阶段就能发现，而不是运行时才暴露。
- **灵活的连接时机**：可以在应用完全就绪后再连接，如等待依赖服务启动。
- **重试友好**：多次调用 Connect 是安全的，便于实现重试逻辑。

### 2.2 资源所有权：谁创建，谁释放

`connector` 采用借用模型：

- **连接器（Owner）**：拥有底层连接，负责 Close，生命周期由应用层通过 defer 管理。
- **上层组件（Borrower）**：如 cache、db、mq、dlock 等，它们借用连接器中的客户端，Close 通常是 no-op。

这种设计遵循 LIFO 关闭顺序，使用 defer 确保关闭顺序与创建顺序相反。这解决了直接在多个组件中创建底层客户端时可能出现的问题：连接被多次创建导致资源浪费；不知道应该由谁负责关闭；某个组件关闭后影响其他仍在使用的组件。

---

## 3 接口设计：统一抽象与类型安全

### 3.1 基础连接器接口

所有连接器都实现 Connector 基础接口，包含五个方法：

- `Connect`：建立连接，幂等多次调用安全。
- `Close`：关闭连接，幂等多次调用安全。
- `HealthCheck`：主动探测连接健康状态，有 I/O 开销。
- `IsHealthy`：返回缓存的连接状态，无 I/O 开销。
- `Name`：返回连接器名称用于日志标识。

### 3.2 类型化连接器接口

为了提供类型安全的客户端访问，引入泛型接口 `TypedConnector[T]`，它继承 Connector 并增加 `GetClient` 方法返回类型 T 的客户端。

使用示例：

```go
var client *redis.Client = conn.GetClient()
```

类型明确无需类型断言。这避免了传统代码中常见的类型断言，如 `client := conn.(*RedisConnector).GetClient()`。

### 3.3 专用接口

每种连接器还提供专用接口，便于在函数签名中明确依赖。

- `RedisConnector` 继承 `TypedConnector[*redis.Client]`
- `MySQLConnector` 和 `PostgreSQLConnector` 继承各自的 `TypedConnector[*gorm.DB]`

这种设计让依赖类型在编译期确定，而非运行时断言。

---

## 4 配置设计：扁平化与默认值

### 4.1 配置结构原则

所有配置结构遵循扁平化原则，不使用嵌套的子配置，所有字段平铺。核心参数如 Host、Port、Addr 等必填字段在 validate 时检查。可选参数有默认值，通过 `setDefaults` 方法自动填充。

对于数据库类连接器，支持直接传入完整 DSN，优先级高于独立字段。

### 4.2 配置处理流程

配置处理在 New 函数内部自动完成。

1.  调用 `cfg.validate()` 检查必填字段。
2.  内部先调用 `cfg.setDefaults()` 自动填充默认值。
3.  业务侧无需手动调用 SetDefaults 或 Validate，简化使用。

---

## 5 健康检查设计：主动探测与状态缓存

### 5.1 双接口设计

健康检查分为两个接口：

- `HealthCheck`：主动探测，有 I/O 开销，用于定期健康检查或 K8s 存活探针。
- `IsHealthy`：状态查询，无 I/O，用于请求前快速判断或业务逻辑降级。

### 5.2 使用场景

- **定时健康检查**：使用 `HealthCheck`，如每 30 秒一次，定期更新状态缓存。
- **请求前快速判断**：使用 `IsHealthy`，避免正常请求增加延迟。
- **K8s 存活探针**：使用 `HealthCheck`，需要实际探测连接状态。
- **业务逻辑降级**：使用 `IsHealthy`，如缓存服务不可用时直接访问数据库。

---

## 6 可观测性集成：日志与错误处理

### 6.1 日志集成

通过 `WithLogger` 选项注入 `clog.Logger`，自动添加连接器命名空间。日志输出自动包含：

- `namespace`: 固定为 "connector"
- `connector`: 连接器类型（redis/mysql/etcd 等）
- `name`: 来自配置的连接器名称

这种结构便于按服务或连接器类型过滤日志。

### 6.2 错误处理

使用 Genesis `xerrors` 组件提供一致的错误类型。定义了 `ErrNotConnected`、`ErrAlreadyClosed`、`ErrConnection`、`ErrTimeout`、`ErrConfig`、`ErrClientNil`、`ErrHealthCheck` 等标准错误。

使用 `xerrors.Is` 检查特定错误类型进行精确的错误处理。错误包装时使用 `xerrors.Wrapf` 添加连接器名称等上下文信息，便于问题定位。

---

## 7 实现细节：Redis 连接器的可观测性增强

### 7.1 Tracing 支持

Redis 连接器新增了 `EnableTracing` 配置项。当启用时，通过 `redisotel.InstrumentTracing` 包装客户端，自动为所有 Redis 命令添加分布式追踪能力。这让 Redis 操作能自动关联到 trace 中的当前 span，实现端到端的可观测性，无需业务代码手动注入 trace context。

### 7.2 连接池配置

Redis 连接器支持完整的连接池配置，包括：

- `PoolSize`：连接池大小
- `MinIdleConns`：最小空闲连接数
- `DialTimeout`：连接超时
- `ReadTimeout`：读取超时
- `WriteTimeout`：写入超时

这些参数直接影响应用在高并发场景下的表现，合理的配置可以避免连接数爆炸导致的下游服务压力。

---

## 8 支持的连接器类型

| 类型           | 接口                  | 底层客户端         | 工厂函数        |
| :------------- | :-------------------- | :----------------- | :-------------- |
| **Redis**      | `RedisConnector`      | `*redis.Client`    | `NewRedis`      |
| **MySQL**      | `MySQLConnector`      | `*gorm.DB`         | `NewMySQL`      |
| **PostgreSQL** | `PostgreSQLConnector` | `*gorm.DB`         | `NewPostgreSQL` |
| **SQLite**     | `SQLiteConnector`     | `*gorm.DB`         | `NewSQLite`     |
| **Etcd**       | `EtcdConnector`       | `*clientv3.Client` | `NewEtcd`       |
| **NATS**       | `NATSConnector`       | `*nats.Conn`       | `NewNATS`       |
| **Kafka**      | `KafkaConnector`      | `*kgo.Client`      | `NewKafka`      |

---

## 9 最佳实践

### 9.1 分离创建与连接

在应用启动阶段先调用 New 验证配置，然后在系统就绪后再调用 Connect。避免在 init 函数中直接连接，这会导致启动失败时无法优雅退出。

```go
func main() {
    // 启动阶段：验证配置
    redisConn, err := connector.NewRedis(&cfg.Redis)
    if err != nil {
        log.Fatalf("invalid redis config: %v", err)
    }

    // 等待依赖服务...
    time.Sleep(5 * time.Second)

    // 系统就绪：建立连接
    if err := redisConn.Connect(ctx); err != nil {
        log.Fatalf("failed to connect: %v", err)
    }
}
```

### 9.2 必须使用 defer Close

始终使用 defer 确保资源释放，即使在 panic 场景下也能正确关闭。这遵循谁创建谁释放的原则，连接器由应用层创建和关闭，上层组件只借用。

```go
conn, err := connector.NewRedis(&cfg.Redis)
if err != nil {
    return err
}
defer conn.Close() // 必须释放
```

### 9.3 单例使用

在微服务中，每个数据源应只创建一个 Connector 实例并在组件间共享。避免为每个组件创建独立连接器，这会导致连接数爆炸。重复创建连接器是反模式，应通过依赖注入共享连接器。

### 9.4 注入日志

务必通过 `WithLogger` 注入日志组件，以便进行线上监控和排障。未注入日志时连接状态变化是静默的，排障困难。注入后可以追踪连接建立、关闭、健康检查等关键事件。

### 9.5 错误处理

使用 `xerrors.Is` 检查特定错误类型进行精确的错误处理。连接失败可能是网络问题或服务不可用，配置错误是程序 bug 需修复配置。其他错误需要根据上下文判断处理策略。

---

## 10 测试策略：testcontainers 集成测试

`connector` 使用 testcontainers 进行集成测试，确保与真实服务的兼容性。测试组织分为单元测试和集成测试。每个连接器的集成测试覆盖完整生命周期，包括 New、Connect、HealthCheck、IsHealthy、GetClient、Close。

---

## 11 设计权衡与未来方向

### 11.1 当前设计的权衡

- **两阶段初始化**：增加一行代码，但实现 Fail-fast 和灵活连接时机。
- **借用模型**：让上层组件 Close 是 no-op，但避免了资源所有权混乱。
- **健康状态缓存**：减少 I/O，但状态可能有短暂延迟。
- **testcontainers 测试**：更真实，但需要 Docker 环境。

### 11.2 可能的扩展方向

- **更多连接器**：MongoDB Connector、Elasticsearch Connector、RabbitMQ Connector。
- **连接池指标**：导出连接池使用率、等待队列长度等指标。
- **连接预热**：启动时主动建立 N 个连接，避免首请求延迟。
- **配置校验增强**：支持更多验证规则。

---

## 12 总结

`connector` 组件的核心价值在于五个方面：

1.  **统一抽象**：隐藏不同客户端库的差异，提供一致的初始化、连接、健康检查语义。
2.  **资源所有权明确**：谁创建谁释放，借用模型避免资源泄露和双重关闭。
3.  **Fail-fast 原则**：配置验证在启动阶段完成，连接失败快速暴露。
4.  **可观测性集成**：与 clog 和 xerrors 深度集成，统一日志命名空间和错误类型。
5.  **类型安全**：泛型接口提供编译时类型检查，避免运行时类型断言。

在微服务架构中，外部连接管理是基础设施级别的能力。`connector` 组件的设计目标是让业务开发者不需要关心连接细节，只需要创建、连接、使用、关闭即可。
