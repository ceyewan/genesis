# config - Genesis 统一配置管理组件

[![Go Reference](https://pkg.go.dev/badge/github.com/ceyewan/genesis/config.svg)](https://pkg.go.dev/github.com/ceyewan/genesis/config)

`config` 是 Genesis 框架的统一配置管理组件，提供多源配置加载、热更新和配置验证能力。

## 特性

- **多源配置加载**：支持 YAML/JSON 文件、环境变量、.env 文件
- **配置优先级**：环境变量 > .env > 环境特定配置 > 基础配置
- **热更新支持**：实时监听配置文件变化，自动通知应用
- **接口优先设计**：基于接口的 API，隐藏实现细节
- **函数式选项**：使用标准 Genesis Option 模式
- **多环境支持**：内置 `config.{env}.yaml` 环境特定配置
- **零依赖**：仅依赖 `xerrors`，符合 L0 基础组件定位

## 目录结构（完全扁平化设计）

```text
config/                    # 公开 API + 实现（完全扁平化）
├── config.go             # Config 结构体和基础配置
├── interface.go          # Loader 接口定义
├── options.go            # Option 模式实现（WithConfigName 等）
├── errors.go             # 配置相关错误类型
├── viper.go              # Viper 适配器实现
├── *_test.go            # 测试文件
└── README.md             # 本文档
```

**设计原则**：

- 完全扁平化设计，所有公开 API 和实现都在根目录，无 `types/` 子包
- 接口优先设计，基于 `Loader` 接口提供所有功能
- 使用 Viper 作为底层实现，但不暴露给用户

## 快速开始

```go
import "github.com/ceyewan/genesis/config"
```

### 基础使用

```go
// 一步创建并加载配置
loader := config.MustLoad(
    config.WithConfigName("config"),
    config.WithConfigPaths("./config"),
    config.WithEnvPrefix("GENESIS"),
)

var cfg AppConfig
if err := loader.Unmarshal(&cfg); err != nil {
    panic(err)
}
```

### 分步使用

```go
// 创建配置加载器
loader, err := config.New(
    config.WithConfigName("config"),
    config.WithConfigPaths("./config"),
    config.WithEnvPrefix("GENESIS"),
)
if err != nil {
    panic(err)
}

// 加载配置
if err := loader.Load(ctx); err != nil {
    panic(err)
}

// 解析到结构体
var cfg AppConfig
if err := loader.Unmarshal(&cfg); err != nil {
    panic(err)
}
```

### 配置监听

```go
// 监听特定配置项变化
ch, _ := loader.Watch(ctx, "mysql.host")

go func() {
    for event := range ch {
        fmt.Printf("配置已更新: %s = %v (来源: %s)\n",
            event.Key, event.Value, event.Source)
        // 重新初始化相关组件...
    }
}()
```

## 配置优先级

配置加载按以下优先级（高到低）：

1. **环境变量** (`GENESIS_*`)
2. **.env 文件**
3. **环境特定配置** (`config.{env}.yaml`)
4. **基础配置** (`config.yaml`)
5. **代码默认值**

### 环境变量规则

- **前缀**：默认为 `GENESIS`（可通过 `WithEnvPrefix` 修改）
- **分隔符**：使用下划线 `_` 替代点 `.`
- **格式**：`{PREFIX}_{SECTION}_{KEY}`（全大写）

**示例**：
- YAML: `mysql.host`
- 环境变量: `GENESIS_MYSQL_HOST`

## API 参考

### 核心接口

```go
type Loader interface {
    // Load 加载配置并初始化内部状态
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

### 配置事件

```go
type Event struct {
    Key       string    // 配置 key
    Value     any       // 新值
    OldValue  any       // 旧值
    Source    string    // "file" | "env" | "remote"
    Timestamp time.Time
}
```

### 工厂函数

```go
// New 创建 Loader 实例
func New(opts ...Option) (Loader, error)

// Load 创建 Loader 实例并立即加载配置
func Load(opts ...Option) (Loader, error)

// MustLoad 类似 Load，但出错时 panic（仅用于初始化）
func MustLoad(opts ...Option) Loader
```

### 配置选项

```go
// 设置配置文件名称（不含扩展名）
func WithConfigName(name string) Option

// 添加配置文件搜索路径
func WithConfigPath(path string) Option

// 设置配置文件搜索路径（覆盖默认值）
func WithConfigPaths(paths ...string) Option

// 设置配置文件类型 (yaml, json, etc.)
func WithConfigType(typ string) Option

// 设置环境变量前缀
func WithEnvPrefix(prefix string) Option

// 设置远程配置中心选项
func WithRemote(provider, endpoint string) Option
```

## 多环境支持

支持环境特定的配置文件：

```text
config/
├── config.yaml          # 基础配置
├── config.dev.yaml      # 开发环境覆盖
├── config.prod.yaml     # 生产环境覆盖
└── config.local.yaml    # 本地开发覆盖 (git ignored)
```

**加载逻辑**：
1. 先加载 `config.yaml`
2. 根据 `GENESIS_ENV` 环境变量加载对应的 `config.{env}.yaml` 进行合并覆盖

## 使用示例

运行完整示例：

```bash
cd examples/config
go run main.go
```

### 应用配置结构

```go
type AppConfig struct {
    App struct {
        Name        string `mapstructure:"name"`
        Version     string `mapstructure:"version"`
        Environment string `mapstructure:"environment"`
        Debug       bool   `mapstructure:"debug"`
    } `mapstructure:"app"`

    MySQL struct {
        Host     string `mapstructure:"host"`
        Port     int    `mapstructure:"port"`
        Username string `mapstructure:"username"`
        Password string `mapstructure:"password"`
        Database string `mapstructure:"database"`
    } `mapstructure:"mysql"`

    Redis struct {
        Addr     string `mapstructure:"addr"`
        Password string `mapstructure:"password"`
        DB       int    `mapstructure:"db"`
    } `mapstructure:"redis"`

    Logger struct {
        Level       string `mapstructure:"level"`
        Format      string `mapstructure:"format"`
        Output      string `mapstructure:"output"`
        EnableColor bool   `mapstructure:"enable_color"`
    } `mapstructure:"clog"`
}
```

### 配置文件示例

```yaml
# config.yaml
app:
  name: "Genesis 应用"
  version: "1.0.0"
  debug: false

mysql:
  host: "localhost"
  port: 3306
  username: "root"
  database: "genesis"
  charset: "utf8mb4"

redis:
  addr: "localhost:6379"
  db: 0

clog:
  level: "info"
  format: "json"
  output: "stdout"
  enable_color: false
```

### 环境变量示例

```bash
# 覆盖应用配置
export GENESIS_APP_NAME="生产环境应用"
export GENESIS_APP_DEBUG="true"

# 覆盖数据库配置
export GENESIS_MYSQL_HOST="prod-db.example.com"
export GENESIS_MYSQL_PORT="5432"
```

## 错误处理

```go
import "github.com/ceyewan/genesis/xerrors"

// 检查错误类型
if config.IsNotFound(err) {
    // 配置文件未找到
}

if config.IsInvalidInput(err) {
    // 配置格式无效或验证失败
}

// 包装错误
return config.WrapValidationError(err)
return config.WrapLoadError(err, "配置文件加载失败")
```

## 最佳实践

### 1. 初始化顺序

配置加载应该是最早的步骤：

```go
func main() {
    // 1. 加载配置（最先）
    loader := config.MustLoad(
        config.WithConfigName("config"),
        config.WithEnvPrefix("MYAPP"),
    )

    var cfg AppConfig
    if err := loader.Unmarshal(&cfg); err != nil {
        log.Fatal(err)
    }

    // 2. 初始化 Logger
    logger := clog.Must(&cfg.Logger)

    // 3. 初始化其他组件...
}
```

### 2. 配置结构体设计

- 使用 `mapstructure` 标签指定字段映射
- 嵌套结构体按组件分组
- 为每个组件提供独立的配置结构体

### 3. 配置验证

```go
// 在 Load() 后自动调用 Validate()
type Config struct {
    RequiredField string `mapstructure:"required_field" validate:"required"`
    Port         int    `mapstructure:"port" validate:"min=1,max=65535"`
}
```

### 4. 配置监听

- 仅监听关键配置项
- 使用缓冲通道避免阻塞
- 在上下文取消时自动清理资源

## 文档

- 使用 `go doc -all ./config` 查看完整 API 文档
- 更多示例见 `examples/config/`
- 设计理念详见原设计文档

## License

[MIT License](../../LICENSE)