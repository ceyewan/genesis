# Config 组件设计文档

## 1. 概述

Config 组件为 Genesis 框架提供统一的配置管理能力，支持多源加载（YAML/JSON、环境变量、.env）和热更新。

* **所属层级**：L0 (Base) — 框架基石，在应用启动最早阶段加载
* **核心职责**：统一的配置加载、解析、验证和热更新
* **设计原则**：
  * 零依赖：不依赖任何 Genesis 组件（除 `xerrors`）
  * 接口优先：对外暴露 `Loader` 接口，隐藏具体实现
  * 扁平化 API：所有类型在 `pkg/config` 根包下

## 2. 目录结构

```text
pkg/config/
├── config.go          # 工厂函数 New() + 快捷函数
├── interface.go       # Loader 接口和 Event 类型
├── options.go         # Option 模式定义
├── errors.go          # 错误定义
└── viper.go           # Viper 实现（内部）
```

## 3. 核心接口

### 3.1 Loader 接口

```go
type Loader interface {
    // Load 加载配置（启动阶段调用）
    Load(ctx context.Context) error

    // Get 获取原始配置值
    Get(key string) any

    // Unmarshal 将整个配置反序列化到结构体
    Unmarshal(v any) error

    // UnmarshalKey 将指定 Key 的配置反序列化到结构体
    UnmarshalKey(key string, v any) error

    // Watch 监听配置变化
    Watch(ctx context.Context, key string) (<-chan Event, error)

    // Validate 验证当前配置的有效性
    Validate() error
}
```

### 3.2 Event 事件类型

```go
type Event struct {
    Key       string    // 配置 key
    Value     any       // 新值
    OldValue  any       // 旧值
    Source    string    // "file" | "env" | "remote"
    Timestamp time.Time
}
```

### 3.3 Config 结构体（配置组件自身的配置）

```go
type Config struct {
    Name      string   `mapstructure:"name"`       // 配置文件名称（不含扩展名）
    Paths     []string `mapstructure:"paths"`      // 配置文件搜索路径
    Type      string   `mapstructure:"type"`       // 配置文件类型 (yaml, json)
    EnvPrefix string   `mapstructure:"env_prefix"` // 环境变量前缀
}
```

## 4. 工厂函数

```go
// New 创建 Loader 实例
func New(opts ...Option) (Loader, error)

// Load 快捷函数：创建 Loader 并立即加载配置
func Load(path string, opts ...Option) (Loader, error)

// MustLoad 类似 Load，但出错时 panic（仅用于初始化）
func MustLoad(path string, opts ...Option) Loader
```

## 5. Option 模式

```go
type options struct {
    name      string   // 配置文件名称
    paths     []string // 搜索路径
    fileType  string   // 文件类型
    envPrefix string   // 环境变量前缀
}

type Option func(*options)

func WithConfigName(name string) Option
func WithConfigPaths(paths ...string) Option
func WithConfigType(typ string) Option
func WithEnvPrefix(prefix string) Option
```

## 6. 加载优先级

由高到低：

1. **环境变量** (`GENESIS_*`)
2. **.env 文件**
3. **环境特定配置** (`config.{env}.yaml`)
4. **基础配置** (`config.yaml`)
5. **代码默认值**

## 7. 环境变量规则

- **前缀**: 默认为 `GENESIS`（可通过 `WithEnvPrefix` 修改）
- **分隔符**: 使用下划线 `_` 替代层级点 `.`
- **格式**: `{PREFIX}_{SECTION}_{KEY}`（全大写）

**示例**: YAML `mysql.host` → 环境变量 `GENESIS_MYSQL_HOST`

## 8. 多环境支持

```text
config/
├── config.yaml          # 基础配置
├── config.dev.yaml      # 开发环境覆盖
├── config.prod.yaml     # 生产环境覆盖
└── config.local.yaml    # 本地开发覆盖 (git ignored)
```

加载逻辑：先加载 `config.yaml`，然后根据 `GENESIS_ENV` 环境变量加载对应的 `config.{env}.yaml` 进行合并覆盖。

## 9. 错误处理

使用 `xerrors` 进行错误处理：

```go
var (
    ErrConfigNotFound = xerrors.New("config: file not found")
    ErrInvalidConfig  = xerrors.New("config: invalid format")
    ErrValidation     = xerrors.New("config: validation failed")
)
```

## 10. 与其他组件的集成

### 10.1 典型应用配置结构

```go
// AppConfig 应用配置结构体
type AppConfig struct {
    Log       clog.Config      `mapstructure:"log"`
    Metrics   metrics.Config   `mapstructure:"metrics"`
    
    Redis     connector.RedisConfig `mapstructure:"redis"`
    MySQL     connector.MySQLConfig `mapstructure:"mysql"`
    
    DLock     dlock.Config     `mapstructure:"dlock"`
    Cache     cache.Config     `mapstructure:"cache"`
    RateLimit ratelimit.Config `mapstructure:"ratelimit"`
}
```

### 10.2 配置文件示例

```yaml
# config.yaml
log:
  level: info
  format: json
  output: stdout

metrics:
  enabled: true
  port: 9090
  path: /metrics

redis:
  addr: localhost:6379
  pool_size: 10

mysql:
  dsn: user:pass@tcp(localhost:3306)/db
  max_open_conns: 100

dlock:
  retry_interval: 100ms
  retry_count: 3

cache:
  default_ttl: 5m
```

### 10.3 使用流程

```go
func main() {
    ctx := context.Background()

    // 1. 加载配置（最先）
    loader := config.MustLoad("config.yaml",
        config.WithConfigPaths("./config", "/etc/myapp"),
        config.WithEnvPrefix("MYAPP"),
    )

    var cfg AppConfig
    if err := loader.Unmarshal(&cfg); err != nil {
        log.Fatal(err)
    }

    // 2. 初始化 Logger
    logger := clog.Must(&cfg.Log)

    // 3. 初始化 Metrics
    meter, _ := metrics.New(&cfg.Metrics)

    // 4. 创建 Connectors
    redisConn, _ := connector.NewRedis(&cfg.Redis,
        connector.WithLogger(logger),
        connector.WithMeter(meter),
    )
    defer redisConn.Close()

    // 5. 创建组件
    locker, _ := dlock.NewRedis(redisConn, &cfg.DLock,
        dlock.WithLogger(logger),
    )

    // 6. 业务逻辑...
}
```

### 10.4 配置热更新

```go
// 监听配置变化
ch, _ := loader.Watch(ctx, "ratelimit.qps")
go func() {
    for event := range ch {
        logger.Info("config changed",
            clog.String("key", event.Key),
            clog.Any("value", event.Value),
        )
        // 重新加载组件配置...
    }
}()
```

## 11. 使用示例

### 11.1 基础使用

```go
loader, _ := config.New(
    config.WithConfigName("config"),
    config.WithConfigPaths("./config"),
    config.WithEnvPrefix("GENESIS"),
)
loader.Load(ctx)

var cfg AppConfig
loader.Unmarshal(&cfg)
```

### 11.2 快捷方式

```go
loader := config.MustLoad("./config/config.yaml")

var cfg AppConfig
loader.Unmarshal(&cfg)
```

### 11.3 部分解析

```go
var mysqlCfg connector.MySQLConfig
loader.UnmarshalKey("mysql", &mysqlCfg)
```

## 12. 实现细节

当前使用 `spf13/viper` 作为核心实现：

- **初始化流程**：New → Load → Unmarshal
- **热更新**：利用 Viper 的 `WatchConfig` + `fsnotify`
- **环境变量**：自动绑定 `{PREFIX}_{KEY}` 格式
