# Genesis 重构计划 (Refactoring Plan)

本计划旨在指导将现有代码库重构为符合 [Component Specification](specs/component-spec.md) 和相关设计文档的新架构。

## 1. 重构目标

* **统一初始化:** 所有组件和连接器统一采用 `New(Dep, Config, ...Option)`（组件）和 `Factory(cfg, logger)`（连接器）模式，签名与 [Component Spec](specs/component-spec.md) / [Connector Design](connector-design.md) 保持一致。
* **强制注入:** 移除所有内部依赖创建，强制通过构造函数或 Option 注入 Logger、Connector、Metrics/Tracer 等依赖。
* **配置集中:** 通过 `pkg/config` 和 `AppConfig` 统一管理所有组件和连接器配置，业务代码不直接读取配置文件。
* **可观测性:** 统一日志 Namespace（`<app>.<component>[.<sub>]`）和 Metrics/Tracing 注入方式，后续按需补充埋点。

## 2. 重构阶段与顺序

建议按照 **自底向上** 的顺序进行重构，确保依赖链的稳定性。

### Phase 1: 基础组件 (Foundation)

**目标:** 校准配置和可观测性基础实现，使之与设计文档完全对齐。

1. **Config 模块 (`pkg/config`)**
    * **确认/补充:** `AppConfig` 及其子结构体（如 `ConnectorsConfig`、`ComponentsConfig`）的定义，与实际使用需求一致。
    * **确认:** `Manager` 接口与实现符合 `docs/config-design.md`
      中对 Bootstrapping / Lifecycle / 远程配置中心依赖边界的约束。
    * **清理:** 移除组件或业务内直接使用 viper 的代码，统一通过 `AppConfig` 传递配置。

2. **Telemetry 模块 (`pkg/telemetry`)**
    * **确认/补充:** 指标与 tracing 抽象接口（Meter、Tracer 等）与 `docs/telemetry-design.md` 保持一致。
    * **确认:** Container 能够在启动阶段将 `metrics.Meter` / `trace.Tracer` 注入到各组件 Option 中，组件内部不直接依赖 OTel SDK。

### Phase 2: 连接器层 (Connectors)

**目标:** 规范化连接器初始化和日志注入，确保 Manager 可被 Container 与 Config 复用。

1. **接口对齐 (`pkg/connector`)**
    * **确认:** `Factory` 函数签名为 `func(cfg C, logger clog.Logger) (Connector, error)`，与 `docs/connector-design.md` 一致。
    * **确认:** 连接器接口（`Connector`、`TypedConnector`、`Configurable` 等）与设计文档保持一致。

2. **实现更新 (`internal/connector`)**
    * **修改/确认:** `manager.go` 中的 `Get` 方法接收 `logger` 并传递给 Factory，内部不依赖 Container。
    * **修改/确认:** `redis`, `mysql`, `etcd`, `nats` 的具体实现，在初始化时保存 logger，并在后续操作中使用其记录日志，命名空间形如 `user-service.connector.redis.default`。
    * **验证:** Manager 可被 Container、Config 模块等同时复用，而不会引入循环依赖。

### Phase 3: 业务组件层 (Components)

**目标:** 统一组件目录结构和初始化模式，先选一两个组件做试点（建议从 `dlock` 开始）。

**优先顺序建议:** `dlock` → `db` → `mq` → `idgen` → `cache`

对于每个组件，执行以下步骤：

1. **配置结构体 (`pkg/<comp>/types/config.go`)**
    * **新建/修改:** 定义独立的 `Config` 结构体，添加 yaml tags，与 `config.AppConfig` 中对应字段对齐。
    * **移除:** 移除任何直接读取 viper 或文件的代码，仅通过传入的 Config 使用配置。

2. **Option 定义 (`pkg/<comp>/options.go`)**
    * **新建/修改:** 定义 `options` 结构体和 `WithLogger`, `WithMeter`, `WithTracer` 等方法，签名与 Component Spec 保持一致。

3. **工厂函数 (`pkg/<comp>/component.go`)**
    * **修改:** `New` 函数签名改为 `New(dep types.Dep, cfg types.Config, opts ...Option)`。
    * **实现:** 在函数内部解析 Options，并在 `WithLogger` 中调用 `logger.WithNamespace("<comp>")`，统一命名空间派生。

4. **内部实现 (`internal/<comp>`)**
    * **修改:** 构造函数接收处理后的 logger、meter、tracer 以及依赖 `dep`。
    * **埋点（按需）:** 在关键方法中预留或添加 Metrics 埋点（如 `Lock` 耗时），不强制一次性完成所有指标。

### Phase 4: 容器层 (Container)

**目标:** 组装所有组件，实现从 Config → AppConfig → Container → Components 的统一配置流。

1. **Container 结构体 (`pkg/container`)**
    * **对齐:** `New` 方法接收 `config.AppConfig`（指针或值）以及必要的 Option，例如 `WithLogger`、`WithConfigManager` 等。
    * **实现/确认:**
        * 初始化应用级 Root Logger（基于 `AppConfig.Log`），并通过 `WithNamespace(AppConfig.App.Namespace)` 附加服务级命名空间；
        * 初始化 Telemetry Provider，并为后续组件提供 `metrics.Meter` 与 `trace.Tracer`；
        * 初始化 Connectors（通过 Manager，注入 `logger.WithNamespace("connector.<type>.<name>")`）；
        * 初始化 Components（传入对应的 Dep、Config 和 `WithLogger` / `WithMeter` / `WithTracer`）。

2. **生命周期管理**
    * **验证:** 确保所有实现了 `container.Lifecycle` 的对象（config.Manager 可选、telemetry、connectors、components）正确注册到 `lifecycles` 列表，并按 Phase 排序启动/逆序关闭。

## 3. 重点关注的文档变更

在重构过程中，请务必反复查阅以下文档的特定章节：

| 文档 | 关键章节 | 关注点 |
| :--- | :--- | :--- |
| `docs/specs/component-spec.md` | **3. 初始化规范** | 确保 `New(Dep, Config, ...Option)` 签名和 Option 模式完全一致。 |
| `docs/specs/component-spec.md` | **4. 日志与命名空间** | 检查 Namespace 是否正确派生 (如 `user-service.dlock.redis`)。 |
| `docs/config-design.md` | **2.1/2.2/4 节** | 确保 `AppConfig` 定义完整，Manager 与 Container/Etcd 依赖边界符合规范。 |
| `docs/connector-design.md` | **1/4 节** | 注意 Factory/Manager `Get` 签名、Logger 注入和命名空间规则。 |
| `docs/telemetry-design.md` | **3/4/7 节** | 确保 Telemetry 初始化顺序与 Metrics/Tracing 注入方式与规范一致。 |

## 4. 验证标准

重构完成后，应通过以下方式验证：

1. **单元测试:** 现有单元测试应通过（可能需要适配新的 `New` 签名）。
2. **集成测试:** 编写一个 `main.go` 示例，使用 `config.yaml` 启动 Container，验证：
    * 日志输出是否包含正确的 Namespace。
    * 组件是否正常工作。
    * Metrics 是否有数据输出。
