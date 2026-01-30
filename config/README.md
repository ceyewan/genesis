# config - Genesis 配置管理组件

[![Go Reference](https://pkg.go.dev/badge/github.com/ceyewan/genesis/config.svg)](https://pkg.go.dev/github.com/ceyewan/genesis/config)

基于 Viper 的多源配置加载组件。

## 特性

- **多源配置**：YAML/JSON 文件、环境变量、.env 文件
- **配置优先级**：环境变量 > .env > 环境特定配置 > 基础配置
- **热更新**：文件变化自动重载（支持基础配置和环境特定配置）
- **多环境**：支持 `config.{env}.yaml`

## 快速开始

```go
loader, _ := config.New(&config.Config{
    Name:     "config",
    Paths:    []string{"./config"},
    FileType: "yaml",
})

err := loader.Load(ctx)
var cfg AppConfig
loader.Unmarshal(&cfg)
```

## 配置优先级

从高到低：

1. **环境变量** (`GENESIS_MYSQL_HOST`)
2. **.env 文件**
3. **环境特定配置** (`config.prod.yaml` 合并到 `config.yaml`)
4. **基础配置** (`config.yaml`)

> **注意**：`.env` 文件会覆盖同名环境变量（godotenv 默认行为），但 Viper 的 `AutomaticEnv` 会在 `Get()` 时优先读取运行时环境变量，因此最终效果仍是"运行时环境变量优先"。

## API

```go
type Config struct {
    Name      string   // 配置文件名（不含扩展名）
    Paths     []string // 搜索路径
    FileType  string   // yaml|json
    EnvPrefix string   // 环境变量前缀，默认 GENESIS
}

type Loader interface {
    Load(ctx) error
    Get(key) any
    Unmarshal(v) error
    UnmarshalKey(key, v) error
    Watch(ctx, key) (<-chan Event, error)
    Validate() error
}

type Event struct {
    Key       string
    Value     any
    OldValue  any
    Source    string  // "file" | "env"
    Timestamp time.Time
}

var (
    ErrNotFound         = xerrors.New("config file not found")
    ErrValidationFailed = xerrors.New("configuration validation failed")
)

func New(cfg *Config) (Loader, error)
```

## 热更新 (Watch)

监听配置变化，当文件修改时自动通知：

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

ch, _ := loader.Watch(ctx, "mysql.host")
for event := range ch {
    fmt.Printf("配置变化: %s = %v\n", event.Key, event.Value)
}
```

**实现细节：**

- 无论调用多少次 `Watch`，内部只启动一个文件监听 goroutine
- 返回的 channel 缓冲区大小为 10，若消费者处理过慢可能丢失事件
- 监听基础配置文件和环境特定配置文件（如 `config.yaml` 和 `config.dev.yaml`）
- `.env` 文件变更不会触发通知
- 热更新时若配置文件读取失败，不会推送变更事件（静默忽略以避免中断服务）

## 环境特定配置

```text
config/
├── config.yaml          # 基础配置
├── config.dev.yaml      # 开发环境（合并）
└── config.prod.yaml     # 生产环境（合并）
```

通过 `GENESIS_ENV` 环境变量指定环境。

## 环境变量映射

| YAML 键    | 环境变量           |
| ---------- | ------------------ |
| mysql.host | GENESIS_MYSQL_HOST |
| redis.addr | GENESIS_REDIS_ADDR |
| app.debug  | GENESIS_APP_DEBUG  |

规则：`{PREFIX}_{SECTION}_{KEY}`（全大写，`.` 替换为 `_`）

## 错误处理

```go
loader, err := config.New(&config.Config{...})
if errors.Is(err, config.ErrNotFound) {
    // 配置文件未找到
}
```

## 示例配置

```yaml
# config.yaml
app:
    name: "Genesis 应用"
    debug: false

mysql:
    host: "localhost"
    port: 3306

redis:
    addr: "localhost:6379"
```
