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
    - 影响：如有需要，提供 `WithConfig(cfg)`、`WithLogger(l)`、`WithMeter(m)`、`WithTracer(t)` 等选项。
4. 生命周期与阶段
    - 决策：`Lifecycle` 接口仅包含 `Start(ctx)` 和 `Stop(ctx)`；`Phase` 不属于组件接口。容器通过注册时提供的 phase 管理顺序：`RegisterWithPhase(lc, phase)`。
    - 理由：Phase 是编排层面的关注点，不是组件自身的关注点。
    - 影响：更新 `pkg/container`，以支持按 phase 注册和排序逻辑。
5. 错误处理
    - 决策：新增 `pkg/xerrors`，提供哨兵错误和包装辅助函数（Wrap、Is、As 兼容）。
    - 理由：统一的错误语义提升了可观测性和测试性。
    - 影响：实现 `xerrors`，并逐步迁移各组件的错误返回。

## 后续步骤

- 更新以下文档以反映上述决策：`component-spec.md`、`container-design.md`、`genesis-design.md`。
- 以 `pkg/ratelimit` 为试点，开始对 `pkg/*/types` 进行机械化扁平化重构。

---

最后更新：2025-11-25
