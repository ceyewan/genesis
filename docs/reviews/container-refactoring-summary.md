# Container 重构总结

**日期:** 2025-11-23  
**基于审计报告:** `docs/reviews/container-implementation-review.md`

## 1. 重构目标

根据审计报告中的建议，本次重构主要解决以下问题:

1. **引入 Option 模式**: 允许外部注入 Logger、Tracer、Meter 等依赖
2. **集成 Telemetry**: 将可观测性能力作为一级公民集成到 Container
3. **优化配置结构**: 移除 Log 配置，增加 Telemetry 配置
4. **完善生命周期管理**: 确保 Telemetry 作为 Phase 0 组件被正确管理
5. **为未来扩展做准备**: 为 Cache 组件和组件的 Telemetry 集成预留接口

## 2. 主要变更

### 2.1 New 函数签名变更

**之前:**
```go
func New(cfg *Config) (*Container, error)
```

**现在:**
```go
func New(cfg *Config, opts ...Option) (*Container, error)
```

### 2.2 新增 Option 函数

```go
// WithLogger 注入外部 Logger
func WithLogger(logger clog.Logger) Option

// WithTracer 注入外部 Tracer
func WithTracer(tracer types.Tracer) Option

// WithMeter 注入外部 Meter
func WithMeter(meter types.Meter) Option
```

### 2.3 Config 结构体变更

**移除:**
```go
Log *clog.Config  // 改为通过 Option 注入
```

**新增:**
```go
Telemetry *telemetry.Config  // 支持自动初始化 Telemetry
```

### 2.4 Container 结构体新增字段

```go
// Telemetry 组件
Telemetry telemetry.Telemetry
// Meter 指标接口
Meter types.Meter
// Tracer 链路追踪接口
Tracer types.Tracer
```

## 3. 使用方式

### 3.1 基础用法 (使用默认 Logger)

```go
cfg := &container.Config{
    Redis: &connector.RedisConfig{...},
    DLock: &dlock.Config{...},
}

app, err := container.New(cfg)
```

### 3.2 使用 Option 注入自定义 Logger

```go
// 创建应用级 Logger
appLogger, _ := clog.New(logConfig, &clog.Option{
    NamespaceParts: []string{"my-service"},
})

// 使用 Option 注入
app, _ := container.New(cfg, container.WithLogger(appLogger))
```

### 3.3 使用配置自动初始化 Telemetry

```go
cfg := &container.Config{
    Telemetry: &telemetry.Config{
        ServiceName:          "order-service",
        ExporterType:         "stdout",
        PrometheusListenAddr: ":9091",
    },
    // ... 其他配置
}

app, _ := container.New(cfg)
// app.Telemetry, app.Meter, app.Tracer 会自动初始化
```

### 3.4 混合使用

```go
// 外部创建 Logger 和 Telemetry
app, _ := container.New(cfg,
    container.WithLogger(appLogger),
    container.WithTracer(tracer),
    container.WithMeter(meter),
)
```

## 4. 向后兼容性

### 4.1 破坏性变更

1. **Config.Log 字段移除**
   - 影响: 在 Config 中配置 Log 的代码需要更新
   - 迁移: 改为使用 `WithLogger` Option 注入

### 4.2 兼容性保持

1. **New 函数调用**: `container.New(cfg)` 仍然有效 (opts 为可选参数)
2. **默认行为**: 未注入 Logger 时，Container 会自动创建默认 Logger

## 5. 示例更新

- ✅ `examples/container/main.go`: 新增示例，演示基础用法和 Option 模式
- ✅ `examples/config-with-container/main.go`: 新增示例，演示 Config + Container 集成
- ✅ `examples/dlock-etcd/main.go`: 更新为使用 WithLogger Option
- ✅ `examples/dlock-redis/main.go`: 更新为使用 WithLogger Option
- ✅ `examples/cache/main.go`: 更新为使用 WithLogger Option
- ✅ `examples/idgen/main.go`: 移除 Config.Log 字段
- ✅ `examples/connector/main.go`: 保持兼容
- ✅ `examples/db/main.go`: 保持兼容
- ✅ `examples/mq/main.go`: 保持兼容

## 6. Config Manager 集成

### 6.1 循环依赖问题解决

**问题**: `pkg/config/types/interface.go` 原本导入了 `pkg/container`,造成循环依赖。

**解决方案**: 在 `pkg/config/types` 中定义独立的 `Lifecycle` 接口:

```go
type Lifecycle interface {
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    Phase() int
}
```

此接口与 `container.Lifecycle` 方法签名完全相同，可以无缝适配。

### 6.2 Container 新增方法

**新增 Start 方法**:
```go
func (c *Container) Start(ctx context.Context) error
```

**新增 RegisterConfigManager 方法**:
```go
func (c *Container) RegisterConfigManager(mgr interface {
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    Phase() int
})
```

### 6.3 正确的使用流程

```go
// 1. Bootstrap: 创建并加载配置 (Container 之外)
cfgMgr, _ := config.New(...)
_ = cfgMgr.Load(ctx)
var appCfg AppConfig
_ = cfgMgr.Unmarshal(&appCfg)

// 2. 创建应用级 Logger (Container 之外)
logger, _ := clog.New(appCfg.Log, ...)

// 3. 创建 Container (注入 Logger)
app, _ := container.New(&containerCfg, container.WithLogger(logger))

// 4. (可选) 注册 ConfigManager 到 Container
app.RegisterConfigManager(cfgMgr)

// 5. 启动 Container (会自动启动 ConfigManager 的 Watch)
_ = app.Start(ctx)

// 6. 关闭 Container (会自动停止 ConfigManager)
defer app.Close()
```

## 7. 未来工作

1. **组件 Option 支持**: 为 DB、DLock、MQ 等组件添加 WithTracer/WithMeter Option
2. **Cache 组件实现**: 实现 Cache 组件并集成到 Container
3. **文档更新**: 更新用户文档和 API 文档

## 7. 设计原则遵循

本次重构严格遵循以下设计原则:

1. ✅ **依赖注入优先**: 通过 Option 模式支持外部注入
2. ✅ **可观测性优先**: Telemetry 作为一级公民集成
3. ✅ **灵活性与便利性平衡**: 既支持 Option 模式，也支持配置驱动
4. ✅ **生命周期管理**: 统一管理所有组件的启动和关闭
5. ✅ **接口驱动**: 依赖抽象接口而非具体实现

## 8. 参考文档

- [Container 设计文档](../container-design.md)
- [组件开发规范](../specs/component-spec.md)
- [Container 实现审计报告](./container-implementation-review.md)

