# CLAUDE.md

此文件用于约束 Claude Code (claude.ai/code) 在 `Genesis` 仓库中的工作方式。请全程使用中文交流与记录，遵守下述行为准则，并对自己负责。

## 项目概览

`Genesis` 是一个 Go 语言微服务基座项目，旨在沉淀可复用的基础设施组件。仓库采用单一 `go.mod`，通过 `pkg/` 定义抽象接口，在 `internal/` 提供默认实现，并计划使用 `go.uber.org/fx` 完成依赖注入和生命周期管理。

## 行为准则（编程版八荣八耻）
1. **以凭空猜测为耻，以查阅文档为荣**：对接口、配置、流程有疑问时，先读 `docs/`、`pkg/` 源码与注释。
2. **以模糊执行为耻，以确认反馈为荣**：不确定的改动先询问或验证，避免"差不多"式修改。
3. **以自说自话为耻，以对齐需求为荣**：实现前确认需求背景与边界，必要时向人类说明假设。
4. **以重复造轮为耻，以复用抽象为荣**：优先复用 `pkg/` 接口和已存在的工具，避免新增无用抽象。
5. **以跳过验证为耻，以主动测试为荣**：改动后运行对应的测试或静态检查，确保功能与行为稳定。
6. **以破坏架构为耻，以遵循规范为荣**：保持 `pkg` 暴露接口、`internal` 隐藏实现的结构，不将第三方依赖泄漏到 `pkg`。
7. **以假装理解为耻，以诚实求助为荣**：遇到不懂的概念或代码路径，坦诚指出并寻找答案。
8. **以鲁莽提交为耻，以谨慎重构为荣**：重构前先理解上下游调用，必要时分步骤进行并记录风险。

## 开发命令

### 基础命令
```bash
# 依赖管理
go mod tidy

# 编译检查
go build ./...

# 运行测试
go test ./...
go test -v ./pkg/log/...  # 运行特定包的测试
go test -run TestSpecificFunction ./pkg/log/  # 运行特定测试函数

# 代码格式化
go fmt ./...

# 静态分析
go vet ./...

# 生成文档示例
go test -run Example ./pkg/...
```

### 应用运行
```bash
# 运行主服务（当前为占位符实现）
go run ./cmd/server

# 查看项目版本信息
go version
```

## 核心架构

### 接口与实现分离
```
pkg/                    # 对外接口层 - 业务代码依赖
├── cache/             # 缓存接口
├── config/            # 配置管理接口  
├── db/                # 数据库接口
├── log/               # 日志接口 ✅ 已完整实现
├── middleware/        # 中间件接口
├── mq/                # 消息队列接口
├── uid/               # ID生成接口
└── coord/             # 分布式协调接口

internal/               # 具体实现层 - 可替换扩展
├── cache/redis/       # Redis 缓存实现
├── config/viper/      # Viper 配置实现
├── db/gorm/           # GORM 数据库实现
├── log/zap/           # Zap 日志实现 ✅ 已完整实现
├── middleware/        # 限流、熔断、幂等实现
├── mq/kafka/          # Kafka 消息队列实现
├── uid/snowflake/     # 雪花算法 ID 实现
└── coord/etcd/        # etcd 协调实现
```

### 已实现组件
**日志组件 (pkg/log/ + internal/log/zap/)**
- 支持结构化日志 (JSON/Console 格式)
- 层次化命名空间 (`logger.Namespace("module")`)
- 上下文感知 (`log.WithContext(ctx)`)
- 分布式追踪支持 (`log.WithTraceID(ctx, traceID)`)
- 日志轮转 (基于 lumberjack)
- 开发/生产环境配置 (`log.GetDefaultConfig("development")`)

### 依赖注入模式
- 使用 `go.uber.org/fx` 进行依赖管理
- 组件通过构造函数接受依赖
- 生命周期统一管理

### 配置驱动
- 统一的配置接口 (`config.Provider`)
- 支持热重载 (`Watch()` 方法)
- 环境差异化配置 (`GetDefaultConfig(env)`)

## 工作流程

