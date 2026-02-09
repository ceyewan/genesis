# Genesis breaker：服务级熔断的设计与实现

Genesis `breaker` 是治理层（L3）的熔断组件，面向 gRPC 客户端场景，核心目的是在下游异常时快速失败、隔离故障，并在冷却后自动探测恢复。

---

## 0. 摘要

- 组件基于 `sony/gobreaker` 实现，按 `key` 维度维护独立熔断器。
- 默认集成点是 `UnaryClientInterceptor`，无侵入接入 gRPC 客户端。
- 熔断判定由 `FailureRatio + MinimumRequests` 控制。
- 状态机为 `closed -> open -> half_open -> closed/open`。
- 支持 `Fallback` 降级函数，在 `open` 状态下执行替代逻辑。
- 默认 Key 是 `cc.Target()`（服务级）；可通过 `WithKeyFunc` 自定义到方法级等粒度。

---

## 1. 组件定位与接口

`Breaker` 对外能力很聚焦：

- `Execute(ctx, key, fn)`：执行受熔断保护的调用
- `UnaryClientInterceptor(opts...)`：返回 gRPC 一元客户端拦截器
- `State(key)`：查询某个熔断键当前状态

定位上，它不是"通用限流器"或"重试器"，而是"失败隔离器"。常见组合是：`breaker + retry + timeout`，分别处理不同故障维度。

---

## 2. 配置模型与默认值

`Config` 字段：

- `MaxRequests`：半开状态允许通过的请求数（默认 1）
- `Interval`：闭合状态统计窗口（默认 0，依赖 gobreaker 默认行为）
- `Timeout`：打开状态持续时间（默认 60s）
- `FailureRatio`：触发熔断的失败率阈值（默认 0.6）
- `MinimumRequests`：最小采样请求数（默认 10）

触发条件可概括为：

1. `requests >= MinimumRequests`
2. `total_failures / requests >= FailureRatio`

满足后从 `closed` 进入 `open`。

---

## 3. 服务级熔断：按 Key 独立隔离

实现使用 `sync.Map` 维护 `map[key]*CircuitBreaker`。  
每个 `key` 拥有自己的统计、状态和迁移，不会互相污染。

默认 gRPC 拦截器 Key 为 `cc.Target()`，因此同一目标地址共享一套熔断状态。  
通过 `WithKeyFunc` 可切换粒度，例如：

- 方法级：`/pkg.Service/Method`
- 业务分片级：`target + tenant`

Key 设计决定隔离域大小，是 breaker 的第一优先级配置。

---

## 4. 执行路径与状态迁移

### 4.1 Execute 路径

1. 根据 `key` 获取或创建 breaker。
2. 调用 `breaker.Execute(fn)`。
3. 若返回 `open state` 错误：
   - 有 fallback：执行 fallback
   - 无 fallback：返回 `ErrOpenState`

### 4.2 状态机语义

- `closed`：请求正常通过并统计成功/失败。
- `open`：快速失败，不再调用下游。
- `half_open`：冷却后放少量探测请求（受 `MaxRequests` 控制）。
- 探测成功则回到 `closed`，失败回到 `open`。

---

## 5. gRPC 拦截器集成

`UnaryClientInterceptor` 的核心逻辑是把一次 gRPC 调用包装进 `Execute`：

- 由 `KeyFunc` 生成熔断键
- 在闭合/半开状态调用 `invoker`
- 在打开状态快速失败或走 fallback

默认行为是服务级熔断，适合大多数场景；当单个方法异常会拖累整服务时，建议改为方法级 Key。

---

## 6. 失败判定边界（非常关键）

当前实现未自定义 `IsSuccessful`，因此 `fn` 返回的任意非 `nil` 错误都会计入失败统计。  
这意味着：

- 网络错误会计入失败（合理）
- 下游业务错误（如参数错误）也会计入失败（需注意）

如果你的业务错误占比较高，可能导致"被业务错误误触发熔断"。常见做法是：

- 在进入 breaker 前，把可预期业务错误转换为成功返回（由上层单独处理）
- 或者按方法粒度拆分 Key，减少互相影响

---

## 7. 降级策略（Fallback）

可通过 `WithFallback` 注入降级函数，签名：

- `func(ctx, key, err) error`

行为：

- 仅在 breaker 打开时触发
- fallback 返回 `nil` 视为降级成功，`Execute` 返回 `nil, nil`
- fallback 返回错误则向上透传该错误

建议 fallback 明确记录"降级来源"和"降级结果"，便于排障与容量评估。

---

## 8. 实践建议

### 8.1 参数起点

- `FailureRatio`: `0.5~0.7`
- `MinimumRequests`: `10~50`
- `Timeout`: `10s~60s`
- `MaxRequests`: `1~5`

先保守，再基于真实错误分布做压测和回放调优。

### 8.2 Key 设计

- 服务整体不稳定：服务级 Key（默认）
- 个别接口不稳定：方法级 Key
- 多租户隔离需求：`service + tenant` 组合 Key

### 8.3 与重试协同

- 先短超时，再有限重试，最后 breaker 兜底
- 避免在 `open` 状态继续重试，造成无效流量放大

---

## 9. 总结

`breaker` 在 Genesis 中承担的是"故障隔离"职责：通过按键独立熔断、状态迁移与可选降级，避免单点故障放大为系统雪崩。  
真正用好它的关键不在 API，而在三件事：合理的 Key 粒度、准确的失败口径、以及与重试超时策略的协同配置。
