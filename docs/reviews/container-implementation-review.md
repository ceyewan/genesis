# Container 模块审查报告

**审查时间:** 2025-11-23
**审查对象:** `pkg/container`, `examples/connector/main.go`, `examples/dlock-etcd/main.go`
**参考文档:**

- `docs/container-design.md`
- `docs/genesis-design.md`
- `docs/specs/component-spec.md`

## 1. 总体评价

`container` 模块作为 Genesis 框架的"骨架"，实现了核心的组件组装、生命周期管理和依赖注入功能。
当前实现虽然逻辑清晰，能够满足基本的运行需求，但与设计文档相比存在**显著的功能缺失**（主要是 Telemetry 和 Option 模式），处于"可用但不完整"的状态。

| 维度 | 评分 | 说明 |
|---|---|---|
| **设计一致性** | ⚠️ 中 | 核心流程一致，但 API 签名和 Telemetry 集成严重偏离设计 |
| **功能完备性** | ⚠️ 中 | 缺失 Telemetry、Cache 支持；New 函数缺乏灵活性 |
| **代码质量** | ✅ 高 | 生命周期管理 (`LifecycleManager`) 和管理器集成逻辑清晰 |
| **Example 覆盖** | ✅ 高 | `connector` 和 `dlock` 的示例很好地展示了容器的用法 |

---

## 2. 详细发现

### 2.1 优点 (Strengths)

1. **生命周期管理 (Lifecycle Management):**
    - `LifecycleManager` 实现了基于 Phase 的排序启动和逆序关闭。
    - `StartAll` 和 `StopAll` 逻辑健壮，能够正确处理依赖关系。
    - **代码位置:** `pkg/container/lifecycle.go`

2. **连接器集成 (Connector Integration):**
    - 很好地利用了 `internal/connector/manager` 来管理连接器实例。
    - 在 `New` 流程中正确初始化了 MySQL, Redis, Etcd, NATS 并注册了生命周期。
    - **代码位置:** `pkg/container/container.go` -> `initManagers`, `initConnectors`

3. **日志集成 (Logging):**
    - 实现了自动为组件和连接器派生 namespace (e.g., `WithNamespace("mysql")`)，符合规范。

### 2.2 偏差与缺陷 (Deviations & Gaps)

#### 🛑 严重 (Critical)

1. **缺失 Telemetry 集成:**
    - **设计要求:** 设计文档明确指出 Container 负责 "初始化 Telemetry... 导出 `metrics.Meter` 和 `trace.Tracer` 供后续组件使用"。
    - **现状:** `Container` 结构体和初始化流程中完全没有 Telemetry、Tracing 或 Metrics 的相关代码。这意味着目前的组件无法获得可观测性能力的注入。
    - **影响:** 违反了 "可观测性优先" 的设计原则。

2. **API 签名不符 (Option Pattern 缺失):**
    - **设计要求:** `container.New(appCfg, container.WithLogger(logger), ...)`。
    - **现状:** `func New(cfg *Config) (*Container, error)`。
    - **问题:**
        - 不支持传入外部已初始化好的 Logger（目前是内部强行 `clog.New`）。
        - 不支持传入 `config.Manager`。
        - 扩展性差，未来增加参数需要修改函数签名。

#### ⚠️ 中等 (Medium)

1. **Cache 组件未实现:**
    - `Container` 结构体中有 `Cache Cache` 字段，但在 `initComponents` 中未进行初始化。
    - 虽然 Roadmap 中 Cache 是 "Planned"，但代码中留空的字段应该有注释或 TODO。

2. **Config 依赖方式差异:**
    - **设计:** 建议 `Container` 接收 `config.Manager` 或通用的 `AppConfig`。
    - **现状:** `New` 接收一个巨大的 `container.Config` 结构体，这要求调用方必须手动拼装这个大结构体，而不是直接传递业务层面的 `AppConfig`。这使得 `container` 包与业务配置耦合较紧。

### 2.3 Example 审查

- **`examples/connector/main.go`:**
  - ✅ 演示了手动构建 `container.Config`。
  - ✅ 演示了 `container.New` 和 `defer app.Close()`。
  - ✅ 演示了通过 `app.GetMySQLConnector` 获取资源。
  - ⚠️ 代码中展示的是手动拼装 Config，比较繁琐，侧面印证了 Config 集成方式有待优化。

## 3. 改进建议 (Action Plan)

建议按以下优先级进行重构：

1. **重构 `New` 函数 (Refactor Constructor):**
    - 引入 Option 模式：`type Option func(*Container)`。
    - 支持 `WithLogger`, `WithTracer`, `WithMeter`。
    - 允许外部注入 `clog.Logger`，而不是在内部重新初始化。

2. **集成 Telemetry (Integrate Telemetry):**
    - 在 `Container` 中增加 `Tracer trace.Tracer` 和 `Meter metrics.Meter` 字段。
    - 在 `initComponents` 阶段，将 Tracer/Meter 传递给组件（需要组件支持 `WithTracer/WithMeter` Option）。

3. **完善 Cache 支持:**
    - 实现 Cache 组件的初始化逻辑。

4. **优化配置定义:**
    - 考虑让 `New` 接收一个更通用的配置接口，或者保持现状但提供辅助工具来从 `config.Manager` 转换配置。

## 4. 结论

`container` 模块的**核心骨架（生命周期、连接器管理）是稳健的**，但在**可扩展性（Option 模式）**和**可观测性（Telemetry）**方面存在主要缺失。建议在下一阶段重点补齐 Telemetry 集成，并将 `New` 函数重构为 Option 模式以符合设计规范。
