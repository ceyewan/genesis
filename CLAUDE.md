# CLAUDE.md

此文件约束 AI 助手在 `Genesis` 仓库中的工作方式。**全程使用中文交流**。

## 项目概述

`Genesis` 是一个 Go 语言微服务组件库，沉淀可复用的基础设施组件。采用四层扁平化架构，通过显式依赖注入和 Go 原生设计，帮助开发者快速构建健壮、可维护的微服务应用。

**Genesis 不是框架**——提供积木，用户自己搭建。

## 架构概览

| 层次 | 核心组件 | 职责 |
| :--- | :--- | :--- |
| **Level 3: Governance** | `auth`, `ratelimit`, `breaker`, `registry` | 流量治理，身份认证 |
| **Level 2: Business** | `cache`, `idgen`, `dlock`, `idem`, `mq` | 业务能力封装 |
| **Level 1: Infrastructure** | `connector`, `db` | 连接管理，底层 I/O |
| **Level 0: Base** | `clog`, `config`, `metrics`, `xerrors` | 框架基石，被所有上层依赖 |

设计原则：显式优于隐式、简单优于聪明、组合优于继承。

## 关键依赖

- **语言**：Go 1.26
- **日志**：`slog`（标准库）+ `clog`（Genesis 封装）
- **ORM**：GORM
- **配置**：Viper
- **指标**：OpenTelemetry
- **缓存**：Redis / 内存
- **分布式锁**：Redis / Etcd
- **消息队列**：NATS (Core/JetStream) / Redis Stream
- **数据库**：MySQL（支持分库分表）

## 开发指南

```bash
# 环境管理
make up          # 启动所有开发服务（Redis, MySQL, Etcd, NATS）
make down        # 停止所有开发服务
make status      # 查看服务状态
make logs        # 查看服务日志

# 代码质量
go test ./...                    # 运行所有测试
go test -race -count=1 ./...     # 带竞态检测
make lint                        # 运行 golangci-lint
go doc -all ./<component>        # 查看组件文档

# 示例
make examples                    # 列出所有示例
make example-<component>         # 运行特定组件示例
make example-all                 # 运行所有示例
```

## 代码风格

### 格式化与 Import

- 格式化使用 `gofumpt`（比 `gofmt` 更严格），通过 `golangci-lint` 强制执行
- Import 使用 `goimports` 格式，分三组：标准库 / 第三方 / 内部包（`github.com/ceyewan/genesis/...`）

### 命名

- 导出符号用 PascalCase，未导出用 camelCase，遵循标准 Go 命名约定
- JSON 字段名使用 snake_case（`json:"user_id"`）
- 枚举使用带类型的 `iota` 常量，相关常量统一放在同一个 `const` 块中

### 类型与接口

- 优先使用显式类型，可用类型别名增强语义（如 `type DriverName string`）
- 接口在消费方包中定义，保持小而专注（`interface{ Close() error }` 而非大而全）
- 用 struct embedding 实现组合，相关字段放在一起

### Context 与错误

- `context.Context` 始终作为函数第一个参数
- 错误使用 `xerrors` 包装（`xerrors.Wrap` / `xerrors.New`），禁止 `errors.New` / `fmt.Errorf`
- 错误要显式返回，不要忽略

### L0 基础组件（强制）

- 日志：使用 `clog`，禁止 `log.Printf` / `fmt.Printf`
- 配置：使用 `config`，禁止直接读取环境变量
- 指标：使用 `metrics`，禁止直接使用 Prometheus 客户端
- 使用函数式选项模式（`WithXxx`）暴露组件配置
- 遵循"谁创建，谁 `Close()`"的资源所有权原则

### 日志与注释

- 日志消息首字母大写（`"Cache miss"` 而非 `"cache miss"`）
- 注释以句号结尾，行尾注释除外

### 文件权限

- 文件权限使用八进制字面量（`0o755`、`0o644`），不使用十进制

### 组件初始化标准模式

```go
cfg, _ := config.Load("config.yaml")
logger, _ := clog.New(&cfg.Log)
defer logger.Close()

redisConn, _ := connector.NewRedis(&cfg.Redis, connector.WithLogger(logger))
defer redisConn.Close()

cache, _ := cache.New(redisConn, &cfg.Cache, cache.WithLogger(logger))
```

### 禁止的写法

```go
log.Printf("cache miss: %s", key)   // ❌ 使用 clog
fmt.Errorf("failed: %w", err)       // ❌ 使用 xerrors.Wrap
os.Getenv("CACHE_PREFIX")           // ❌ 使用 config
```

## 测试规范

详见 [测试指南](testkit/testing-guide.md)。核心要求：

- 优先使用 `testkit`：`testkit.GetRedisClient(t)` 等方法获取基础设施连接
- 真实集成测试：依赖 Redis、MySQL 等的测试通过 `testkit` 内置的 **testcontainers** 自动启动容器，无需手动执行 `make up`
- **`make up` / `make status` 只用于运行 `examples`**，AI 不应在运行测试前执行这些命令
- 可复用的测试代码写在 `testkit` 包，供全局复用
- 业务核心逻辑覆盖率 > 80%
- 使用 `testify/require` 包做断言（不用 `assert`，失败立即停止）
- 可并行的测试用 `t.Parallel()` 标记
- 需要临时目录时使用 `t.TempDir()`，无需手动清理
- 需要设置环境变量时使用 `t.Setenv()`，测试结束后自动还原

## Git 工作流

**查看状态**：`git status` / `git log --oneline` / `git diff`，不使用交互式命令。

### 分支命名

`<type>/<description>`，类型：`feature` | `fix` | `refactor` | `docs` | `chore`

### 提交规范

```
<type>(<scope>): <中文简述>

- 具体变更说明
- 为什么做这个改动
```

类型：`feat`, `fix`, `refactor`, `docs`, `style`, `test`, `chore`

**不允许在 commit 信息中添加 Co-Authored-By 等 AI 署名信息。**

## 行为准则

1. **先读文档再动手**：对接口、配置、流程有疑问，先用 `go doc -all ./<component>` 查阅，再读源码。
2. **不确定就确认**：不确定的改动先询问，避免"差不多"式修改。
3. **复用优于新建**：优先使用已有接口和工具，不新增无用抽象。
4. **改后必须验证**：改动后运行对应测试或 lint，确保行为稳定。
5. **遵守架构边界**：保持四层扁平化架构，不将第三方依赖泄漏到组件接口。
6. **谨慎重构**：重构前理解上下游调用，必要时分步进行。
