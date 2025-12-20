# Genesis 重构进度追踪 (Refactoring Progress Tracking)

本文档用于追踪 Genesis 各组件的重构详细进度。每个组件的重构必须完成以下四个关键步骤：

1. **Code**: 代码重构完成（扁平化、DI 注入、资源所有权对齐）。
2. **Example**: 更新或新增对应的 `examples/` 示例。
3. **Design Docs**: 更新 `docs/` 下对应的详细设计文档。
4. **API Docs**: 生成或更新最新的 API 文档（Go Doc 兼容）。

---

## 进度总览

| 组件 | 层级 | Code | Example | Design | API | 状态 |
| :--- | :--- | :---: | :---: | :---: | :---: | :--- |
| **clog** | L0 | [x] | [x] | [x] | [x] | ✅ 完成 |
| **config** | L0 | [x] | [x] | [x] | [x] | ✅ 完成 |
| **metrics** | L0 | [x] | [x] | [x] | [x] | ✅ 完成 |
| **xerrors** | L0 | [x] | [x] | [x] | [x] | ✅ 完成 |
| **connector** | L1 | [x] | [x] | [x] | [x] | ✅ 完成 |
| **db** | L1 | [x] | [x] | [x] | [x] | ✅ 完成 |
| **dlock** | L2 | [x] | [x] | [x] | [x] | ✅ 完成 |
| **cache** | L2 | [x] | [x] | [x] | [x] | ✅ 完成 |
| **idgen** | L2 | [x] | [x] | [x] | [x] | ✅ 完成 |
| **mq** | L2 | [x] | [x] | [x] | [x] | ✅ 完成 |
| **idempotency** | L2 | [x] | [x] | [x] | [x] | ✅ 完成 |
| **auth** | L3 | [x] | [x] | [x] | [x] | ✅ 完成 |
| **ratelimit** | L3 | [ ] | [ ] | [ ] | [ ] | ⏳ 待重构 |
| **breaker** | L3 | [ ] | [ ] | [ ] | [ ] | ⏳ 待重构 |
| **registry** | L3 | [x] | [x] | [ ] | [x] | ✅ 完成 |

---

## 组件详细检查清单

### Level 0: Base

- [x] **clog**:
  - 代码已对齐 `slog`
  - 示例位于 `examples/clog`
  - 文档 `docs/foundation/clog-design.md` 已同步
- [x] **config**:
  - 代码支持强类型绑定
  - 示例位于 `examples/config`
  - 文档 `docs/foundation/config-design.md` 已同步

### Level 1: Infrastructure

- [x] **connector**:
  - 移除 Lifecycle 接口
  - 示例位于 `examples/connector`
  - 文档 `docs/infrastructure/connector-design.md` 已同步

### Level 2: Business

- [x] **dlock**:
  - 扁平化重构完成
  - 支持 Redis/Etcd
  - 示例 `examples/dlock-redis`, `examples/dlock-etcd`
  - 文档 `docs/business/dlock-design.md`
- [x] **cache**:
  - 扁平化重构完成
  - 示例 `examples/cache`
  - 文档 `docs/business/cache-design.md`

### Level 3: Governance

- [x] **auth**:
  - [x] 代码重构 (pkg/auth)
  - [x] 示例更新 (examples/auth)
  - [x] 更新 `docs/governance/auth-design.md`
  - [x] 生成 API 文档
- [x] **registry**:
  - [x] 代码重构 (pkg/registry)
  - [x] 示例更新 (examples/registry, examples/grpc-registry)
  - [x] 生成 API 文档
  - [x] 删除 types 子包，实现扁平化结构
  - [x] 完整 Etcd 实现迁移，避免循环依赖
  - [x] 完整 gRPC resolver 集成

---

## 重构标准规范 (DoD)

- **扁平化**: 消除 `pkg/*/types`，导出类型直接在 `pkg/*` 下。
- **显式 DI**: 构造函数 `New(conn, cfg, ...opts)`。
- **资源所有权**: 组件 `Close()` 为 no-op（借用模式）。
- **示例一致性**: `examples/` 必须能够直接运行并展示核心用法。
