# Genesis - Go 微服务基座库

一个轻量级的 Go 微服务基础库，提供稳定的接口层和可替换的实现，聚焦稳定性、可观测性和可扩展性。

## 定位

Genesis 是一个**库**（而非可执行项目），主要用途：
- 提供微服务常用的基础能力接口（日志、分布式锁等）
- 隐藏具体实现细节，便于平滑替换和 A/B 验证
- 通过依赖注入（`go.uber.org/fx`）管理组件生命周期

**不提供**：
- `cmd/` 可执行入口（示例在 `examples/` 目录）
- 特定的 Web 框架绑定（业务可自由选择）

## 核心特性

### 日志（Log）
- **接口**：`pkg/log.Logger` 提供结构化日志能力
- **默认实现**：基于 `go.uber.org/zap` 的高性能实现
- **特性**：
  - 多级日志（Debug, Info, Warn, Error）
  - 结构化字段支持
  - 上下文注入（TraceID、RequestID、UserID）
  - 可配置编码格式（JSON/Console）和输出目标

### 分布式锁（Lock）
- **接口**：`pkg/lock.Locker` 提供分布式锁能力
- **默认实现**：基于 Redis 的实现
- **特性**：
  - 非阻塞锁（`TryLock`）和阻塞锁（`Lock`）
  - 可配置 TTL、超时、重试策略
  - 基于 Lua 脚本的原子解锁（防误解）
  - Token-based 设计便于上层做幂等控制

## 快速开始

### 安装

```bash
go get github.com/<your-username>/genesis
```

### 日志示例

```go
package main

import (
	"context"
	"genesis/internal/log/zap"
	"genesis/pkg/log"
)

func main() {
	// 创建 logger
	cfg := zap.DefaultConfig()
	cfg.Level = log.InfoLevel
	logger, _ := zap.New(cfg)
	defer logger.Sync()

	// 基础日志
	logger.Info("Application started", log.String("version", "1.0"))

	// 上下文日志
	ctx := context.WithValue(context.Background(), log.TraceIDKey, "trace-123")
	logger.WithContext(ctx).Info("Processing request")

	// 字段日志
	logger.WithFields(log.String("module", "auth")).Info("User login")
}
```

### 分布式锁示例

```go
package main

import (
	"context"
	"genesis/internal/lock/redis"
	"genesis/pkg/lock"
	"time"
)

func main() {
	cfg := redis.Config{
		Addr:       "localhost:6379",
		DefaultTTL: 10 * time.Second,
	}
	locker, _ := redis.New(cfg)
	defer locker.Sync()

	ctx := context.Background()

	// 非阻塞锁
	guard, acquired, _ := locker.TryLock(ctx, "resource-1")
	if acquired {
		defer guard.Unlock(ctx)
		// 执行受保护的操作
	}

	// 阻塞锁（带超时）
	guard, _ := locker.Lock(ctx, "resource-2",
		lock.WithTTL{Duration: 5 * time.Second},
		lock.WithTimeout{Duration: 30 * time.Second},
	)
	defer guard.Unlock(ctx)
}
```

## 目录结构

```
genesis/
├── pkg/                       # 公共 API（稳定接口）
│   ├── log/                   # 日志接口定义
│   └── lock/                  # 分布式锁接口定义
├── internal/                  # 具体实现（不稳定 API）
│   ├── log/
│   │   └── zap/               # 基于 Zap 的日志实现
│   └── lock/
│       └── redis/             # 基于 Redis 的锁实现
├── examples/                  # 使用示例
│   ├── logging/               # 日志使用示例
│   └── locking-redis/         # Redis 锁使用示例
├── docs/                      # 设计文档和指南
├── go.mod                     # Go 模块定义
└── README.md                  # 本文件
```

## 设计原则

### 1. 抽象优先
- 业务代码只依赖 `pkg/*` 中的接口
- 具体实现在 `internal/` 中，业务代码不直接导入

