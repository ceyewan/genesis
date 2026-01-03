# refactor(idgen): 支持 Etcd 分配 WorkerID

## 背景
`idgen` 组件中的 `AssignInstanceID` 功能用于为 Snowflake 算法分配唯一的 WorkerID（实例 ID）。目前该功能强依赖 Redis 后端实现。

考虑到微服务架构中，`Etcd` 常作为服务注册与发现的中心组件，许多服务可能仅依赖 `Etcd` 而不依赖 `Redis`。为了提高组件的适用性，我们需要扩展 `idgen` 以支持基于 `Etcd` 的 WorkerID 分配。

此外，为了符合 "Unified API Design" (#32) 的重构目标，我们需要将 `idgen` 的初始化方式重构为配置驱动模式。

## 需求
1.  **Etcd 后端支持**：实现基于 `Etcd` 的 WorkerID 分配与保活机制（利用 Lease）。
2.  **API 重构**：将独立的 `AssignInstanceID` 函数重构为统一的工厂模式，支持通过配置切换后端。
3.  **Option 注入**：使用 Option 模式注入 `connector`，替代直接参数传递。

## 详细设计

### 1. 新增 Allocator 接口
抽象 WorkerID 分配器接口，屏蔽底层实现差异。

```go
type Allocator interface {
    // Allocate 尝试分配一个 WorkerID
    Allocate(ctx context.Context) (int64, error)
    // KeepAlive 保持 WorkerID 的租约 (阻塞方法)
    KeepAlive(ctx context.Context) error
    // Stop 停止保活并释放资源
    Stop()
}
```

### 2. Config 变更
在 `idgen` 配置中增加驱动选择。

```go
type Config struct {
    // Driver: "redis" | "etcd"
    Driver string `yaml:"driver" json:"driver"`
    // ... 其他 WorkerID 分配相关配置 (如 KeyPrefix, MaxID, TTL 等)
}
```

### 3. API 变更
新增统一工厂函数：

```go
// NewWorkerIDAllocator 创建 WorkerID 分配器
// 根据 cfg.Driver 选择 redis 或 etcd 实现
func NewWorkerIDAllocator(cfg *Config, opts ...Option) (Allocator, error)
```

### 4. Option 变更
提供 Connector 注入选项：

```go
func WithRedisConnector(conn connector.RedisConnector) Option
func WithEtcdConnector(conn connector.EtcdConnector) Option
```

## 关联 Issue
- #32: refactor: 统一配置驱动初始化 API 设计 (Unified API Design)
