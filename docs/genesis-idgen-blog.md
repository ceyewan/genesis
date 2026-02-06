# Genesis idgen：序列号与雪花 ID 生成的设计与实现

Genesis `idgen` 是业务层（L2）的 ID 生成组件，覆盖三类常见需求：本地高性能分布式 ID（Snowflake）、分布式递增序列号（Sequencer）、以及集群实例唯一 ID 分配（Allocator）。本文重点介绍这三部分的设计与边界。

---

## 0. 摘要

- `Snowflake` 适合生成高吞吐、趋势递增、分布式唯一的 `int64` ID。
- `Sequencer` 适合“按业务键递增”的序列场景（消息序号、订单流水号等）。
- `Allocator` 用于为每个服务实例分配唯一 WorkerID，并通过租约机制保活。
- `UUID()` 提供 UUID v7，适合无需 WorkerID 管理的通用唯一标识场景。
- Snowflake 使用无锁 CAS 状态机，内置时钟回拨分级处理策略。
- Sequencer 基于 Redis Lua 脚本原子执行 `INCRBY + 上限检查 + TTL`，支持批量分配。
- Allocator 支持 Redis/Etcd 两种后端，分配后可持续续约并在退出时释放。
- 组件遵循 Genesis 规范：`clog` 日志、`metrics` 指标、`xerrors` 错误语义、显式依赖注入。

---

## 1. 背景：为什么需要两类 ID 能力

ID 生成在业务中通常分成两类问题：

- 全局唯一主键：要求高并发、低延迟、跨节点唯一，通常不要求“严格连续”。
- 业务局部序号：要求在同一业务维度内单调递增（如会话内消息 seq）。

Snowflake 解决第一类，Sequencer 解决第二类。二者并非替代关系，而是互补关系。

---

## 2. 组件总览与接口形态

`idgen` 对外核心接口：

- `Generator`（雪花）：`Next()`、`NextString()`
- `Sequencer`（序列号）：`Next/NextBatch/Set/SetIfNotExists`

统一初始化模式：

- `NewGenerator(cfg, opts...)`
- `NewSequencer(cfg, opts...)`

这种模式保证了配置校验、依赖注入和可观测性入口的一致性。

---

## 3. Snowflake 设计：高性能本地唯一 ID

### 3.1 位结构与配置约束

当前实现使用标准 64 位雪花布局：

- 41 bit 时间戳（毫秒）
- 5 bit 数据中心 ID（`datacenter_id`）
- 5 bit 工作节点 ID（`worker_id`）
- 12 bit 毫秒内序列号

因此有两个关键约束：

- `datacenter_id` 范围 `[0, 31]`
- 当 `datacenter_id > 0` 时，`worker_id` 只能是 `[0, 31]`
- 当 `datacenter_id == 0` 时，`worker_id` 可扩展到 `[0, 1023]`

这对应了“5+5”和“10”两种 worker 编码模式。

### 3.2 并发模型：CAS 状态机

实现中把 `lastTime` 与 `sequence` 打包到一个 `uint64` 原子状态：

- 高位存 `lastTime`（毫秒）
- 低 12 位存 `sequence`

每次生成 ID 的过程：

1. 读取旧状态。
2. 根据当前时间计算新序列号。
3. 通过 `CompareAndSwap` 更新状态。
4. CAS 成功后拼出最终 ID。

优点是：

- 热路径无互斥锁，竞争下仍有较好吞吐。
- 状态更新具备原子性，天然避免重复序列。

### 3.3 时钟回拨保护策略

Snowflake 的核心风险是系统时钟回拨。当前实现做了三级处理：

- `<= 5ms`：微小回拨，尽量复用 `lastTime`（若序列没满）。
- `5ms ~ 1s`：等待时钟追平后继续。
- `> 1s`：直接失败，返回 `ErrClockBackwards`。

这保证了“可容忍微抖动，但拒绝大幅回拨”的安全边界。

### 3.4 序列号溢出行为

同一毫秒内最多 4096（`2^12`）个序列。当序列耗尽时：

- 生成器睡眠 1ms，等待进入下一毫秒再继续。

因此 Snowflake 的理论上限是“单实例每毫秒约 4096 个 ID”。

### 3.5 输出与可观测性

- `Next()` 返回 `int64`；内部失败时返回 `-1`。
- `NextString()` 返回字符串；内部失败时返回空串。
- 指标 `idgen_snowflake_generated_total` 统计生成次数。

这要求调用方在关键路径上对异常返回值做显式校验。

---

## 4. Sequencer 设计：按键递增的分布式序列号

### 4.1 适用场景

Sequencer 更适合这种需求：

- “同一个 key 内必须递增”
- “不同 key 之间互不影响”
- “可按步长增长，可选最大值循环，可选 TTL 自动过期”

典型例子：IM 会话消息序号、业务分组流水号。

### 4.2 配置与实现边界

`SequencerConfig` 关键字段：

- `driver`（默认 `redis`）
- `key_prefix`
- `step`（默认 1）
- `max_value`（0 表示不限制）
- `ttl`（秒，0 表示不过期）

需要注意：当前代码实现仅支持 `redis` 驱动，传其他 driver 会直接报错。

### 4.3 单条生成：Lua 原子脚本

`Next(ctx, key)` 使用 Lua 一次性执行：

1. `INCRBY key step`
2. 若超过 `max_value`（且 max>0），重置为 `step`
3. 若 `ttl>0`，执行 `EXPIRE`
4. 返回当前值

因为全部在 Redis 脚本中完成，天然原子，不会出现并发竞态导致的重复序号。

### 4.4 批量生成：区间预分配

`NextBatch(ctx, key, count)` 的策略是：

