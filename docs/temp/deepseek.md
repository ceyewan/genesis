## 核心问题分析

### 1. 初始化方式不一致

- **cache-design**: 直接通过 `New(conn, cfg)` 初始化
- **dlock-design**: 通过 container 注入 logger
- **其他组件**: 混合模式，没有统一标准

### 2. 日志注入不统一

有些组件设计提到了日志注入，有些完全没有考虑，这会导致日志追踪链路断裂。

### 3. 配置管理不清晰

用户应该如何处理配置？是直接读取还是通过配置组件？

## 建议的统一规范

### 1. 统一初始化方式：**双模式支持**

```go
// 模式1：通过 Container 初始化（推荐）
app, err := container.New(cfg)
dlock := app.DLock

// 模式2：独立初始化（用于测试或特殊场景）
logger := clog.New(cfg, clog.WithNamespace("my-service"))
redisConn := connector.NewRedisConnector(redisCfg, logger.WithNamespace("redis"))
dlock := dlock.New(redisConn, dlockCfg, logger.WithNamespace("dlock"))
```

### 2. 统一组件接口设计

每个组件应该遵循相同的模式：

```go
// pkg/component/types/interface.go
package types

type Component interface {
    // 核心业务方法
    DoSomething(ctx context.Context, param string) error
    
    // 可选：生命周期管理
    Close() error
}

// pkg/component/component.go
package component

// 工厂函数签名统一
func New(
    connector dependency.Connector,  // 依赖的连接器
    cfg *config.Config,             // 组件配置
    logger clog.Logger,             // 注入的日志实例
    opts ...Option,                 // 可选参数
) (types.Component, error)
```

### 3. 统一配置管理架构

```text
业务应用
    ↓
配置组件 (pkg/config)
    ↓ 提供统一配置访问
核心容器 (pkg/container) 
    ↓ 解析配置并初始化
各个组件 (pkg/dlock, pkg/db, ...)
```

### 4. 统一日志注入规范

```go
// 在 container 中统一处理日志命名空间
func (c *Container) initDLock(cfg *Config) error {
    dlockLogger := c.baseLogger.WithNamespace("dlock")
    redisConn := c.getRedisConnector(cfg.Redis)
    
    dlock, err := dlock.New(redisConn, cfg.DLock, dlockLogger)
    if err != nil {
        return err
    }
    
    c.DLock = dlock
    c.lifecycles = append(c.lifecycles, dlock)
    return nil
}
```

## 具体改进建议

### 1. 在 `genesis-design.md` 中明确规范

```markdown
## 8. 组件开发规范

### 8.1. 初始化模式
所有组件必须支持两种初始化方式：
1. **容器初始化**（生产环境推荐）
2. **独立初始化**（测试/特殊场景）

### 8.2. 依赖注入
组件必须通过构造函数接收：
- 连接器依赖
- 配置对象  
- 日志实例
- 可选的 Options

### 8.3. 日志规范
- 使用注入的 Logger，不要创建全局 Logger
- 通过 `WithNamespace()` 派生组件专用 Logger
- 日志格式统一使用结构化字段

### 8.4. 配置管理
业务代码通过 config 组件获取配置，容器负责配置解析和组件初始化。
```

### 2. 统一组件代码结构

```text
pkg/component/
├── component.go              # 工厂函数和类型导出
├── types/
│   ├── interface.go          # 组件接口
│   ├── config.go            # 配置结构体
│   └── options.go           # 运行时选项
└── adapter/                 # 适配器（如中间件）
    ├── gin.go
    └── grpc.go

internal/component/
├── impl.go                  # 核心实现
├── lifecycle.go            # 生命周期管理（如需要）
└── utils.go               # 内部工具函数
```

### 3. 配置设计改进

```go
// pkg/config/types/config.go
package types

type AppConfig struct {
    Log       *clog.Config      `yaml:"log"`
    DB        *db.Config        `yaml:"db"`
    DLock     *dlock.Config     `yaml:"dlock"`
    Cache     *cache.Config     `yaml:"cache"`
    IDGen     *idgen.Config     `yaml:"idgen"`
    // ... 其他组件配置
}

// 业务使用方式
func main() {
    // 方式1：通过配置组件（推荐）
    cfg, err := config.Load("config.yaml")
    if err != nil {
        log.Fatal(err)
    }
    
    app, err := container.New(cfg)
    if err != nil {
        log.Fatal(err)
    }
    
    // 方式2：直接配置（简单场景）
    app, err := container.New(&config.AppConfig{
        Log: &clog.Config{Level: "info", Format: "json"},
        DB: &db.Config{EnableSharding: true},
        // ...
    })
}
```

## 总结建议

1. **统一初始化模式**：支持容器和独立两种方式，但推荐容器方式
2. **强制日志注入**：所有组件必须接收 Logger 参数
3. **明确配置流程**：业务代码 → config组件 → container → 各个组件
4. **标准化接口**：所有组件遵循相同的工厂函数签名
5. **完善文档**：在 genesis-design 中明确这些规范

这样的设计既保证了大型项目的统一性，又为简单场景提供了灵活性，同时为未来的可观测性（Metrics、Tracing）打下了良好基础。
