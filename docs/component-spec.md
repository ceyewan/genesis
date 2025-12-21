# Genesis 组件开发规范 (Component Specification)

本规范定义了 Genesis 组件库中所有组件（Component）和连接器（Connector）必须遵循的开发标准。旨在确保组件的一致性、可测试性和易用性。

## 1. 核心原则

1. **显式依赖注入 (Explicit Dependency Injection):** 组件不得在内部隐式创建或从全局容器获取依赖（如连接器、Logger、配置加载器）。所有依赖必须通过构造函数或 Option 显式注入。
2. **Go Native 模式:** 废弃 DI 容器，拥抱 Go 原生的显式初始化方式。
3. **配置分离 (Configuration Decoupling):** 组件不得直接读取配置文件或环境变量，必须通过结构体参数接收配置。
4. **可观测性优先 (Observability First):** 日志、Metrics 和 Tracing 必须作为一级公民，通过 Option 模式注入。组件内部只依赖抽象接口（如 `clog.Logger`, `metrics.Meter`）。

## 2. 目录结构规范

Genesis 采用完全扁平化结构，所有组件直接放在根目录，不使用 `pkg/` 或 `types/` 子包：

建议的目录结构示例：

```text
# 对于所有组件（扁平化）
<component>/
├── <component>.go      # 工厂函数 (New)、导出接口、Sentinel Errors
├── config.go           # Config 结构体定义
├── options.go          # Option 模式定义 (WithLogger, WithMeter 等)
├── <impl>.go           # 非导出实现
├── adapter/            # (可选) 协议适配器（如 Gin 中间件）
└── README.md           # 组件使用文档

# internal/ 目录用于内部实现
internal/<component>/
```

## 3. 初始化规范

### 3.1 工厂函数签名

工厂函数需遵循下列约定：

- **必选参数**：核心物理依赖（如 `Connector`）和必要的业务配置（`Config`）。
- **可选参数**：使用 `Option` 模式注入可观测性组件（Logger, Meter, Tracer）。
- **无阻塞**：`New` 函数不应执行阻塞 I/O。

推荐签名示例：

```go
// New 创建组件实例
func New(conn connector.RedisConnector, cfg *Config, opts ...Option) (Interface, error)
```

### 3.2 Option 模式

必须使用 Option 模式处理可选依赖。

```go
// pkg/<component>/options.go

type options struct {
    logger clog.Logger
    meter  metrics.Meter
}

type Option func(*options)

func WithLogger(l clog.Logger) Option {
    return func(o *options) {
        // 组件内部自动追加 component 标签
        o.logger = l.With("component", "<component_name>")
    }
}
```

## 4. 日志规范

1. **命名空间与标签**: 组件在记录日志时，应确保带有组件标识。推荐在 `WithLogger` 时通过 `logger.With("component", "...")` 绑定。
2. **Context 支持**: 必须使用支持 Context 的日志 API（如 `InfoContext`），以确保 TraceID 等链路信息能够正确透传。

## 5. 配置规范

1. **独立结构体**: 在组件包根目录定义 `Config` 结构体。
2. **Tag 规范**: 使用 `yaml` 和 `json` tag。
3. **零值可用**: 尽可能使配置的零值具有合理的默认行为，或在 `New` 中校验必填项。

## 6. 资源管理 (Lifecycle)

由于移除了 DI 容器的生命周期管理，组件需遵循以下规范：

1. **借用资源不关闭**: 凡是传入的 `Connector` 等共享资源，组件的 `Close()` 方法应为 **no-op**。
2. **独占资源显式释放**: 如果组件内部启动了 Goroutine 或创建了私有连接，必须提供 `Close()` 方法并在 `main.go` 中通过 `defer` 调用。

## 7. 错误处理

1. **哨兵错误**: 在包根目录定义导出的 Sentinel Errors，方便外部通过 `errors.Is` 判断。
2. **错误包装**: 使用 `xerrors.Wrap` 或 `fmt.Errorf("%w")` 包装底层错误，保留上下文。

## 8. 组件开发 Checklist

- [ ] 目录结构扁平化（L2/L3）。
- [ ] 提供 `New(Conn, Config, ...Option)` 工厂函数。
- [ ] 实现 `WithLogger`, `WithMeter` 等 Option。
- [ ] Sentinel Errors 定义在包根目录。
- [ ] `Close()` 方法符合资源所有权原则。
