# Genesis 组件重构计划：配置驱动与统一 API

## 1. 背景与目标

当前 Genesis 组件库的初始化方式依赖于显式的构造函数注入（如 `cache.New(redisConn, ...)`），这种方式符合显式依赖原则，但在需要通过配置切换底层实现（如从 Redis 切换到 Memory，或从 MySQL 切换到 SQLite）时，需要修改业务代码。

本次重构的目标是：
1.  **统一 API**：所有核心组件提供统一的 `New(cfg, opts...)` 入口。
2.  **配置驱动**：通过 `Config.Driver` 字段控制底层实现的选择。
3.  **零代码修改切换**：业务层通过 Option 注入所有可能用到的资源，组件根据配置自动选择，实现切换配置即切换后端。
4.  **保持显式依赖**：拒绝引入 Service Locator 或隐式 Registry，坚持使用 Option 模式注入依赖。

## 2. 设计规范

### 2.1 统一入口
所有组件提供统一的工厂函数：
```go
func New(cfg *Config, opts ...Option) (Interface, error)
```

### 2.2 统一配置
所有组件的 Config 结构体包含标准化的 `Driver` 字段：
```go
type Config struct {
    // Driver 指定后端实现类型，如 "redis", "mysql", "standalone"
    Driver string `json:"driver" yaml:"driver"`
    // ... 其他具体配置
}
```

### 2.3 统一选项
各组件的 `options.go` 提供类型安全的 Connector 注入方法：
```go
func WithRedisConnector(conn connector.RedisConnector) Option
func WithEtcdConnector(conn connector.EtcdConnector) Option
// ...
```

## 2.4 补充建议（风险控制）
1. **Driver 强校验**：`Config.Driver` 必须严格校验，未知值直接返回错误，禁止隐式回退。
2. **默认值显式化**：若组件只有单一后端（如当前 Idempotency 仅 Redis），`Driver` 可选，但默认值必须在 `Config.setDefaults()` 明确设置，避免空字符串导致误判。
3. **按需注入**：Option 注入只覆盖“该组件可能用到的 Connector”，不要做全局注册或 Locator，保证依赖可读性。
4. **测试策略**：单元测试只需注入对应 Connector mock；配置驱动测试覆盖 `Driver` 切换与缺失 Connector 的错误分支。
5. **错误规范**：缺失 Connector、Driver 非法等情况返回清晰错误（可复用 xerrors + 统一错误码）。
6. **文档与示例**：所有示例迁移为 `New(cfg, opts...)` 形式，并提供不同 Driver 的配置示例。

## 3. 组件详细变更

### 3.1 Cache 组件
-   **API**:
    -   `New(conn, cfg)` -> 重命名为 `NewRedis(conn, cfg)`
    -   新增 `New(cfg, opts...)`
-   **Config**: 新增 `Driver` ("redis", "memory")
-   **Options**: 新增 `WithRedisConnector`

### 3.2 Idempotency 组件
-   **API**:
    -   `New(conn, cfg)` -> 重命名为 `NewRedis(conn, cfg)`
    -   新增 `New(cfg, opts...)`
-   **Config**: 新增 `Driver` ("redis")
-   **Options**: 新增 `WithRedisConnector`
    -   **建议**: 目前仅 Redis 后端，`Driver` 默认值需明确设置为 "redis"，并在未来扩展时保持兼容。

### 3.3 DLock 组件
-   **API**:
    -   保留 `NewRedis`, `NewEtcd`
    -   新增 `New(cfg, opts...)`
-   **Config**: 新增 `Driver` ("redis", "etcd")
-   **Options**: 新增 `WithRedisConnector`, `WithEtcdConnector`

### 3.4 RateLimit 组件
-   **API**:
    -   `New` 逻辑更新，支持通过 `Driver` 字段分发
    -   保留 `NewStandalone`, `NewDistributed`
-   **Config**: `Mode` 字段迁移/标准化为 `Driver` ("standalone", "distributed")
-   **Options**: 优化 Connector 注入

### 3.5 MQ 组件
-   **API**:
    -   仅保留 `New(cfg, opts...)` 作为统一入口
-   **Config**: 新增 `Driver` ("redis", "nats")
-   **Options**: 新增 `WithRedisConnector`, `WithNATSConnector`
    -   **建议**: Driver 枚举需区分 NATS Core/JetStream（如 "nats_core"、"nats_jetstream"），避免语义歧义。

### 3.6 DB 组件
-   **API**:
    -   `New(conn, cfg)` -> 修改签名为 `New(cfg, opts...)`
-   **Config**: 新增 `Driver` ("mysql", "sqlite")
-   **Options**: 新增 `WithMySQLConnector`, `WithSQLiteConnector`
    -   **建议**: 当 `Driver=sqlite` 且启用分片时应直接报错，避免静默忽略分片配置。

### 3.7 IdGen 组件
-   **需求背景**: `AssignInstanceID` 用于分配微服务实例唯一 ID（WorkerID），目前强依赖 Redis。考虑到服务注册常依赖 Etcd，且部分服务可能不使用 Redis，需支持 Etcd 后端。
-   **API**:
    -   `AssignInstanceID` (破坏性变更) -> 重构为 `NewWorkerIDAllocator(cfg, opts...)` 或类似工厂函数。
    -   返回 `Allocator` 接口，屏蔽底层实现。
-   **Config**: 新增 `Driver` ("redis", "etcd")。
-   **Options**: 新增 `WithRedisConnector`, `WithEtcdConnector`。

## 4. 实施步骤

1.  **Phase 1**: 重构 Cache 和 Idempotency 组件。
2.  **Phase 2**: 重构 DLock 和 RateLimit 组件。
3.  **Phase 3**: 重构 MQ 和 DB 组件。
4.  **Phase 4**: 重构 IdGen 组件（支持 Etcd 分配 WorkerID）。
5.  **Phase 5**: 验证所有组件的编译和测试兼容性。