**示例**：
```go
// 正确 ✓
import "genesis/pkg/log"
var logger log.Logger

// 错误 ✗
import "genesis/internal/log/zap"
logger := zap.New(cfg)  // 业务代码不应该知道具体实现
```

### 2. 依赖注入
- 优先使用 `go.uber.org/fx` 进行依赖注入
- 同时提供"手动构造器"供非 fx 项目使用

**Fx 示例**：
```go
import "go.uber.org/fx"

func NewApp() *fx.App {
	return fx.New(
		fx.Provide(
			func() (log.Logger, error) {
				cfg := zap.DefaultConfig()
				return zap.New(cfg)
			},
		),
		fx.Invoke(func(logger log.Logger) {
			logger.Info("App initialized")
		}),
	)
}
```

**手动示例**：
```go
logger, _ := zap.New(zap.DefaultConfig())
locker, _ := redis.New(redis.Config{Addr: "localhost:6379"})
```

### 3. 可观测性
- 所有组件暴露关键的日志输出
- 错误分级：
  - **Warn**：可恢复的问题（重试、降级可解决）
  - **Error**：需告警的问题（系统故障、配置错误）

### 4. 简洁交付
- 默认配置可直接工作
- 复杂能力通过 `Option` 参数按需启用

---

## 开发规范

### 新增组件流程

当需要添加新的基础能力（如配置管理、指标上报）时，遵循以下步骤：

#### 1. 定义公共接口 (`pkg/*/`)

在 `pkg/` 下新建模块，定义稳定的接口和数据结构。

**文件**：`pkg/newmodule/newmodule.go`
```go
package newmodule

type Component interface {
	// 核心方法
	Method(ctx context.Context, arg string) (result interface{}, err error)
	
	// 生命周期
	Sync() error
}

type Option interface{}
```

**规范**：
- 接口名称简洁、表意清晰
- 首参数必须是 `context.Context`（便于链路和取消传播）
- 用 `Option` 进行可选配置
- 明确生命周期方法（`Sync` 等）

#### 2. 实现默认实现 (`internal/*/provider/`)

在 `internal/` 下实现具体逻辑。

**文件**：`internal/newmodule/provider/provider.go`
```go
package provider

import "genesis/pkg/newmodule"

type Impl struct {
	// 私有字段
}

func New(cfg Config) (*Impl, error) {
	// 初始化逻辑
	return &Impl{}, nil
}

func (i *Impl) Method(ctx context.Context, arg string) (interface{}, error) {
	// 实现
}

func (i *Impl) Sync() error {
	// 清理资源
}
```

**规范**：
- 默认实现放在 `internal/modulename/defaultprovider/`
- 提供 `New()` 构造函数
- 若使用外部库，在此处统一管理依赖版本

#### 3. 编写测试 (`*_test.go`)

- **单元测试**：覆盖接口契约和边界条件
- **集成测试**：使用 Docker Compose 启动依赖服务

**示例**：`internal/newmodule/provider/provider_test.go`
```go
package provider

import (
	"context"
	"testing"
	
	"genesis/pkg/newmodule"
)

func TestMethod(t *testing.T) {
	impl, err := New(Config{})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer impl.Sync()

	result, err := impl.Method(context.Background(), "arg")
	if err != nil {
		t.Fatalf("Method failed: %v", err)
	}
	// assertions
}
```

#### 4. 添加示例 (`examples/`)

在 `examples/` 下添加最小可用示例，展示常用场景。

**规范**：
- 示例应独立可运行
- 包含注释说明关键步骤
- 文件结构 `examples/newmodule/main.go`

#### 5. 撰写文档 (`docs/`)

在 `docs/` 下添加使用指南，包括：
- 模块定位与能力说明
- API 参考
- 常见问题
- 扩展指南

**文件**：`docs/NEWMODULE.md`

### 编码规范

#### 命名约定
- 接口名：`Logger`, `Locker`, `Component` 等，避免 `I` 前缀
- 实现类型：`Impl`, `Provider` 或具体库名称（`ZapLogger`, `RedisLocker`）
- 配置结构体：`Config`
- 工厂函数：`New`, `NewWith[Config]`

