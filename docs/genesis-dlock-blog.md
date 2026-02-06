# Genesis dlock：分布式锁的设计与实现

Genesis `dlock` 是业务层（L2）的分布式锁组件，提供统一 `Locker` 接口，支持 Redis 与 Etcd 两种后端。它的核心目标是：在保证易用性的前提下，尽可能降低“误删锁”“锁丢失不可感知”“并发竞态”这些常见风险。

---

## 0. 摘要

- `dlock` 统一暴露 `Lock / TryLock / Unlock / Close` 四个核心方法。
- 配置驱动选择后端：`driver=redis` 或 `driver=etcd`。
- Redis 方案基于 `SET NX PX` + token 校验 Lua + watchdog 自动续期。
- Etcd 方案基于 `concurrency.Mutex` + Session KeepAlive 自动续租。
- 组件内维护本地持锁表，防止同一 `Locker` 实例重复持有同一 key。
- `WithTTL` 支持运行时覆盖默认 TTL；`Lock` 支持重试，`TryLock` 非阻塞。
- 组件遵循 Genesis 规范：显式依赖注入、`clog` 日志、`xerrors` 错误语义。

---

## 1. 组件定位：统一接口，后端可切换

对外接口非常克制：

- `Lock(ctx, key, opts...) error`：阻塞式获取锁
- `TryLock(ctx, key, opts...) (bool, error)`：非阻塞尝试
- `Unlock(ctx, key) error`：释放锁
- `Close() error`：释放组件内部资源

初始化使用配置 + Option 注入：

- Redis：`dlock.New(cfg, dlock.WithRedisConnector(redisConn), ...)`
- Etcd：`dlock.New(cfg, dlock.WithEtcdConnector(etcdConn), ...)`

这种设计和 Genesis 其他组件一致：能力通过配置选择，依赖通过构造注入，资源所有权清晰。

---

## 2. 配置模型与默认值策略

`Config` 核心字段：

- `Driver`：后端类型（`redis` / `etcd`）
- `Prefix`：锁键前缀
- `DefaultTTL`：默认过期时间（默认 `10s`）
- `RetryInterval`：`Lock` 重试间隔（默认 `100ms`）

默认值策略：

- `DefaultTTL <= 0` 时回落到 `10s`
- `RetryInterval <= 0` 时回落到 `100ms`

这保证了“最小可用配置”，同时允许按场景精细调参。

---

## 3. Redis 实现：SETNX + Token + Watchdog

### 3.1 获取锁流程

Redis 路径的关键步骤：

1. 本地检查：若同一 `Locker` 已持有该 key，直接返回 `ErrLockAlreadyHeld`。
2. 生成随机 token（16 字节随机值，hex 编码）。
3. 执行 `SET key token NX PX(ttl)` 尝试抢锁。
4. 成功后写入本地持锁表，并启动 watchdog 续期协程。

本地“先查后写 + 二次检查”的处理能避免并发竞态下重复持有。

### 3.2 安全释放（防误删）

释放锁不是直接 `DEL`，而是执行 Lua 脚本：

- 只有当 `GET key == token` 时才 `DEL key`
- 否则返回 0，报告 `ErrOwnershipLost`

这确保了“只有锁拥有者才能删锁”，避免误删其他实例的锁。

### 3.3 自动续期（Watchdog）

watchdog 策略：

- 续期间隔为 `ttl/3`（最小 1 秒）
- 每次用 Lua 校验 token 后再 `PEXPIRE`
- 续期失败或 token 不匹配，协程退出并记录日志

这可以覆盖“业务执行时间超出初始 TTL”的场景，减少锁意外过期导致的并发问题。

---

## 4. Etcd 实现：Mutex + Session 租约

### 4.1 加锁机制

Etcd 路径基于 `clientv3/concurrency`：

- 使用 `concurrency.NewMutex(session, key)`
- `Lock` 走 `mutex.Lock(ctx)`
- `TryLock` 走 `mutex.TryLock(ctx)`，占用时返回 `false, nil`

### 4.2 TTL 与 Session 策略

- 默认情况下复用组件级 session（默认 TTL）
- 如果调用 `WithTTL` 且与默认值不同，会为这次锁创建独立 session
- 该 session 由 etcd keepalive 自动续租
- 解锁后若是独立 session，会主动关闭

这个设计兼顾了“默认路径低开销”和“单次锁可定制 TTL”。

### 4.3 生命周期

- `Unlock`：先释放 mutex，再清理本地持锁记录
- `Close`：关闭默认 session

相比 Redis，Etcd 的所有权语义更多由租约与 mutex 原语保障。

---

## 5. Lock、TryLock 与上下文语义

### 5.1 `Lock`

- 阻塞式重试获取锁
- 每次重试间隔由 `RetryInterval` 控制
- 上下文取消/超时会及时返回 `context.Canceled` 或 `context.DeadlineExceeded`

### 5.2 `TryLock`

- 单次尝试，不重试
- 成功返回 `true, nil`
- 被占用返回 `false, nil`
- 真正错误返回 `false, err`

这让调用方可以清晰区分“竞争失败”和“系统异常”。

---

## 6. 错误模型与可观测性

### 6.1 标准错误

组件定义了稳定错误语义：

- `ErrConfigNil`：配置为空
- `ErrConnectorNil`：连接器为空
- `ErrLockNotHeld`：释放未持有的锁
- `ErrLockAlreadyHeld`：同一实例重复持锁
- `ErrOwnershipLost`：释放时发现所有权丢失

### 6.2 日志

通过 `WithLogger` 注入后，组件会带上 `component=dlock`。关键事件包括：

- lock acquired
- lock released
- watchdog renew failed / lost ownership

对于线上排障，建议把业务 key、request id 一并打到上层日志上下文中。

---

## 7. 设计边界与注意事项

- `dlock` 是“互斥原语”，不是事务协调器；业务幂等仍需自己保证。
- Redis watchdog 续期失败后会退出，业务侧应关注日志和异常路径。
- Etcd 的 TTL 是秒级（`WithTTL` 最终按秒传入 session），不适合亚秒精度控制。
- `Prefix` 建议按应用或业务域隔离，避免跨服务 key 冲突。
- 长耗时任务建议显式设置 `WithTTL`，并结合业务超时控制。

---

## 8. 实践建议

### 8.1 场景选型

- 已有 Redis 基础设施且追求接入简单：优先 Redis 驱动。
- 控制面、强一致协调场景：优先 Etcd 驱动。

### 8.2 推荐参数起点

- `DefaultTTL`: 10s~30s
- `RetryInterval`: 50ms~200ms
- 单次关键任务可用 `WithTTL` 提升容错余量

### 8.3 典型模式

任务竞选：

- 定时任务实例先 `TryLock("job:xxx")`
- 成功者执行任务，结束后 `Unlock`

临界资源保护：

- 进入关键区前 `Lock`
- 使用 `defer Unlock`
- 结合上下文超时防止无限等待

---

## 9. 总结

`dlock` 的价值不在“隐藏后端差异”，而在于把分布式锁最容易出错的几件事做成默认正确：

- 持锁所有权校验（防误删）
- 自动续期（降低长任务锁过期风险）
- 本地重复持锁防护（降低应用内竞态）
- 统一错误语义与上下文取消行为

在此基础上，业务可以按场景选择 Redis 或 Etcd，并通过 `TTL/重试/日志` 做工程化调优。
