# Genesis 重构计划 (Refactoring Plan)

本计划旨在指导将现有代码库重构为符合 [Component Specification](specs/component-spec.md) 和相关设计文档的新架构。

## 1. 重构目标

* **统一初始化:** 所有组件支持 `New(Config, ...Option)` 模式。
* **强制注入:** 移除所有内部依赖创建，强制注入 Logger 和 Connector。
* **配置集中:** 引入 `AppConfig` 和 `Config` 组件。
* **可观测性:** 统一日志 Namespace 和 Metrics 埋点。

## 2. 重构阶段与顺序

建议按照 **自底向上** 的顺序进行重构，确保依赖链的稳定性。

### Phase 1: 基础组件 (Foundation)

**目标:** 建立配置和可观测性基础。

1. **Config 组件 (`pkg/config`)**
    * **新建:** 实现 `AppConfig` 结构体及其子结构体 (`ConnectorsConfig`, `ComponentsConfig`)。
    * **新建:** 实现 `Loader` 接口，支持 YAML 加载。
    * **参考:** `docs/config-design.md`

2. **Observability 组件 (`pkg/observability`)**
    * **新建:** 定义 `Metrics` 接口 (`Counter`, `Gauge`, `Histogram`)。
    * **参考:** `docs/observability-design.md`

### Phase 2: 连接器层 (Connectors)

**目标:** 规范化连接器初始化和日志注入。

1. **接口更新 (`pkg/connector`)**
    * **修改:** `Factory` 函数签名，增加 `logger clog.Logger` 参数。
    * **参考:** `docs/connector-design.md` (4.1 节)

2. **实现更新 (`internal/connector`)**
    * **修改:** `manager.go` 中的 `Get` 方法，接收 `logger` 并传递给 Factory。
    * **修改:** `redis`, `mysql`, `etcd`, `nats` 的具体实现，在初始化时保存 logger，并在后续操作中使用它记录日志。
    * **关键点:** 确保连接器内部日志使用注入的 logger (e.g., `connector.redis.default`)。

### Phase 3: 业务组件层 (Components)

**目标:** 统一组件结构和初始化模式。

**涉及组件:** `dlock`, `db`, `mq`, `idgen`, `cache` (按此顺序进行)

对于每个组件，执行以下步骤：

1. **配置结构体 (`pkg/<comp>/types/config.go`)**
    * **新建/修改:** 定义独立的 `Config` 结构体，添加 yaml tags。
    * **移除:** 移除任何直接读取 viper 或文件的代码。

2. **Option 定义 (`pkg/<comp>/options.go`)**
    * **新建:** 定义 `options` 结构体和 `WithLogger`, `WithMetrics` 方法。

3. **工厂函数 (`pkg/<comp>/component.go`)**
    * **修改:** `New` 函数签名改为 `New(conn, cfg, opts...)`。
    * **实现:** 在函数内部解析 Options，并调用 `logger.WithNamespace("<comp>")`。

4. **内部实现 (`internal/<comp>`)**
    * **修改:** 构造函数接收处理后的 logger 和 metrics。
    * **埋点:** 在关键方法中添加 Metrics 埋点 (e.g., `Lock` 耗时)。

### Phase 4: 容器层 (Container)

**目标:** 组装所有组件，实现自顶向下的配置流。

1. **Container 结构体 (`pkg/container`)**
    * **修改:** `New` 方法接收 `*config.AppConfig`。
    * **实现:**
        * 初始化 Root Logger (`cfg.Log`)。
        * 初始化 Metrics Provider。
        * 初始化 Connectors (注入 `logger.WithNamespace("connector.<type>.<name>")`)。
        * 初始化 Components (注入 `logger`, `metrics`, `connector`)。

2. **生命周期管理**
    * **验证:** 确保所有组件正确注册到 `lifecycles` 列表，并按 Phase 排序。

## 3. 重点关注的文档变更

在重构过程中，请务必反复查阅以下文档的特定章节：

| 文档 | 关键章节 | 关注点 |
| :--- | :--- | :--- |
| `docs/specs/component-spec.md` | **3. 初始化规范** | 确保 `New` 函数签名和 Option 模式完全一致。 |
| `docs/specs/component-spec.md` | **4. 日志与命名空间** | 检查 Namespace 是否正确派生 (e.g., `user-service.dlock`)。 |
| `docs/config-design.md` | **2.2 核心结构体** | 确保 `AppConfig` 包含所有组件的配置定义。 |
| `docs/connector-design.md` | **4.3 连接管理器** | 注意 `Manager.Get` 方法签名的变化 (增加了 logger)。 |
| `docs/observability-design.md` | **5. 命名规范** | 确保 Metrics Key 符合 `genesis_<comp>_<op>` 格式。 |

## 4. 验证标准

重构完成后，应通过以下方式验证：

1. **单元测试:** 现有单元测试应通过（可能需要适配新的 `New` 签名）。
2. **集成测试:** 编写一个 `main.go` 示例，使用 `config.yaml` 启动 Container，验证：
    * 日志输出是否包含正确的 Namespace。
    * 组件是否正常工作。
    * Metrics 是否有数据输出。
