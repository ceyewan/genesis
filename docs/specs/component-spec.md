# Genesis 组件开发规范 (Component Specification)

本规范定义了 Genesis 框架中所有组件（Component）和连接器（Connector）必须遵循的开发标准。旨在确保框架的一致性、可测试性和可观测性。

## 1. 核心原则

1. **依赖注入 (Dependency Injection):** 组件不得在内部创建依赖（如连接器、Logger、配置加载器），必须通过构造函数或 Option 注入。
2. **配置分离 (Configuration Decoupling):** 组件不得直接读取配置文件或环境变量，必须通过结构体参数接收配置。
3. **双模式支持 (Dual Mode Support):**
    * **容器模式 (Containerized) – 生产环境推荐：** 组件实例由 `pkg/container` 统一构建和管理生命周期，业务代码仅通过 `Container` 获取组件接口。
    * **独立模式 (Standalone)：** 组件提供标准的 `New` 工厂函数，允许在不依赖 Container 的情况下独立实例化（主要用于单元测试和工具脚本），调用方需要自行管理依赖与资源释放。
4. **可观测性优先 (Observability First):** 日志、Metrics 和 Tracing 必须作为一级公民，通过统一接口注入，组件内部只依赖抽象接口，不直接依赖 OTel 等具体实现。

## 2. 目录结构规范 (已对齐重构计划)

按照当前重构执行计划，各层的目录组织有所差别：

- Level 1 (Infrastructure): 保持 `pkg/` (对外 API) + `internal/` (实现) 的分离。
- Level 2 / Level 3 (Business / Governance): 为了扁平化 API 并减少导入前缀，`types/` 子包将被扁平化并移入 `pkg/<component>/` 根目录；实现细节以非导出结构体封装在同包中或放在 `internal/`（见下一条约定）。

建议的目录结构示例：

```text
# 对于 L2 / L3 组件（扁平化）
pkg/<component>/
├── component.go        # Factory (New)、导出接口与导出类型 (Config, Errors, Interface)
├── options.go          # Option 模式定义 (WithLogger, WithMeter, WithTracer 等)
├── impl.go             # 非导出实现（如 type limiter struct{}）
└── adapter/            # (可选) protocol adapters 或 middleware

# 对于 L1 基础设施（保留 internal 实现）
pkg/<component>/
├── api.go              # 导出接口、Config 定义
└── internal/
    └── impl.go         # 实现细节、驱动逻辑（connection pools, health checks）
```

说明：此处的关键点是将组件面向用户的类型（`Config`, `Interface`, `Errors`）放在 `pkg/<component>/` 根目录，避免 `pkg/<component>/types` 的冗长导入路径；同时通过非导出实现或 `internal/` 控制可见性。

## 3. 初始化规范

### 3.1 工厂函数签名（已调整，参照重构计划）

为简化调用方代码与提高一致性，工厂函数需遵循下列约定：

- 必选参数只用于传递核心依赖（例如 Connector 接口或其他必需服务）；禁止在必选参数中传入 Logger/Meter/Tracer。
- 可选项（如日志、指标、链路）使用 `Option` 模式注入。
- `New` 不应执行阻塞 I/O；所有 I/O 或后台任务应在 `Start(ctx)` 中完成。

推荐签名示例：

```go
// New 创建组件实例（示例）
// conn: 核心连接器或依赖
// opts: 可选依赖（WithLogger, WithMeter, WithTracer, WithConfig 等）
func New(conn connector.RedisConnector, opts ...Option) (Interface, error)
```

如果组件确实需要一个强类型的配置结构体（推荐做法），可将其通过 `WithConfig(cfg)` 形式作为 Option 传入，或在容器模式下由 Container 在调用 `New` 前裁剪并传入。

### 3.2 Option 模式

必须使用 Option 模式处理可选依赖，特别是**日志**和**Metrics**。

