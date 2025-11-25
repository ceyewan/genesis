# Genesis 重构与审计计划 (Genesis Refactoring & Audit Plan)

> NOTE: 本文档为当前重构的执行计划（source-of-truth）。在进行代码重构或调整架构时，应以本文件的约定为优先；其他设计文档（如 `genesis-design.md`、`component-spec.md`）为参考。任何偏离本计划的重要决策，应记录在 `docs/reviews/architecture-decisions.md`。

**目标**：将 Genesis 从原型集合转变为生产级、符合 Go 习惯的微服务基座库。
**核心原则**：**层次分明**（分层架构）、**易用**（扁平化 API）、**健壮**（统一规范）、**工程化**（CI/CD）。

---

## 1. 总体架构：四层模型 (Four-Layer Architecture)

我们将 Genesis 的组件划分为四个逻辑层次，针对不同层次采用不同的重构策略：

| 层次                                                  | 核心组件                                                | 职责                                                       | 重构策略                                                                               | 目录结构                                           |
| :---------------------------------------------------- | :------------------------------------------------------ | :--------------------------------------------------------- | :------------------------------------------------------------------------------------- | :------------------------------------------------- |
| **Glue Layer**`<br>`(胶水层)                  | `container`, `config`                               | 组装依赖，编排生命周期。                                   | **保持极简**。不侵入业务，只负责 `Start/Stop`。                                | `pkg/container`                                  |
| **Level 3: Governance**`<br>`(治理层)         | `ratelimit`, `breaker`, `registry`, `telemetry` | 流量治理，切面属性。通常作为 Middleware/Interceptor 存在。 | **Core 扁平化 + Adapter 丰富化**。重点建设 gRPC/HTTP 适配器。                    | `pkg/{comp}` (Core)`<br>pkg/{comp}/adapter`    |
| **Level 2: Business**`<br>`(业务能力层)       | `cache`, `idgen`, `dlock`, `idempotency`        | 具体的业务逻辑封装，被业务代码直接调用。                   | **完全扁平化**。为了极致易用，直接暴露高级 API。                                 | `pkg/{comp}<br>`(无 internal)                    |
| **Level 1: Infrastructure**`<br>`(基础设施层) | `connector`, `db`, `mq`                           | 连接管理，底层 I/O。                                       | **保持分层**。驱动逻辑复杂，保持 `interface`(pkg) 与 `impl`(internal) 分离。 | `pkg/{comp}` (API)`<br>internal/{comp}` (Impl) |
| **Level 0: Base**`<br>`(基石层)               | `clog` (Log), `telemetry` (Metric/Trace)            | 框架基石，无处不在。                                       | **极度稳定与规范**。强制所有上层组件通过 Option 注入。                           | `pkg/clog<br>``pkg/telemetry`                  |

---

## 2. 架构重构详细方案

### 2.1. 组件扁平化 (Flattening Strategy)

* **适用范围**：Level 2 (Business) 和 Level 3 (Governance) 的 Core 部分。
* **行动**：
  1. **移除 `internal`**：将 `internal/{comp}/*` 实现逻辑移至 `pkg/{comp}/`。
  2. **封装机制**：使用 **非导出结构体**（如 `type redisLimiter struct`）实现封装。用户只面向 Interface 编程。
  3. **移除 `types` 子包**：将 `Config`、`Interface`、`Errors` 移至 `pkg/{component}/` 根目录。避免 `ratelimit.New(&types.Config)` 这种冗余写法，改为 `ratelimit.New(&ratelimit.Config)`。

### 2.2. 基础设施分层 (Layered Strategy)

* **适用范围**：Level 1 (Infrastructure)，主要是 `connector`。
* **行动**：
  * **保留** `internal/connector/manager.go` 及具体驱动实现（mysql, redis）。
  * **理由**：驱动层逻辑复杂，包含连接池管理、健康检查等，且不应被用户直接 import 具体实现代码。

---

## 3. API 与开发规范 (Specifications)

### 3.1. 构造函数规范 (Constructor Spec)

所有组件的 `New` 函数必须遵循以下签名规范：

```go
// 1. 必选参数：核心依赖 (如 Connector, Config)
// 2. 可选参数：统一使用 Option 模式
// 3. 禁止：在必选参数中传递 Logger/Metric/Tracer
func New(conn connector.RedisConnector, opts ...Option) (Interface, error)
```