1. 先一次性 `INCRBY step*count` 得到批次末尾值 `endSeq`
2. 在本地倒推出整段序列区间

例如 `step=5, count=3`，返回 `[5, 10, 15]`。  
这种方式只需一次 Redis 往返，适合批处理吞吐优化。

### 4.5 序列初始化能力

- `Set`：强制覆盖当前值（慎用）
- `SetIfNotExists`：仅在 key 不存在时初始化

`SetIfNotExists` 特别适合历史数据迁移场景：只做“首次基线写入”，避免并发覆盖。

### 4.6 语义注意点

- `max_value` 触发后会回绕，序列不再全局严格递增。
- `ttl` 每次 `Next/NextBatch` 都会刷新（当 `ttl > 0`）。
- 不同 key 的序列完全独立。

---

## 5. Allocator 设计：实例唯一 WorkerID 分配与保活

### 5.1 解决的问题

Snowflake 需要稳定且不冲突的 `worker_id`。  
在 Kubernetes Deployment 或弹性扩缩容场景，手工配置 WorkerID 很容易冲突，Allocator 就是为此提供自动分配能力。

### 5.2 统一接口

Allocator 对外暴露三个方法：

- `Allocate(ctx)`：分配唯一 WorkerID
- `KeepAlive(ctx)`：保活租约，返回错误通道
- `Stop()`：停止保活并释放分配到的 ID

标准用法是：

1. 启动时先 `Allocate`
2. 后台 goroutine 运行 `KeepAlive`
3. `KeepAlive` 返回错误时，触发进程退出或摘流量
4. 优雅退出时调用 `Stop`

### 5.3 Redis 后端实现原理

Redis 分配逻辑：

- 以随机 `offset` 为起点，环形扫描 `[0, max_id)`。
- 使用 Lua 脚本原子执行 `SET key value NX EX ttl` 抢占 ID。
- 成功后记录 `redisKey` 与 `instanceID`。

Redis 保活逻辑：

- 周期为 `TTL/3` 秒。
- 定时执行 `EXPIRE redisKey ttl` 续租。
- 续租失败时通过错误通道上报。

释放逻辑：

- `Stop()` 删除对应 key，归还 WorkerID。

### 5.4 Etcd 后端实现原理

Etcd 分配逻辑：

- 先创建 Lease（TTL 秒）。
- 以随机 offset 环形遍历候选 ID。
- 每个候选 ID 使用 Txn CAS 抢占：`ModRevision(key)==0` 才写入并绑定 lease。

Etcd 保活逻辑：

- 调用原生 `KeepAlive(ctx, leaseID)` 获取续约流。
- 若续约通道关闭或收到 nil，判定 lease 失效，返回 `ErrLeaseExpired`。

释放逻辑：

- `Stop()` 调用 `Revoke(leaseID)`，Etcd 自动删除关联 key。

### 5.5 配置关键点

`AllocatorConfig` 关键字段：

- `driver`: `redis | etcd`（默认 `redis`）
- `key_prefix`: 默认 `genesis:idgen:worker`
- `max_id`: 分配区间大小，范围 `(0, 1024]`
- `ttl`: 租约秒数，必须大于 0

工程建议：

- `max_id` 与 Snowflake WorkerID 位宽一致设计（通常不超过 1024）。
- `ttl` 不宜过短，避免网络抖动导致误判失效。

---

## 6. Snowflake、Sequencer、Allocator 的选型建议

- 需要全局唯一主键：优先 Snowflake。
- 需要按业务维度严格递增：优先 Sequencer。
- 需要自动管理 WorkerID：用 Allocator 给 Snowflake 提供 WorkerID。
- 需要跨语言、无中心依赖、低接入成本的唯一 ID：可直接用 UUID v7。
- 需要可读的有序字符串主键：Snowflake + `NextString()`。
- 需要“按会话/租户分桶”序列：Sequencer + 动态 key。

常见组合方式：

- 数据库主键用 Snowflake。
- 业务展示号/消息号用 Sequencer。
- 服务实例启动先 Allocator 分配 WorkerID，再创建 Snowflake Generator。

---

## 7. UUID（简单补充）

`idgen.UUID()` 返回 UUID v7 字符串，特点是“带时间排序特性 + 全局唯一概率高 + 使用简单”。

适用场景：

- 不希望维护 WorkerID/租约体系；
- 需要跨系统直接传递字符串 ID；
- 以工程易用性优先，不追求 Snowflake 的数值结构可解析性。

与 Snowflake 的主要区别：

- UUID v7 不依赖实例编号分配；
- Snowflake 更易做数值索引与分片路由；
- UUID 更通用，Snowflake 更偏基础设施治理场景。

---

## 8. 可靠性与治理实践

- WorkerID 必须稳定分配，避免多实例冲突（可配合 Allocator）。
- 监听 `KeepAlive` 错误通道，失效后及时摘流量或退出进程。
- 对 Snowflake 返回的 `-1/""` 做告警与降级处理。
- Sequencer 使用前规划好 `key_prefix`，隔离环境与业务域。
- 对需要“绝不回绕”的业务，不要启用 `max_value`。
- 指标至少监控：
  - `idgen_snowflake_generated_total`
  - `idgen_sequence_generated_total`

---

## 9. 设计取舍总结

`idgen` 在设计上做了明确分工：

- Snowflake 追求本地极致性能和分布式唯一性；
- Sequencer 追求分布式原子递增和业务可控性；
- Allocator 追求集群 WorkerID 自动治理与故障可感知；
- 三者统一在同一套组件规范下，便于在微服务中标准化落地。

这使 `idgen` 能同时覆盖“主键生成”、“业务序号”和“实例 ID 治理”三条关键链路。
