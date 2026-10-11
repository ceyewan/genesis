# 组件选型与边界

Genesis 提供组件，不接管应用生命周期。下面的选择基于当前行为，而不是“统一 API
意味着统一后端语义”。初始化与退出见[生产接入](./production.md)。

| 问题 | 优先选择 | 不应假设 |
| :--- | :--- | :--- |
| 进程内热点查询 | cache.Local | 其他实例更新时自动失效 |
| 跨实例共享缓存 | cache.Distributed | 与数据库事务强一致 |
| 可容忍短期旧值的热点查询 | cache.Multi | 本地 TTL 不超过远端剩余 TTL |
| 同进程防缓存击穿 | 应用层 singleflight | 分布式互斥或持久去重 |
| 共享缓存跨实例重建 | dlock + Distributed，锁后再检查 | Local 加一把分布式锁即可跨实例只回源一次 |
| 短期跨实例互斥 | Redis dlock | 锁续期即可阻止暂停后的旧持有者写入 |
| 基于 lease 的互斥 | Etcd dlock | 秒级 lease 与 Redis 毫秒 TTL 完全等价 |
| 单实例开发/请求去重 | idem.Memory | 重启、多实例后继续去重 |
| 跨实例结果复用 | idem.Redis | 业务副作用与结果记录组成同一个事务 |
| 持久异步任务 | JetStream durable / Redis consumer group | exactly-once、默认自动 DLQ |
| 趋势递增整数 ID | idgen.Generator + 唯一 WorkerID | 两个同 WorkerID 生成器共享序列状态 |
| 每业务键的序列 | idgen.Sequencer | TTL 到期后仍永久递增 |
| 出站系统故障保护 | breaker | 代替调用超时、重试预算或实例摘除 |
| 服务注册与发现 | registry | 完整服务网格或瞬时一致的实例视图 |
| 本地签发 JWT | auth | OAuth/OIDC、撤销或 refresh token 重放检测 |

## 组合而不是叠加

- 缓存是优化层，数据库是权威状态；权限、库存不能仅由陈旧本地值决定。
- 分布式锁应立即监听 Lost；不可逆写入仍需要数据库版本/CAS fencing。
- 幂等组件保留成功结果，不保证副作用只发生一次。数据库唯一约束是最终防线。
- HTTP 幂等放在认证之后，identity scope 来自已认证上下文，不能信任任意 Header。
- Redis 广播没有服务端确认/重投保证；需要持久消费进度时选择 consumer group。
- 多层重试会乘法放大请求。设置整体 deadline，区分永久错误和暂时错误。

具体 TTL、确认、错误分类与所有权见各组件 README、GoDoc，以及
[API 决策](./v1-api-decisions.md)。
