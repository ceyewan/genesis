# 架构决策 — Genesis

本文档记录了持续重构过程中采纳的决策。它是一个简明的架构决策记录（ADR），总结了所做的选择、其理由及对实现和文档的影响。

## 决策

1. 事实来源
    - 决策：`docs/refactoring-plan.md` 是当前重构执行的事实来源。
    - 理由：该文件包含可执行的阶段和与当前目标一致的执行路线图。
    - 影响：其他设计文档需更新以引用或与该文件保持一致。

2. 目录结构 / 扁平化
    - 决策：L2（业务）和 L3（治理）组件将采用扁平的 `pkg/<component>/` 结构，在包根目录下暴露 `Config`、`Interface` 和 `Errors`。L1（基础设施）组件保留 `internal/` 以实现复杂的驱动。
    - 理由：扁平化提升了使用者的便利性（更短的导入路径），而 `internal/` 保护了复杂驱动的内部实现。
    - 影响：将类型从 `pkg/<component>/types/` 移动到 `pkg/<component>/`，如有需要，使用非导出的实现类型。

3. New() 签名
    - 决策：`New` 只接受核心依赖作为必需参数（如 connectors），可选的 logger/meter/tracer/config 通过 `opts ...Option`（Option 模式）注入。避免将 logger/metrics/tracer 作为必需参数传递。
    - 理由：保持构造函数简洁且与依赖注入一致；可观测性通过选项注入。
    - 影响：提供 `WithLogger(l)`, `WithMeter(m)` 等选项。

4. 移除容器与显式生命周期管理
    - 决策：完全移除 `pkg/container`。资源生命周期通过 Go Native 的 `defer` 机制管理。
    - 理由：DI 容器在 Go 中往往引入不必要的复杂度（魔法），显式初始化更符合 Go 哲学。
    - 影响：删除 `pkg/container`，更新所有文档和示例以展示显式 DI。

5. 资源所有权 (Ownership)
    - 决策：确立 "谁创建，谁负责释放" 的原则。组件（Borrower）通常不负责释放传入的连接器（Owner）。
    - 理由：避免重复释放或过早释放连接池资源。
    - 影响：组件的 `Close()` 方法通常实现为 no-op，除非它拥有独占资源。

6. 错误处理
    - 决策：使用 `pkg/xerrors` 提供统一的哨兵错误和包装辅助函数。
    - 理由：统一的错误语义提升了可观测性和测试性。
    - 影响：迁移各组件至 `xerrors`。

## 后续步骤

- 更新 `component-spec.md` 和 `genesis-design.md` 以对齐上述决策（已完成）。
- 继续执行 `pkg/ratelimit`、`pkg/breaker` 等治理组件的扁平化重构。

---

最后更新：2025-12-20
