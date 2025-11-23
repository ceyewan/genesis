# Genesis 架构总结

## 1. 核心设计原则

### 1.1 分层架构

```
┌─────────────────────────────────────────┐
│         Application Layer               │  业务代码
├─────────────────────────────────────────┤
│         Container (集成层)               │  依赖注入 & 生命周期管理
├─────────────────────────────────────────┤
│    Components (业务组件层)              │  DB, DLock, Cache, MQ, IDGen
├─────────────────────────────────────────┤
│    Connectors (连接器层)                │  MySQL, Redis, Etcd, NATS
├─────────────────────────────────────────┤
│    Telemetry & Logger (横切关注点)      │  可观测性
└─────────────────────────────────────────┘
```

### 1.2 依赖方向

- **单向依赖**: 上层依赖下层，下层不依赖上层
- **接口驱动**: `pkg/` 定义接口，`internal/` 提供实现
- **避免循环**: 通过接口抽象和依赖注入避免循环依赖

## 2. 组件职责

### 2.1 Config (配置中心)

**职责**: Bootstrapping 组件，在 Container 之外初始化

**特点**:
- 不依赖 Container
- 定义独立的 `Lifecycle` 接口
- 可选地由 Container 托管生命周期

**使用流程**:
```go
// 1. 创建并加载配置
cfgMgr, _ := config.New(...)
_ = cfgMgr.Load(ctx)
var appCfg AppConfig
_ = cfgMgr.Unmarshal(&appCfg)

// 2. 使用配置创建其他组件
logger, _ := clog.New(appCfg.Log, ...)
app, _ := container.New(&containerCfg, container.WithLogger(logger))

// 3. (可选) 注册到 Container 托管生命周期
app.RegisterConfigManager(cfgMgr)
```

### 2.2 Logger (日志)

**职责**: 提供结构化日志能力

**特点**:
- 应用级 Logger 在 Container 之外创建
- 通过 `WithLogger` Option 注入到 Container
- Container 内部派生子 Logger 给各个组件

**使用流程**:
```go
// 创建应用级 Logger
logger, _ := clog.New(logConfig, &clog.Option{
    NamespaceParts: []string{"my-service"},
})

// 注入到 Container
app, _ := container.New(cfg, container.WithLogger(logger))
```

### 2.3 Telemetry (可观测性)

**职责**: 提供 Metrics 和 Tracing 能力

**特点**:
- 作为 Phase 0 组件，最先启动
- 支持通过 Option 注入或配置自动初始化
- 为其他组件提供 Meter 和 Tracer

**使用流程**:
```go
// 方式 1: 配置驱动
cfg := &container.Config{
    Telemetry: &telemetry.Config{...},
}
app, _ := container.New(cfg)

// 方式 2: Option 注入
app, _ := container.New(cfg,
    container.WithTracer(tracer),
    container.WithMeter(meter),
)
```

### 2.4 Container (容器)

**职责**: 依赖注入和生命周期管理

**管理范围**:
- ✅ Connectors: MySQL, Redis, Etcd, NATS
- ✅ Components: DB, DLock, Cache, MQ, IDGen
- ✅ Telemetry: Metrics & Tracing
- ❌ Config: 在 Container 之外初始化
- ❌ Logger: 在 Container 之外创建并注入

**生命周期阶段**:
- Phase -10: ConfigManager (如果注册)
- Phase 0: Telemetry
- Phase 10: Connectors
- Phase 20: Components
- Phase 30: Services

## 3. 初始化流程

### 3.1 标准流程

```go
func main() {
    ctx := context.Background()

    // 1. Bootstrap: 加载配置
    cfgMgr, _ := config.New(...)
    _ = cfgMgr.Load(ctx)
    var appCfg AppConfig
    _ = cfgMgr.Unmarshal(&appCfg)

    // 2. 创建应用级 Logger
    logger, _ := clog.New(appCfg.Log, &clog.Option{
        NamespaceParts: []string{appCfg.App.Name},
    })

    // 3. 创建 Container
    containerCfg := &container.Config{
        Redis: &connector.RedisConfig{...},
        DLock: &dlock.Config{...},
    }
    app, _ := container.New(containerCfg, container.WithLogger(logger))

    // 4. (可选) 注册 ConfigManager
    app.RegisterConfigManager(cfgMgr)

    // 5. 启动 Container
    _ = app.Start(ctx)
    defer app.Close()

    // 6. 使用组件
    app.DLock.Lock(ctx, "my-lock")
}
```

## 4. 设计亮点

### 4.1 避免循环依赖

- Config 定义独立的 `Lifecycle` 接口，不依赖 Container
- Container 通过接口适配，可以托管任何实现了 `Lifecycle` 的组件

### 4.2 灵活的依赖注入

- Option 模式支持外部注入
- 配置驱动支持自动初始化
- 两种方式可以混合使用

### 4.3 统一的生命周期管理

- 所有组件通过 `Lifecycle` 接口统一管理
- Phase 机制确保正确的启动顺序
- 自动按逆序关闭

### 4.4 清晰的职责边界

- Config: Bootstrapping
- Logger: 横切关注点
- Telemetry: 可观测性
- Container: 集成和生命周期管理
- Components: 业务能力

## 5. 参考文档

- [Genesis 宏观设计](./genesis-design.md)
- [Container 设计文档](./container-design.md)
- [Config 设计文档](./config-design.md)
- [组件开发规范](./specs/component-spec.md)
- [Container 重构总结](./reviews/container-refactoring-summary.md)

