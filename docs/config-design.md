# Config 组件设计文档

## 1. 概述

Config 组件为 Genesis 框架提供统一的配置管理能力，支持多源加载（YAML/JSON、环境变量、.env）和热更新。

**定位**: Glue Layer - 在应用启动前进行配置引导，独立于容器构造过程。

## 2. 核心目标

1. **统一管理**: 集中管理所有组件（DB, Redis, Log, MQ 等）的配置
2. **多源支持**: 支持从本地文件、环境变量、.env 以及未来的远程配置中心加载
3. **热更新**: 支持配置的动态监听与实时更新，无需重启服务
4. **类型安全**: 提供强类型的配置解析与绑定能力
5. **接口抽象**: 屏蔽底层实现（Viper/Etcd），便于未来切换

## 3. 核心设计原则

1. **扁平化 API**: 所有类型、接口和错误都在 `pkg/config` 根包下，无冗余子包
2. **零依赖**: 不依赖任何 Genesis 组件（除 `xerrors`），避免循环依赖
3. **标准化错误**: 所有错误使用 `xerrors` Sentinel Errors 或 `xerrors.Wrap` 进行包装
4. **接口优先**: 对外暴露 `Loader` 接口，隐藏具体实现
5. **生命周期管理**: 实现 `container.Lifecycle` 接口，支持由容器管理 Watch 后台任务

## 4. 核心接口

### 4.1 Loader 接口

```go
type Loader interface {
    // Load 加载配置（Bootstrap 阶段调用，在 Start 之前）
    Load(ctx context.Context) error

    // Get 获取原始配置值
    Get(key string) any

    // Unmarshal 将整个配置反序列化到结构体
    Unmarshal(v any) error

    // UnmarshalKey 将指定 Key 的配置反序列化到结构体
    UnmarshalKey(key string, v any) error

    // Watch 监听配置变化，通过 context 取消监听
    Watch(ctx context.Context, key string) (<-chan Event, error)

    // Validate 验证当前配置的有效性
    Validate() error

    // Start 启动后台任务（如文件监听）
    Start(ctx context.Context) error

    // Stop 停止后台任务
    Stop(ctx context.Context) error

    // Phase 返回生命周期阶段（用于容器排序启动顺序）
    Phase() int
}
```

### 4.2 Event 事件类型

```go
type Event struct {
    Key       string    // 配置 key
    Value     any       // 新值
    OldValue  any       // 旧值
    Source    string    // "file" | "env" | "remote"
    Timestamp time.Time
}
```

### 4.3 选项模式

```go
type Option func(*Options)

type Options struct {
    Name       string        // 配置文件名称（不含扩展名）
    Paths      []string      // 配置文件搜索路径
    FileType   string        // 配置文件类型 (yaml, json)
    EnvPrefix  string        // 环境变量前缀
    RemoteOpts *RemoteOptions
}
```

主要 Option 函数：

- `WithConfigName(name)` - 设置配置文件名称
- `WithConfigPaths(paths...)` - 设置搜索路径
- `WithConfigType(typ)` - 设置文件类型
- `WithEnvPrefix(prefix)` - 设置环境变量前缀

### 4.4 错误处理

所有错误使用 `xerrors` 处理：

- `xerrors.ErrInvalidInput` - 配置格式无效或验证失败
- `xerrors.ErrNotFound` - 配置文件不存在
- 其他错误通过 `xerrors.Wrap` 包装

## 5. 加载优先级（由高到低）

1. **环境变量** (GENESIS_*)
2. **.env 文件**
3. **环境特定配置** (config.{env}.yaml)
4. **基础配置** (config.yaml)
5. **代码默认值**Í

## 6. 环境变量规则

- **前缀**: 默认为 `GENESIS`（可通过 `WithEnvPrefix` 修改）
- **分隔符**: 使用下划线 `_` 替代层级点 `.`
- **格式**: `{PREFIX}_{SECTION}_{KEY}`（全大写）