#### 错误处理
```go
// 正确 ✓
if err != nil {
	return nil, fmt.Errorf("operation failed: %w", err)
}

// 在 Sync/Close 时吞掉部分错误是可接受的
_ = l.client.Close()
```

#### 日志输出
- 使用 `pkg/log.Logger` 而非 `fmt.Println`
- Error 日志配合返回 error
- Warn 日志用于可恢复情况

```go
if err := l.client.Ping(ctx).Err(); err != nil {
	logger.Error("Redis connection failed", log.Error(err))
	return nil, err
}
```

### 版本管理

- 遵循语义化版本 (SemVer)：`v0.y.z`
- `v0.y.z` 阶段允许快速迭代，`pkg/` 中的 API 应标注稳定性
- 破坏性更改需在 CHANGELOG.md 详细说明

### 提交清单

新增或修改组件前，确保满足以下条件：

- [ ] 接口定义清晰、文档完整（`pkg/*/`)
- [ ] 默认实现已验证（`internal/*/`)
- [ ] 单元测试覆盖核心路径（70% 以上代码覆盖率）
- [ ] 集成测试用例（若涉及外部服务）
- [ ] 示例代码可独立运行（`examples/*/`)
- [ ] 文档齐全（`docs/MODULE.md`)
- [ ] 更新 CHANGELOG.md
- [ ] 代码通过 `go fmt`, `go vet` 检查

---

## 最佳实践

### 1. 上下文的正确使用

**在处理链路日志时**，确保 context 在调用链中传递：

```go
func ProcessRequest(ctx context.Context, logger log.Logger) {
	ctx = context.WithValue(ctx, log.TraceIDKey, "trace-123")
	logger.WithContext(ctx).Info("Start processing")
	
	// 传递 ctx 到下游调用
	CallDownstream(ctx, logger)
}
```

### 2. 分布式锁的正确解锁

**务必使用 defer 确保解锁**：

```go
guard, err := locker.Lock(ctx, "key")
if err != nil {
	return err
}
defer guard.Unlock(ctx)
// 保护的操作
```

### 3. 接口隔离

**若业务需要扩展实现，仅依赖 `pkg/` 中的接口**：

```go
// 业务包
package mybiz

import "genesis/pkg/log"

func NewService(logger log.Logger) *Service {
	return &Service{logger: logger}
}

// 这样业务对具体实现无依赖，可灵活替换
```

### 4. 生命周期管理

**为所有长生命周期的组件实现 `Sync` 方法**：

```go
defer logger.Sync()
defer locker.Sync()
```

---

## 进展与路线图

### v0.1.0 (MVP)
- ✓ 基础目录结构
- ✓ `pkg/log` + `internal/log/zap`
- ✓ `pkg/lock` + `internal/lock/redis`
- ✓ 示例代码和文档

### v0.2.0 (规划中)
- [ ] `internal/lock/etcd` 实现
- [ ] 日志字段规范与最佳实践指南
- [ ] 错误分类体系

### v0.3.0 (规划中)
- [ ] 指标埋点 (Prometheus 格式)
- [ ] 链路追踪 (OpenTelemetry 集成)
- [ ] Docker Compose 集成测试样例

---

## 常见问题

**Q: 如何在非 fx 项目中使用？**

A: 直接使用 `New()` 构造函数即可：
```go
logger, _ := zap.New(zap.DefaultConfig())
```

**Q: 能否替换为其他日志库？**

A: 可以。只需实现 `pkg/log.Logger` 接口，业务代码无需变更。在初始化时切换即可。

**Q: Redis 连接失败怎么办？**

A: 会在 `New()` 时返回错误。建议配合健康检查和重试机制在启动阶段处理。

**Q: 支持日志文件输出吗？**

A: 支持。通过 `Config.Output` 指定文件路径即可。

---

## 许可

MIT License

## 贡献

欢迎提交 Issue 和 PR！请遵循上述开发规范。
