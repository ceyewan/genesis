# Genesis breaker：服务级熔断的设计与实现

Genesis `breaker` 是治理层（L3）的熔断组件，面向 gRPC 客户端场景，核心目的是在下游异常时快速失败、隔离故障，并在冷却后自动探测恢复。它遵循 Genesis 基础规范，使用 `clog` 记录状态迁移，通过 `WithLogger` 注入依赖，支持 nil 配置时使用默认值。

---

## 0 摘要

- `breaker` 基于 `sony/gobreaker` 实现，按 `key` 维度维护独立熔断器
- 默认集成点是 `UnaryClientInterceptor`，无侵入接入 gRPC 客户端
- 熔断判定由 `FailureRatio + MinimumRequests` 控制
- 状态机为 `closed → open → half_open → closed/open`
- 支持 `Fallback` 降级函数，在 `open` 状态下执行替代逻辑
- 默认 Key 是 `cc.Target()`（服务级）；可通过 `WithKeyFunc` 自定义到方法级等粒度

---

## 1 背景：熔断组件要解决的"真实问题"

在微服务调用中，当下游服务异常时，上游服务如果持续重试或等待，会造成：

- **资源耗尽**：线程/连接堆积，拖垮自身服务
- **雪崩效应**：故障向上游传播，影响整个调用链
- **恢复延迟**：下游恢复后，上游仍在积压请求，无法快速感知

熔断器的核心思路是"先断后探"：检测到异常时快速失败（断），冷却后放少量探测请求（探），确认恢复后再放开流量。

---

## 2 核心设计

### 2.1 接口抽象

`Breaker` 接口聚焦三类能力：

```go
type Breaker interface {
    Execute(ctx, key, fn) (result, error)    // 执行受熔断保护的调用
    UnaryClientInterceptor(opts...)          // 返回 gRPC 拦截器
    State(key) (State, error)                // 查询熔断状态
}
```

定位上，它不是"通用限流器"或"重试器"，而是"失败隔离器"。常见组合是 `breaker + retry + timeout`，分别处理不同故障维度。

### 2.2 配置模型

`Config` 字段与默认值：

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `MaxRequests` | uint32 | 1 | 半开状态允许通过的请求数 |
| `Interval` | duration | 0 | 闭合状态统计窗口（0 表示不清空） |
| `Timeout` | duration | 60s | 打开状态持续时间 |
| `FailureRatio` | float64 | 0.6 | 触发熔断的失败率阈值 |
| `MinimumRequests` | uint32 | 10 | 最小采样请求数 |

触发条件可概括为：

1. `requests >= MinimumRequests`
2. `total_failures / requests >= FailureRatio`

满足后从 `closed` 进入 `open`。

### 2.3 依赖注入

组件遵循 Genesis 规范：

```go
brk, _ := breaker.New(cfg,
    breaker.WithLogger(logger),    // 自动添加 namespace: "breaker"
    breaker.WithFallback(fn),      // 可选降级函数
)
```

- `nil cfg`：使用默认配置
- `nil logger`：使用 `clog.Discard()`
- Logger 通过 `WithNamespace("breaker")` 派生，避免手动添加字段

---

## 3 服务级熔断：按 Key 独立隔离

### 3.1 多实例管理

实现使用 `sync.Map` 维护 `map[key]*CircuitBreaker`。每个 `key` 拥有自己的统计、状态和迁移，不会互相污染。

```go
type circuitBreaker struct {
    cfg      *Config
    logger   clog.Logger
    fallback FallbackFunc
    breakers sync.Map  // map[string]*gobreaker.CircuitBreaker[interface{}]
}
```

### 3.2 Key 设计原则

默认 gRPC 拦截器 Key 为 `cc.Target()`（如 `etcd:///logic-service`），因此同一目标地址共享一套熔断状态。通过 `WithKeyFunc` 可切换粒度：

- **服务级**（默认）：`cc.Target()`
- **方法级**：`fullMethod`（如 `/pkg.Service/Method`）
- **业务分片级**：`target + tenant`

Key 设计决定隔离域大小，是 breaker 的第一优先级配置。

---

## 4 执行路径与状态迁移

### 4.1 Execute 路径

```
1. 检查 key 非空
2. 根据 key 获取或创建 breaker（sync.Map.LoadOrStore）
3. 调用 gobreaker.Execute(fn)
4. 若返回 open state 错误：
   - 有 fallback：执行 fallback
   - 无 fallback：返回 ErrOpenState
```

### 4.2 状态机语义

| 状态 | 行为 | 迁移条件 |
|------|------|----------|
| `closed` | 请求正常通过，统计成功/失败 | 失败率达到阈值 → open |
| `open` | 快速失败，不调用下游 | 冷却时间到期 → half_open |
| `half_open` | 放少量探测请求（MaxRequests 控制） | 探测成功 → closed，失败 → open |

### 4.3 状态迁移日志

状态变更通过 `OnStateChange` 回调记录日志：

```go
cb.logger.Info("circuit breaker state changed",
    clog.String("service", name),
    clog.String("from", stateToString(from)),
    clog.String("to", stateToString(to)))
```