### 新增组件开发流程
1. **接口定义**: 在 `pkg/component/` 中定义接口
2. **默认实现**: 在 `internal/component/provider/` 中实现
3. **单元测试**: 编写核心逻辑测试用例
4. **集成测试**: 编写端到端测试
5. **文档更新**: 更新 `docs/component.md`
6. **示例代码**: 提供 `*_example_test.go`

### 日志组件使用示例
```go
// 初始化
config := log.GetDefaultConfig("development")
err := log.Init(ctx, config, log.WithNamespace("user-service"))

// 使用日志器
logger := log.WithContext(ctx)
logger.Info("user login", log.String("user_id", "12345"))

// 命名空间日志
userLogger := log.Namespace("auth")
userLogger.Error("authentication failed", log.Err(err))
```

## 代码守则

### 接口设计原则
- 所有接口方法接受 `context.Context` 作为第一个参数
- 错误处理使用 `fmt.Errorf("...: %w", err)` 包装错误
- 接口保持最小化，避免过度抽象

### 实现层约束
- `internal/` 包不能被 `pkg/` 导入
- 实现不应泄漏第三方依赖到接口层
- 提供降级方案，避免单点故障

### 配置管理
- 组件配置通过结构体定义，支持 JSON/YAML 标签
- 必须提供 `Validate()` 方法检查配置有效性
- 支持环境变量覆盖

## 绝对禁止
- 直接向仓库提交明文密钥或凭证
- 擅自改动用户未交待的历史文件，除非修复因本次改动引发的问题
- 使用英文与用户或日志交互（除代码与 Go 保留字外）
- 破坏 `pkg` 与 `internal` 的分层架构

## 分布式锁组件设计经验（etcd-lock-optimization）

### 项目阶段认知
当前处于开发阶段，**不需要考虑API向后兼容性**，优先保证：
- 易用性：一行初始化，简洁API
- 简单性：避免用户困扰
- 架构清晰：防止循环依赖

### 核心设计成果

**新的简单API**：
```go
// 一行初始化，全部默认
locker, err := simple.New(nil, nil)

// 自定义连接配置
locker, err := simple.New(&simple.Config{
    Backend:   "etcd",
    Endpoints: []string{"localhost:2379"},
}, nil)

// 自定义行为配置
locker, err := simple.New(nil, &simple.Option{
    TTL:       30 * time.Second,
    AutoRenew: false,
})
```

**架构层次（无循环依赖）**：
```
pkg/lock/simple/          (新API层)
  ↓
pkg/lock/                 (接口定义)
  ↓
internal/lock/            (实现层)
  ↓
internal/connector/       (连接管理层)
```

### 关键设计原则

1. **遵循Go规范**：`New(config, option)` - config必需，option可选
2. **职责分离**：Config管连接，Option管行为
3. **零值友好**：两个参数都可为nil，自动应用合理默认值
4. **连接复用**：相同配置自动复用etcd连接，提升性能

### 避免循环依赖的方法

**验证命令**：
```bash
go build ./...                    # 确保编译成功
go list -json ./pkg/lock/simple   # 检查依赖关系，确认无循环
```

**设计要点**：
- 保持单向依赖层次
- 接口定义在最上层（pkg/lock）
- 实现层不依赖具体API层
- 连接管理器作为独立底层服务

### 配置分离策略

**Config（连接相关）**：
- Backend：后端类型
- Endpoints：连接地址
- Username/Password：认证信息
- Timeout：连接超时

**Option（行为相关）**：
- TTL：锁超时时间
- RetryInterval：重试间隔
- AutoRenew：自动续期
- MaxRetries：最大重试次数

### 性能优化

**连接复用机制**：
- SHA256配置哈希作为连接池key
- 读写锁优化并发性能
- 双重检查避免重复创建
- 懒加载连接创建

### 开发阶段认知

1. **无需向后兼容**：可大胆重构API
2. **优先用户体验**：一行初始化，直观易用
3. **架构清晰更重要**：避免为了兼容而妥协设计
4. **渐进式重构**：分步骤验证，降低风险

遵守上述规范，可确保在 Genesis 项目中高效、安全地迭代。