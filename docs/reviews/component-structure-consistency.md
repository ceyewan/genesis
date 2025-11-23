# 组件结构一致性总结

## 1. 统一的文件结构

所有组件（Cache, MQ, DB）现在都遵循相同的文件组织结构：

```
pkg/{component}/
├── {component}.go    # 工厂函数 + 类型导出
├── options.go        # Option 模式定义
└── types/
    ├── config.go     # 配置结构体
    └── interface.go  # 核心接口
```

### 1.1 Cache 组件

```
pkg/cache/
├── cache.go          # 工厂函数 New() + 类型别名 (Cache, Config)
├── options.go        # WithLogger, WithMeter, WithTracer
└── types/
    ├── config.go     # Config 结构体
    └── interface.go  # Cache 接口
```

### 1.2 MQ 组件

```
pkg/mq/
├── mq.go             # 工厂函数 New() + 类型别名 (Client, Message, Config, etc.)
├── options.go        # WithLogger, WithMeter, WithTracer
└── types/
    ├── config.go     # Config, JetStreamConfig, DriverType
    └── mq.go         # Client, Message, Subscription, Handler 接口
```

### 1.3 DB 组件

```
pkg/db/
├── db.go             # 工厂函数 New() + 类型别名 (DB, Config, ShardingRule)
├── options.go        # WithLogger, WithMeter, WithTracer
└── types/
    ├── config.go     # Config, ShardingRule 结构体
    └── interface.go  # DB 接口
```

## 2. 统一的代码模式

### 2.1 主文件结构 ({component}.go)

所有组件的主文件都遵循相同的模式：

```go
package {component}

import (
    internal{component} "github.com/ceyewan/genesis/internal/{component}"
    "github.com/ceyewan/genesis/pkg/clog"
    "github.com/ceyewan/genesis/pkg/connector"
    "github.com/ceyewan/genesis/pkg/{component}/types"
)

// 导出 types 包中的定义，方便用户使用

type {Interface} = types.{Interface}
type Config = types.Config
// ... 其他类型别名

// New 创建组件实例 (独立模式)
func New(conn connector.{Connector}, cfg *Config, opts ...Option) ({Interface}, error) {
    // 应用选项
    opt := Options{
        Logger: clog.Default(),
    }
    for _, o := range opts {
        o(&opt)
    }

    return internal{component}.New(conn, cfg, opt.Logger, opt.Meter, opt.Tracer)
}
```

### 2.2 Options 文件结构 (options.go)

所有组件的 options.go 都完全相同：

```go
package {component}

import (
    "github.com/ceyewan/genesis/pkg/clog"
    "github.com/ceyewan/genesis/pkg/telemetry/types"
)

// Option 组件选项函数
type Option func(*Options)

// Options 选项结构（导出供 internal 使用）
type Options struct {
    Logger clog.Logger
    Meter  types.Meter
    Tracer types.Tracer
}

// WithLogger 注入日志记录器
func WithLogger(l clog.Logger) Option {
    return func(o *Options) {
        if l != nil {
            o.Logger = l.WithNamespace("{component}")
        }
    }
}

// WithMeter 注入指标 Meter
func WithMeter(m types.Meter) Option {
    return func(o *Options) {
        o.Meter = m
    }
}

// WithTracer 注入 Tracer
func WithTracer(t types.Tracer) Option {
    return func(o *Options) {
        o.Tracer = t
    }
}
```

### 2.3 Internal 实现签名

所有组件的 internal 实现都接受相同的参数：

```go
// internal/{component}/{component}.go
func New(
    conn connector.{Connector},
    cfg *types.Config,
    logger clog.Logger,
    meter telemetrytypes.Meter,
    tracer telemetrytypes.Tracer,
) (types.{Interface}, error)
```

## 3. 设计原则

### 3.1 避免循环依赖

- ✅ `pkg/{component}` 导入 `internal/{component}` (单向)
- ✅ `internal/{component}` 导入 `pkg/{component}/types` (单向)
- ✅ `pkg/{component}` 不导入 `pkg/container`
- ✅ `pkg/container` 导入 `pkg/{component}`

### 3.2 类型导出策略

- ✅ 核心类型定义在 `pkg/{component}/types/`
- ✅ 在 `pkg/{component}/{component}.go` 中使用类型别名导出
- ✅ 用户只需导入 `pkg/{component}` 即可使用所有类型

### 3.3 Option 模式

- ✅ Options 结构体导出，供 internal 包使用
- ✅ Option 应用逻辑在 `pkg/{component}/{component}.go` 中
- ✅ 支持默认值（clog.Default()）

### 3.4 Namespace 派生

- ✅ Logger 自动追加组件 Namespace
- ✅ Cache: `logger.WithNamespace("cache")`
- ✅ MQ: `logger.WithNamespace("mq")`
- ✅ DB: `logger.WithNamespace("db")`

## 4. 用户体验

### 4.1 独立模式使用

所有组件都支持相同的独立模式使用方式：

```go
// Cache
redisConn, _ := connector.NewRedis(redisConfig)
cache, _ := cache.New(redisConn, &cache.Config{...}, cache.WithLogger(logger))

// MQ
natsConn, _ := connector.NewNATS(natsConfig)
mq, _ := mq.New(natsConn, &mq.Config{...}, mq.WithLogger(logger))

// DB
mysqlConn, _ := connector.NewMySQL(mysqlConfig)
db, _ := db.New(mysqlConn, &db.Config{...}, db.WithLogger(logger))
```

### 4.2 容器模式使用

所有组件都通过 Container 统一管理：

```go
app, _ := container.New(&container.Config{
    Redis: redisConfig,
    Cache: &cache.Config{...},
    
    NATS: natsConfig,
    MQ: &mq.Config{...},
    
    MySQL: mysqlConfig,
    DB: &db.Config{...},
}, container.WithLogger(logger))

// 直接使用
app.Cache.Set(ctx, "key", value)
app.MQ.Publish(ctx, "topic", data)
app.DB.DB(ctx).Create(&record)
```

## 5. 一致性检查清单

| 检查项 | Cache | MQ | DB |
|--------|-------|----|----|
| 文件结构一致 | ✅ | ✅ | ✅ |
| Option 模式 | ✅ | ✅ | ✅ |
| 类型导出方式 | ✅ | ✅ | ✅ |
| New() 签名 | ✅ | ✅ | ✅ |
| Internal 签名 | ✅ | ✅ | ✅ |
| Namespace 派生 | ✅ | ✅ | ✅ |
| 避免循环依赖 | ✅ | ✅ | ✅ |
| 双模式支持 | ✅ | ✅ | ✅ |

## 6. 总结

通过统一的文件结构和代码模式，Genesis 项目的组件现在具有：

1. **高度一致性**: 所有组件遵循相同的设计模式
2. **易于理解**: 开发者学习一个组件即可理解所有组件
3. **易于维护**: 统一的结构降低维护成本
4. **易于扩展**: 新增组件可以直接复制模式

这种一致性是 Genesis 作为微服务基座项目的核心竞争力之一。

