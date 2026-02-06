# Genesis idem：分布式幂等的设计与实现

Genesis `idem` 是业务层（L2）的幂等组件，用来解决“同一请求/消息被重复提交”带来的副作用问题。它提供统一接口覆盖三类入口：手动调用、Gin 中间件、gRPC 一元拦截器。

---

## 0. 摘要

- `idem` 支持两种驱动：`redis`（分布式）与 `memory`（单机）。
- 核心流程是“先读结果，再抢锁，成功执行后缓存结果并释放锁”。
- `Execute` 对并发同 key 采用“等待结果或抢到锁后执行”的策略，重复请求通常返回同一结果。
- `Consume` 面向消息去重，更强调快速失败：并发冲突时返回 `ErrConcurrentRequest`。
- 内置锁续期（Refresh）机制，避免长耗时任务期间锁过期。
- Gin 中间件默认只缓存 2xx 响应；gRPC 拦截器默认只缓存成功的 `proto.Message`。

---

## 1. 组件定位：把幂等从“业务约定”变成“标准能力”

`Idempotency` 对外提供四个入口：

- `Execute(ctx, key, fn)`：通用幂等执行，返回结果
- `Consume(ctx, key, ttl, fn)`：消息消费去重，只关心“是否执行”
- `GinMiddleware(opts...)`：HTTP 请求幂等
- `UnaryServerInterceptor(opts...)`：gRPC 一元调用幂等

统一目标：

- 第一次请求执行真实逻辑
- 重复请求不重复执行副作用逻辑
- 在可接受窗口内返回一致结果

---

## 2. 配置模型与默认值

`Config` 关键字段：

- `Driver`: `redis | memory`（默认 `redis`）
- `Prefix`: 键前缀（默认 `idem:`）
- `DefaultTTL`: 结果缓存 TTL（默认 24h）
- `LockTTL`: 执行锁 TTL（默认 30s）
- `WaitTimeout`: 等待结果上限（默认 0，跟随 `ctx`）
- `WaitInterval`: 轮询起始间隔（默认 50ms）

实现上有两个重要默认行为：

- 轮询等待采用指数退避，最大到 `500ms`。
- 若实现支持 `Refresh`，执行期间会周期性刷新锁 TTL。

---

## 3. 核心算法：结果优先 + 锁保护

### 3.1 `Execute` 的状态机

对同一 `key`，`Execute` 的主流程是：

1. 读结果缓存：命中直接返回。
2. 未命中则尝试加锁：
   - 抢锁成功：执行 `fn`。
   - 抢锁失败：等待后重试“读结果/抢锁”。
3. `fn` 成功：序列化结果并落库，标记完成。
4. `fn` 失败：不缓存错误，释放锁并返回错误。

这意味着：在并发压力下，通常只有一个请求真正执行业务逻辑，其它请求等到结果后直接复用。

### 3.2 `Consume` 的差异化语义

`Consume` 不是返回业务结果，而是返回 `executed bool`：

- 已有处理标记：`executed=false, nil`
- 抢锁成功并执行成功：`executed=true, nil`
- 抢锁失败（并发冲突）：`executed=false, ErrConcurrentRequest`

相比 `Execute`，`Consume` 没有等待-复读结果的闭环，更适合“消息幂等去重”这种快速判定场景。

---

## 4. 存储抽象与双驱动实现

### 4.1 Store 抽象

`Store` 定义了四个操作：

- `Lock`
- `Unlock`
- `SetResult`
- `GetResult`

可选扩展 `RefreshableStore`：

- `Refresh(ctx, key, token, ttl)` 用于续期

这使上层幂等逻辑可以复用，不和具体后端耦合。

### 4.2 Redis 驱动（分布式）

Redis 使用两类 key：

- `prefix + key + ":lock"`：执行锁
- `prefix + key + ":result"`：执行结果

关键机制：

- `Lock`: `SET NX` + 随机 token
- `Unlock`: Lua 校验 token 后删除（防误删）
- `SetResult`: Lua 原子写结果并按 token 删除锁
- `Refresh`: Lua 校验 token 后 `PEXPIRE`

适用于多实例部署的真实生产场景。

### 4.3 Memory 驱动（单机）

Memory 使用进程内 map + 过期时间：

- 支持锁和结果 TTL
- 支持 token 校验释放和刷新
- 仅在单进程内有效，不能跨实例保证幂等

适用于本地开发、单测或单节点任务。

---

## 5. 锁续期与长耗时任务

当 `Store` 支持 `Refresh` 且 `LockTTL > 0` 时，组件会在执行期间启动后台续期：

- 续期间隔约为 `LockTTL/2`（最小 500ms）
- 续期失败只记录告警日志，不会立即中断业务函数

这能降低“执行时间超过 `LockTTL` 导致锁提前过期”的风险，但并不等价于事务保证。关键副作用操作仍应保持幂等。

---

## 6. Gin 与 gRPC：缓存边界要看清

### 6.1 Gin 中间件

默认请求头键：`X-Idempotency-Key`（可通过 `WithHeaderKey` 覆盖）。

缓存策略：

- 有 key 才启用幂等
- 仅缓存 2xx 响应（状态码、响应头、响应体）
- 5xx/4xx 默认不缓存，后续相同 key 会再次执行 handler

这符合大多数 API 的语义：只缓存“成功结果”，避免把临时错误固化。

### 6.2 gRPC 一元拦截器

默认 metadata 键：`x-idem-key`（可 `WithMetadataKey` 覆盖）。

缓存策略：

- 仅对成功响应尝试缓存
- 仅缓存 `proto.Message` 类型响应
- 非 proto 返回值会跳过缓存

这是因为实现使用 `Any` 封装/反序列化 protobuf 消息，保证跨调用一致恢复。

---

## 7. 错误语义与行为约定

关键错误：

- `ErrConfigNil`: 配置为空
- `ErrKeyEmpty`: 幂等键为空
- `ErrConcurrentRequest`: 并发请求冲突（常见于 `Consume`）
- `ErrResultNotFound`: 内部未命中结果

行为约定：

- 业务函数返回错误时，不写入结果缓存。
- 成功结果按 `DefaultTTL` 或 `Consume` 的传入 TTL 存储。
- `WaitTimeout` 到期时，`Execute` 会按上下文超时返回错误。

---

## 8. 实践建议

### 8.1 幂等键设计

- 保证全局唯一且稳定：如 `order:create:{request_id}`、`msg:{topic}:{msg_id}`
- 键中包含业务域前缀，避免不同操作冲突

### 8.2 TTL 选择

- `DefaultTTL` 应覆盖“客户端可能重试的最长窗口”
- `LockTTL` 要大于常见执行时间，并为慢请求预留余量

### 8.3 两种常见落地模式

HTTP/RPC 幂等：

- 客户端生成并透传幂等键
- 服务端用 `Execute` 或中间件/拦截器托管

MQ 消费去重：

- `Consume` key 建议绑定消息唯一 ID
- `executed=false` 可视为“已处理或并发处理中”，按业务策略跳过

---

## 9. 总结

`idem` 的核心价值不是“缓存一次结果”这么简单，而是把并发竞争、结果复用、锁安全释放、长任务续期这些细节封装成统一语义。  
在分布式场景下建议优先 Redis 驱动；Memory 驱动用于单机与测试。只要键设计和 TTL 合理，`idem` 能显著降低重复请求造成的数据不一致风险。
