# Genesis registry：服务注册发现与 gRPC 解析器核心原理

Genesis `registry` 是治理层（L3）的服务注册发现组件，基于 Etcd 提供实例注册、服务发现、变更订阅，并内置 gRPC resolver，让客户端可以直接使用 `etcd:///service-name` 做负载均衡。

本文重点讲核心原理：注册模型、Watch 机制、resolver 解析与状态更新。

---

## 0. 摘要

- 服务实例以层级 Key 存在 Etcd：`<namespace>/<service>/<instance_id>`。
- 注册通过 Lease 绑定实例生命周期，租约失效即自动下线。
- 发现支持“拉取（GetService）+ 推送（Watch）”两种模式。
- gRPC resolver 使用本地缓存做增量更新，避免每次事件全量拉取。
- 进程内采用单 active registry 约束，保证 resolver 全局行为一致。

---

## 1. 整体模型：注册中心中的三个对象

`registry` 可以抽象成三个核心对象：

- `ServiceInstance`：实例描述（ID、Name、Endpoints、Metadata）
- `Lease`：实例存活时间（TTL）
- `Resolver Cache`：客户端侧可用地址集合

对应三条链路：

1. 服务端注册时写入实例并绑定 Lease。
2. 客户端监听 Etcd 变化，转成 `PUT/DELETE` 事件。
3. resolver 把事件增量应用到本地缓存，再推送到 gRPC 连接状态。

---

## 2. 注册原理：Lease 驱动的“有生命”实例

### 2.1 Key 组织

实例写入路径：

`<namespace>/<service_name>/<instance_id>`

值是 `ServiceInstance` 的 JSON。

这种结构带来两个直接收益：

- 按服务名做前缀扫描即可发现全部实例。
- `instance_id` 天然是最小删除单元，便于精确下线。

### 2.2 Register 的原子步骤

`Register(ctx, service, ttl)` 的关键步骤：

1. 校验输入与 TTL（`ttl==0` 使用默认值）。
2. `Grant` 创建 Lease。
3. `Put(key, value, WithLease(leaseID))` 绑定实例和租约。
4. 启动 `KeepAlive` 协程持续续约。
5. 把该实例写入本地 `keepAlives` 表，便于后续注销和关闭。

只要 Lease 存活，实例就视为在线；Lease 失效后 key 会被 Etcd 自动删除。

### 2.3 Deregister 与优雅下线

`Deregister` 的核心是 `Revoke(leaseID)`：

- 撤销租约会自动删除 lease 关联 key；
- 无需额外执行 `Delete(key)`；
- 能确保“实例状态”和“租约状态”一致收敛。

---

## 3. 发现原理：拉取与订阅并存

### 3.1 GetService（拉取式）

`GetService(serviceName)` 通过前缀查询：

- `Get(prefix, WithPrefix())`
- 逐条反序列化为 `ServiceInstance`

适合初始化、兜底刷新、调试排查。

### 3.2 Watch（订阅式）

`Watch(serviceName)` 返回事件流 `chan ServiceEvent`，事件类型：

- `PUT`：实例新增/更新
- `DELETE`：实例下线

实现要点：

- 记录 `lastRev`，重连时用 `WithRev(lastRev+1)` 续接，降低事件丢失风险。
- 若遇到 compaction（历史 revision 被压缩），先做一次全量 `Get` 同步 revision，再继续 watch。
- watch channel 异常关闭时自动按 `RetryInterval` 重连。

这是一种“增量优先 + 断点续传 + 异常回补”的事件模型。

---

## 4. Resolver 原理：把 Etcd 事件变成 gRPC 地址集

### 4.1 入口：`etcd:///service-name`

`registry` 在包初始化时注册 resolver builder，scheme 固定为 `etcd`。  
当 gRPC Dial 目标是 `etcd:///user-service` 时，会进入 `Build()`。

### 4.2 全局默认 registry 机制

gRPC 的 resolver.Builder 接口不允许直接注入业务参数，所以组件使用“进程内全局默认 registry”：

- `New()` 成功后设置 `defaultRegistry`
- 若已有未关闭实例，再创建会返回 `ErrRegistryAlreadyInitialized`
- `Close()` 后清理默认实例，允许重新初始化

这就是“单 active registry 约束”的根本原因。

### 4.3 resolver 启动流程

`Build()` 创建 `etcdResolver` 后执行：

1. 调用 `Watch(service)` 建立事件订阅。
2. 先做一次 `initializeCache()` 全量拉取。
3. 持续消费事件并增量更新 `localCache`。
4. 每次变化调用 `cc.UpdateState()` 推送最新地址集。

其中 `localCache` key 形如 `instanceID_addr`，用于支持同一实例多个 endpoint。

### 4.4 地址解析规则

endpoint 解析支持：

- `grpc://host:port`
- `http://host:port`
- `https://host:port`
- `host:port`

解析后统一写入 gRPC `resolver.Address{Addr: host:port}`。

### 4.5 空地址保护策略

当缓存地址为空时，resolver 不会主动推送空状态，而是保留旧状态。  
这样可以避免短暂抖动导致连接池立刻“全断”，是客户端发现层常见保护策略。

---

## 5. GetConnection：组件内封装的 Dial 入口

`GetConnection(ctx, service, opts...)` 本质是：

- 组装 target：`etcd:///service`
- 调用 `grpc.NewClient(target, opts...)`
- 如果 `ctx` 有 deadline，则主动 `Connect` 并等待 Ready

这让调用方可以直接拿到“已接入服务发现”的 gRPC 连接，而不必手写 resolver 细节。

---

## 6. 关闭与收敛：为什么 Close 后不可复用

`Close()` 做了四类清理：

1. 标记 closed，后续 API 直接返回 `ErrRegistryClosed`。
2. 取消全部 watcher。
3. 取消 keepalive 并撤销所有 lease。
4. 等待后台 goroutine 退出并清理默认 registry。

这保证了实例生命周期是“单向关闭”，避免半关闭状态下的不确定行为。

---

## 7. 实践建议（围绕核心机制）

- `service.ID` 必须稳定且唯一，避免覆盖或误删别的实例。
- `DefaultTTL` 不要过短，给网络抖动留出续约窗口。
- 客户端建议使用 `round_robin`，发挥 resolver 的多地址能力。
- 生产上优先使用 `Watch + 本地缓存`，`GetService` 作为启动和兜底。
- 一进程只保留一个 active registry，切换时先 `Close` 再 `New`。

---

## 8. 设计取舍总结

`registry` 的核心价值不是“简单封装 Etcd”，而是把以下三件事打通：

- 服务端：Lease 化注册与自动下线
- 控制面：可恢复的 Watch 增量传播
- 客户端：resolver 驱动的地址集实时更新

这三条链路闭环后，服务发现才能在真实故障和动态扩缩容场景下保持稳定。