使用 `Info` 级别，因为熔断打开是预期保护行为，而非异常。

---

## 5 底层实现原理：gobreaker 机制

### 5.1 核心结构

`gobreaker` 的核心是一个带状态机的并发安全计数器：

- `mutex`：保证并发安全，所有状态读写都需要加锁
- `state`：记录当前状态（closed/open/half_open）
- `counts`：滑动窗口计数器，记录成功/失败数
- `expiry`：状态过期时间，用于触发自动迁移

### 5.2 Generation 机制

gobreaker 有一个关键设计叫 `generation`，用于处理"时序错乱"问题。每次状态切换时 generation 会递增，请求完成时会检查 generation 是否匹配——如果不匹配说明是旧周期的请求，其结果不会被统计。

这避免了"熔断已触发，但慢请求的结果还在累加到新统计周期"的问题。

### 5.3 滑动窗口

当配置了 `Interval` 时，gobreaker 使用环形缓冲区实现滑动窗口统计。时间窗口被分成多个桶，每个桶独立计数，超出窗口的旧数据自动丢弃。这样可以避免某个时刻的突发流量影响整体判断。

---

## 6 gRPC 拦截器集成

### 6.1 拦截器逻辑

`UnaryClientInterceptor` 的核心逻辑是把一次 gRPC 调用包装进 `Execute`：

```go
func (cb *circuitBreaker) UnaryClientInterceptor(opts ...InterceptorOption) grpc.UnaryClientInterceptor {
    cfg := &interceptorConfig{keyFunc: defaultKeyFunc}
    for _, opt := range opts { opt(cfg) }

    return func(ctx, method, req, reply, cc, invoker, opts...) error {
        key := cfg.keyFunc(ctx, method, cc)
        _, err := cb.Execute(ctx, key, func() (interface{}, error) {
            return nil, invoker(ctx, method, req, reply, cc, opts...)
        })
        return err
    }
}
```

### 6.2 默认 Key 策略

```go
func defaultKeyFunc(ctx, fullMethod, cc) string {
    return cc.Target()  // 如 "etcd:///logic-service"
}
```

默认行为是服务级熔断，适合大多数场景；当单个方法异常会拖累整服务时，建议改为方法级 Key。

---

## 7 失败判定边界（非常关键）

### 7.1 当前实现

未自定义 `IsSuccessful`，因此 `fn` 返回的任意非 `nil` 错误都会计入失败统计。这意味着：

- 网络错误会计入失败（合理）
- 下游业务错误（如参数错误）也会计入失败（需注意）

### 7.2 业务误触发风险

如果你的业务错误占比较高，可能导致"被业务错误误触发熔断"。常见做法是：

- 在进入 breaker 前，把可预期业务错误转换为成功返回（由上层单独处理）
- 或者按方法粒度拆分 Key，减少互相影响

---

## 8 降级策略（Fallback）

### 8.1 Fallback 签名

```go
type FallbackFunc func(ctx context.Context, key string, err error) error
```

### 8.2 行为语义

- 仅在 breaker 打开时触发
- fallback 返回 `nil` 视为降级成功，`Execute` 返回 `nil, nil`
- fallback 返回错误则向上透传该错误

### 8.3 实践建议

建议 fallback 明确记录"降级来源"和"降级结果"，便于排障与容量评估：

```go
breaker.WithFallback(func(ctx, key, err) error {
    logger.Info("circuit breaker open, using fallback",
        clog.String("key", key),
        clog.String("strategy", "cache"))
    return getCachedResult(ctx, key)
})
```

---

## 9 实践建议

### 9.1 参数起点

| 参数 | 推荐范围 | 说明 |
|------|----------|------|
| `FailureRatio` | 0.5 ~ 0.7 | 过高反应慢，过低误触发 |
| `MinimumRequests` | 10 ~ 50 | 避免小样本误判 |
| `Timeout` | 10s ~ 60s | 冷却时间，给下游恢复窗口 |
| `MaxRequests` | 1 ~ 5 | 探探测请求数 |

先保守，再基于真实错误分布做压测和回放调优。

### 9.2 Key 设计

| 场景 | Key 策略 | 示例 |
|------|----------|------|
| 服务整体不稳定 | 服务级 Key | `cc.Target()`（默认） |
| 个别接口不稳定 | 方法级 Key | `/pkg.Service/Method` |
| 多租户隔离需求 | 组合 Key | `target + tenant` |

### 9.3 与重试协同

- 先短超时，再有限重试，最后 breaker 兜底
- 避免在 `open` 状态继续重试，造成无效流量放大

推荐顺序：`timeout → retry → breaker`

---

## 10 总结

`breaker` 在 Genesis 中承担的是"故障隔离"职责：通过按键独立熔断、状态迁移与可选降级，避免单点故障放大为系统雪崩。

真正用好它的关键不在 API，而在三件事：

1. **合理的 Key 粒度**：决定隔离域大小
2. **准确的失败口径**：区分网络错误与业务错误
3. **与重试超时策略的协同配置**：避免无效重试放大流量

它与 `retry`、`timeout`、`ratelimit` 等组件组合使用，共同构成治理层的流量控制能力。