* **Context 规范**：`New` 函数**禁止包含阻塞 I/O**，也不接受 `context.Context`。所有 I/O 操作推迟到 `Start(ctx)` 或具体业务方法中。

### 3.2. Level 0 能力接入规范 (Base Capabilities)

所有组件**必须**通过 `Option` 接入 Level 0 能力：

1. **Logging (`clog`)**:
   * 必须提供 `WithLogger(clog.Logger)` Option。
   * 组件内部必须使用 `logger.With("component", "xxx")` 派生子 Logger。
2. **Telemetry**:
   * 必须提供 `WithMeter(meter)` 和 `WithTracer(tracer)`。
   * 核心逻辑必须埋点（Metrics）和传递 Context（Tracing）。
3. **Config**:
   * 组件**禁止**直接读取配置文件或环境变量。
   * 只能接受由上层传入的 `Config` 结构体。

### 3.3. 初始化与生命周期规范 (Initialization & Lifecycle)

Genesis 采用 **"容器优先的双模式" (Container-First Dual Mode)** 策略，确保组件既能在生产环境中由容器统一管理，也能在测试/脚本中独立运行。

1. **双模式设计 (Dual Mode)**:

   * **独立模式 (Standalone)**: 组件必须提供纯粹的 `New(Dep, Config, ...Option)` 工厂函数。它**不感知** Container 的存在，由调用方手动管理依赖注入和资源释放。适用于单元测试、工具脚本。
   * **容器模式 (Container)**: Container 充当 **Orchestrator**。它负责加载配置、创建 Connector、调用组件的 standard `New` 函数，并将组件注册到生命周期管理器中。
2. **生命周期接口 (Lifecycle Interface)**:

   * 所有拥有后台任务（如定时清理）或持有长连接资源（如 Connector）的组件，必须实现 `Start/Stop` 接口：

     ```go
     type Lifecycle interface {
         Start(ctx context.Context) error
         Stop(ctx context.Context) error
     }
     ```

   * **职责划分**:

     * **Component/Connector (Worker)**: 负责实现具体的连接建立、后台任务启动和资源释放逻辑。
     * **Container (Manager)**: 负责维护组件依赖拓扑，按正确顺序（Infrastructure -> Business -> Governance）调用 `Start`，并按**相反顺序**调用 `Stop` (Graceful Shutdown)。

### 3.4. 错误处理 (Error Handling)

* **统一组件**：新增 `pkg/xerrors` 组件，定义统一的错误码和错误包装器。
* **规范**：所有对外返回的错误必须 Wrap 原始错误，并使用 Sentinel Errors（如 `ErrNotFound`）。

---

## 4. 工程化建设 (Engineering)

1. **Makefile**: 提供 `make test`, `make lint`, `make up` (启动 dev 环境)。
2. **CI/CD**: 添加 `.github/workflows/ci.yml`，自动化运行测试和 Lint。
3. **Dev Env**: 整合 `deploy/*.yml` 为 `docker-compose.dev.yml`，一键拉起 MySQL/Redis/Etcd/NATS。

---

## 5. 执行路线图 (Execution Roadmap)

| 阶段                           | 任务                                   | 详情                                                           |
| :----------------------------- | :------------------------------------- | :------------------------------------------------------------- |
| **Phase 1: Pilot**       | **Refactor Ratelimit**           | 试点组件。扁平化结构，实现 `New` 规范，接入 Level 0 Option。 |
| **Phase 2: Engineering** | **Infrastructure Setup**         | 创建 Makefile, CI Workflow, Docker Compose Dev 环境。          |
| **Phase 3: Core (L2)**   | **Refactor Cache, Idgen, Dlock** | 执行扁平化重构。                                               |
| **Phase 4: Feature**     | **Add xerrors**                  | 设计并实现统一错误处理组件。                                   |
| **Phase 5: Infra (L1)**  | **Refactor Connector, MQ**       | 保持分层，但优化 `types` 包和 API。                          |
| **Phase 6: Gov (L3)**    | **Refactor Registry, Breaker**   | 重点建设 Adapter (Middleware)。                                |