**示例**: YAML `mysql.host` → 环境变量 `GENESIS_MYSQL_HOST`

## 7. 多环境支持

```text
config/
├── config.yaml          # 基础配置
├── config.dev.yaml      # 开发环境覆盖
├── config.prod.yaml     # 生产环境覆盖
└── config.local.yaml    # 本地开发覆盖 (git ignored)
```

加载逻辑：先加载 `config.yaml`，然后根据 `GENESIS_ENV` 环境变量加载对应的 `config.{env}.yaml` 进行合并覆盖。

## 8. 使用示例

```go
// 基础使用
loader, _ := config.New(
    config.WithConfigName("config"),
    config.WithConfigPath("./config"),
    config.WithEnvPrefix("GENESIS"),
)
loader.Load(ctx)

// 解析配置
var cfg AppConfig
loader.Unmarshal(&cfg)

// 部分解析
var mysqlCfg MySQLConfig
loader.UnmarshalKey("mysql", &mysqlCfg)

// 监听变化
ch, _ := loader.Watch(ctx, "mysql.host")
```

### 8.2 简单使用（无容器）

```go
loader, _ := config.New()
loader.Load(ctx)

var cfg Config
loader.Unmarshal(&cfg)
// 使用配置...
```

### 8.3 监听配置变化

```go
loader.Start(ctx)
defer loader.Stop(ctx)

ch, _ := loader.Watch(ctx, "database.host")
for event := range ch {
    log.Infof("Config changed: %s = %v", event.Key, event.Value)
}
```

## 9. 与 Container 的关系

- `Loader` 的构造与 `Load(ctx)` 发生在 Container 之外
- 流程：创建 Loader → Load → Unmarshal(&AppConfig) → 传给 Container
- Container 接收已准备好的 `AppConfig`，用于构建各类 Connector、业务组件、可观测性能力
- 若 `Loader` 实现了 `container.Lifecycle`，只在 Container 构建后由容器管理其 Watch 后台任务
- 这样避免了 Config ↔ Container 的循环依赖

## 10. 与远程配置中心的关系

- 当需要接入 Etcd/Consul 时，Config 可直接依赖底层 SDK 或复用 `pkg/connector/etcd`
- **Config 模块不得依赖 `pkg/container`**，只允许依赖更底层的能力
- 这确保依赖关系单向：Config → SDK/Connector，Container → Config

## 11. 实现细节

当前使用 `spf13/viper` 作为核心实现。

### 11.1 初始化流程

1. **New**: 创建 Loader 实例，配置 Options
2. **Load**: 依次加载 基础配置 → 环境特定配置 → .env 文件 → 环境变量（按优先级覆盖）
3. **Start**: 启动 WatchConfig 协程，监听文件变更
4. **Watch**: 返回一个只读 channel，当配置发生变化时发送事件

**注意**：实际加载顺序与优先级相反，低优先级先加载，高优先级后加载进行覆盖

### 11.2 热更新实现

利用 Viper 的 `WatchConfig` 能力，通过 `fsnotify` 监听文件变更。

## 12. 目录结构

```text
pkg/config/
├── config.go          # 工厂函数 New()
├── interface.go       # Loader 接口和 Event 类型
├── manager.go         # loader 具体实现
├── options.go         # Option 模式定义
├── errors.go          # 错误处理辅助函数
└── config_test.go     # 单元测试
```

## 13. 演进路线

### Phase 1: 本地文件 + 环境变量（当前）✓

- [x] 文件加载（YAML/JSON）
- [x] 环境变量覆盖
- [x] .env 文件支持
- [x] 多环境支持（config.{env}.yaml）
- [x] 热更新（Watch）
- [x] 验证机制（Validate）
- [x] 扁平化 API，使用 xerrors

### Phase 2: 远程配置中心（未来）

- [ ] Etcd 支持
- [ ] 配置版本管理与回滚
- [ ] 通过公共 Etcd Connector 复用连接
