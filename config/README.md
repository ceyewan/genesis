# config

[![Go Reference](https://pkg.go.dev/badge/github.com/ceyewan/genesis/config.svg)](https://pkg.go.dev/github.com/ceyewan/genesis/config)

`config` 是 Genesis 的 L0 配置组件，基于 Viper 提供统一的多源配置加载与文件驱动的变更通知能力。它面向微服务和组件库场景，解决配置文件、环境特定配置、进程环境变量和 `.env` 文件之间的统一加载问题。

## 组件定位

`config` 的职责很明确：

- 统一加载基础配置文件与环境特定配置文件
- 统一处理环境变量与 `.env` 的覆盖顺序
- 提供 `Get`、`Unmarshal`、`UnmarshalKey` 等读取能力
- 提供按 key 订阅的文件变更通知

它当前不负责以下能力：

- 远程配置中心
- 运行时环境变量热更新
- `.env` 文件监听
- 复杂 schema 校验框架

## 配置优先级

当前优先级从高到低为：

1. 进程环境变量
2. `.env` 文件
3. 环境特定配置文件，例如 `config.dev.yaml`
4. 基础配置文件，例如 `config.yaml`

这里有一个重要约定：`.env` 的语义是“补齐缺失项”，不会覆盖当前进程里已经存在的同名环境变量。这比让 `.env` 反向覆盖部署时显式传入的环境变量更常见，也更容易解释最终行为。

## 快速开始

```go
loader, err := config.New(&config.Config{
    Name:      "config",
    Paths:     []string{"./config"},
    FileType:  "yaml",
    EnvPrefix: "GENESIS",
})
if err != nil {
    return err
}

if err := loader.Load(ctx); err != nil {
    return err
}

var cfg AppConfig
if err := loader.Unmarshal(&cfg); err != nil {
    return err
}
```

## 热更新

`Load` 只负责加载配置，不会自动启动文件监听。第一次调用 `Watch` 时，组件才会启动内部 watcher，因此推荐的调用顺序是先 `Load`，再 `Watch`：

```go
if err := loader.Load(ctx); err != nil {
    return err
}

ch, err := loader.Watch(ctx, "mysql.host")
if err != nil {
    return err
}

for event := range ch {
    fmt.Printf("%s: %v -> %v\n", event.Key, event.OldValue, event.Value)
}
```

热更新当前有明确边界：

- 只监听基础配置文件和环境特定配置文件
- 事件来源固定为 `file`
- 不监听 `.env` 文件
- 不监听运行时环境变量变化
- 若重载时配置读取、合并或校验失败，不推送变更事件

如果你希望在热更新失败时看到明确告警，可以通过 `WithLogger` 注入日志器：

```go
logger, _ := clog.New(clog.NewProdDefaultConfig("genesis"))

loader, err := config.New(&config.Config{
    Name:      "config",
    Paths:     []string{"./config"},
    FileType:  "yaml",
    EnvPrefix: "GENESIS",
}, config.WithLogger(logger))
```

## 环境特定配置

```text
config/
├── config.yaml
├── config.dev.yaml
└── config.prod.yaml
```

通过 `${PREFIX}_ENV` 选择环境，例如 `GENESIS_ENV=dev` 会在基础配置之上合并 `config.dev.yaml`。环境配置是“增量覆盖”，不是完全替换，因此基础配置里的默认值仍然有效。

## 环境变量映射

| 配置 key | 环境变量 |
| --- | --- |
| `mysql.host` | `GENESIS_MYSQL_HOST` |
| `redis.addr` | `GENESIS_REDIS_ADDR` |
| `app.debug` | `GENESIS_APP_DEBUG` |

规则是：将 key 中的 `.` 和 `-` 替换为 `_`，转成大写，再加上前缀。

## 推荐用法

- 应用启动时先 `Load`，确认配置可用后再构造其他组件
- 业务配置优先使用 `Unmarshal` 或 `UnmarshalKey` 映射到结构体，而不是到处手写 `Get`
- 只监听真正需要热更新的 key，不要把 `Watch` 当成全量配置广播
- 在容器或生产环境里，优先使用显式环境变量覆盖配置文件
- 需要排查热更新失败时，为 loader 注入 `WithLogger`

## 相关文档

- [包文档](https://pkg.go.dev/github.com/ceyewan/genesis/config)
- [组件设计博客](../docs/genesis-config-blog.md)
- [Genesis 文档目录](../docs/README.md)