```go
// pkg/<component>/options.go

type options struct {
    logger  clog.Logger
    meter   metrics.Meter   // 指标接口
    tracer  trace.Tracer    // 链路追踪接口
}

type Option func(*options)

// WithLogger 注入日志记录器
// 组件内部必须自动追加 Namespace: logger.WithNamespace("<component>")
func WithLogger(l clog.Logger) Option {
    return func(o *options) {
        o.logger = l.WithNamespace("<component>")
    }
}

// WithMeter 注入指标 Meter
func WithMeter(m metrics.Meter) Option {
    return func(o *options) {
        o.meter = m
    }
}

// WithTracer 注入 Tracer
func WithTracer(t trace.Tracer) Option {
    return func(o *options) {
        o.tracer = t
    }
}
```

## 4. 日志与命名空间规范

### 4.1 注入与派生

1. **强制注入:** 组件必须支持通过 `WithLogger` 接收外部 Logger。
2. **默认行为:** 如果未注入 Logger，组件应使用 `No-op Logger` 或 `clog.Default()`，避免空指针 panic，但不建议在生产环境依赖默认值。
3. **Namespace 派生:** 组件在接收到 Logger 后，**必须**立即调用 `WithNamespace` 派生自己的子 Logger，命名规范为：
   * 应用级：`<app>`，如 `user-service`
   * 组件级：`<app>.<component>`，如 `user-service.dlock`
   * 子模块级（可选）：`<app>.<component>.<sub>`, 如 `user-service.dlock.redis`

### 4.2 实现示例

```go
// internal/<component>/impl.go

func New(dep types.Dep, cfg types.Config, opts ...Option) (*Component, error) {
    // 1. 应用选项
    opt := defaultOptions()
    for _, o := range opts {
        o(&opt)
    }

    // 2. 派生 Namespace (关键步骤)
    // 假设传入的 logger namespace 为 "user-service"
    // 派生后变为 "user-service.dlock"
    log := opt.logger // 已在 WithLogger 中追加组件名

    return &Component{
        logger: log,
        dep:    dep,
        cfg:    cfg,
    }, nil
}
```

### 4.3 日志输出

* 使用派生后的 `log` 实例记录日志。
* 日志将自动携带完整的 Namespace 前缀（如 `user-service.dlock`）。

## 5. 配置规范

1. **结构体定义:** 在 `pkg/<component>/types/config.go` 中定义组件的配置结构体，仅包含与该组件相关的字段。
2. **Tag 规范:** 使用 `yaml` 和 `json` tag 支持配置解析，命名与 `config.AppConfig` 中的字段保持一致。
3. **传递方式:** 业务代码不直接构造此结构体，而是由 Config 模块加载配置后绑定到 `AppConfig`，再由 `Container` 从 `AppConfig` 中裁剪出对应子配置并传递给组件。
4. **配置来源:** 组件不感知配置来自文件、环境变量还是远程配置中心（如 Etcd），只依赖传入的 `Config` 结构体。

```go
package types

type Config struct {
    Prefix      string        `yaml:"prefix" json:"prefix"`
    DefaultTTL  time.Duration `yaml:"default_ttl" json:"default_ttl"`
}
```

## 6. 生命周期管理

如果组件有后台任务或需要清理资源，必须实现 `Lifecycle` 接口：

```go
type Lifecycle interface {
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
}
```

* **Start:** 启动后台任务（如定期清理、心跳维持）。
* **Stop:** 优雅关闭，释放资源。

Container 将负责调用这些方法。

## 7. 总结：组件开发 Checklist

* [ ] 目录结构符合 `pkg/` (API) 和 `internal/` (实现) 分离。
* [ ] 提供 `New(Dep, Config, ...Option)` 工厂函数。
* [ ] 实现 `WithLogger` Option，并自动派生 Namespace。
* [ ] 定义独立的 `Config` 结构体。
* [ ] (可选) 实现 `Lifecycle` 接口。
