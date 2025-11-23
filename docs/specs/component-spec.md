# Genesis 组件开发规范 (Component Specification)

本规范定义了 Genesis 框架中所有组件（Component）和连接器（Connector）必须遵循的开发标准。旨在确保框架的一致性、可测试性和可观测性。

## 1. 核心原则

1. **依赖注入 (Dependency Injection):** 组件不得在内部创建依赖（如连接器、Logger、配置加载器），必须通过构造函数或 Option 注入。
2. **配置分离 (Configuration Decoupling):** 组件不得直接读取配置文件或环境变量，必须通过结构体参数接收配置。
3. **双模式支持 (Dual Mode Support):**
    * **容器模式 (Containerized) – 生产环境推荐：** 组件实例由 `pkg/container` 统一构建和管理生命周期，业务代码仅通过 `Container` 获取组件接口。
    * **独立模式 (Standalone)：** 组件提供标准的 `New` 工厂函数，允许在不依赖 Container 的情况下独立实例化（主要用于单元测试和工具脚本），调用方需要自行管理依赖与资源释放。
4. **可观测性优先 (Observability First):** 日志、Metrics 和 Tracing 必须作为一级公民，通过统一接口注入，组件内部只依赖抽象接口，不直接依赖 OTel 等具体实现。

## 2. 目录结构规范

所有组件应遵循以下目录结构：

```text
pkg/<component>/
├── component.go        # 统一入口：工厂函数 (New) 和导出类型
├── options.go          # Option 模式定义 (WithLogger, WithMetrics 等)
└── types/              # 类型定义
    ├── config.go       # 配置结构体
    ├── interface.go    # 核心接口
    └── errors.go       # 错误定义
internal/<component>/
├── impl.go             # 核心实现
└── ...
```

## 3. 初始化规范

### 3.1 工厂函数签名

所有组件必须在 `pkg/<component>/component.go` 中提供一个统一的工厂函数：

```go
// New 创建一个新的组件实例
// dep: 组件依赖的连接器 / 其他组件接口（可以是聚合结构体）
// cfg: 组件配置结构体 (必须，由上层构造传入)
// opts: 可选参数 (Logger, Metrics, Tracer 等)
func New(dep types.Dep, cfg types.Config, opts ...Option) (types.Interface, error)
```

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
