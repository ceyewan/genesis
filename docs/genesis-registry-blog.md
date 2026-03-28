# Genesis registry：基于 Etcd 的服务注册发现与 gRPC 集成设计

Genesis `registry` 是治理层组件，核心职责是把 Etcd 的 lease、KV 和 watch 机制收敛成更稳定的服务注册发现语义，并直接接入 gRPC resolver。这篇文章重点不在于逐段解释源码，而在于回答四个问题：为什么 Genesis 需要自己的 `registry`，为什么它选择“一个进程一个 active registry”，为什么 endpoint 只接受 gRPC 地址，以及它如何在 compaction、优雅下线和客户端连接这些边界场景里保持语义稳定。

## 0 摘要

`registry` 不是通用服务目录，而是严格面向 gRPC 调用链的 Etcd 注册发现组件。它在设计上接受“一个进程一个 active registry”的约束，这不仅是前提也是为了保持系统状态的一致性。为了保证连接语义的可预测性，`ServiceInstance.Endpoints` 仅接受 `grpc://host:port` 或 `host:port` 格式。在可靠性方面，`Watch` 机制在遇到 Etcd compaction 时会拉取最新快照与本地状态进行 diff，从而补发必要的事件流。同时，`GetConnection` 返回的是已绑定 resolver 的 `grpc.ClientConn`，其是否主动等待连接 `Ready` 完全取决于传入的 `ctx` 是否带有 deadline。最后，组件的 `Close` 方法不再简单地只打印日志，而是会将 lease 撤销的失败显式返回给调用方，确保资源生命周期的严谨性。

---

## 1 背景与问题

服务注册发现看起来只是“把实例地址写到 Etcd 里再读出来”，但真正落地时很容易陷入一堆边界问题。服务实例有生命周期，需要注册、续约和下线；客户端要拿到最新地址，还要在实例变化时及时更新；watch 可能被 compaction 打断；客户端连接又不能直接暴露 Etcd 细节给业务层。直接在业务里手写这些逻辑，不仅重复，而且每个服务都会在 lease、watch、连接管理和错误处理上做出不同选择。

Genesis 需要 `registry`，不是为了再包一层 Etcd client，而是为了把这些边界统一成一个稳定契约。服务端只关心“我注册了哪个实例、什么时候下线”；客户端只关心“我按服务名拿到一个可用的 gRPC 连接”；至于 lease 保活、watch 恢复、resolver 状态推进，都应该由组件内部承担。

同时，Genesis 的 `registry` 不是多驱动注册中心框架，也不是通用 endpoint 仓库。它有非常明确的范围：基于 Etcd，面向 gRPC，服务一个进程里的一个 active 服务角色。

---

## 2 基础原理与前置知识

理解 `registry` 需要两个底层概念：**lease** 和 **watch**。

Etcd 的 lease 决定了服务实例是不是“活着”。服务注册时，实例信息写到某个 key 下，并绑定一个 lease。只要 lease 持续被 keepalive，这个 key 就一直存在；如果进程异常退出或主动停止续约，lease 过期后 key 会自动删除。这个模型天然适合服务注册，因为它避免了僵尸实例长期滞留。

Watch 决定了客户端如何感知实例变化。只靠 `Get` 轮询当然也能实现服务发现，但它无法提供足够及时的更新，也会带来不必要的读放大。`registry` 通过 watch 订阅实例变化，把 PUT 和 DELETE 事件转成上层可消费的服务变更流。

但 watch 并不是绝对连续的。当 Etcd 做 compaction 时，旧 revision 可能已经不可追溯。这个时候如果组件只是简单地把 revision 跳到最新值继续监听，中间变化就会丢掉。Genesis 的 `registry` 在这里补了一层快照 diff：先取最新快照，再和本地已知实例做对比，把新增、更新、删除尽量恢复成事件流。

gRPC resolver 则解决了“发现结果如何进入客户端连接”这个问题。业务代码不应该每次自己查服务列表、随机选实例、手工拨号。更合理的做法是把服务名交给 resolver，让 gRPC 自己根据地址列表维护连接和负载均衡。`registry` 的 `GetConnection` 正是基于这个思路。

---

## 3 设计目标

`registry` 的设计目标可以收敛成五条：

- **收敛 Etcd 复杂度**：让业务直接使用注册、发现和连接能力，而不是处理 lease / watch 细节
- **面向 gRPC 主路径**：只服务 gRPC endpoint，不把 HTTP 和其他协议混进同一模型
- **单进程单 active 实例**：和服务进程的角色模型保持一致，避免全局 resolver 状态分裂
- **生命周期清晰**：注册、续约、watch、下线和关闭都要有明确语义
- **恢复路径可预测**：compaction、空实例集和 lease 撤销失败都必须被显式处理

这五条目标决定了 `registry` 的边界。它没有追求“支持尽可能多的协议和驱动”，也没有把自己做成一个抽象到失真的注册中心框架。

---

## 4 核心接口与配置

`registry` 的公开接口很克制：

```go
type Registry interface {
	Register(ctx context.Context, service *ServiceInstance, ttl time.Duration) error
	Deregister(ctx context.Context, serviceID string) error

	GetService(ctx context.Context, serviceName string) ([]*ServiceInstance, error)
	Watch(ctx context.Context, serviceName string) (<-chan ServiceEvent, error)

	GetConnection(ctx context.Context, serviceName string, opts ...grpc.DialOption) (*grpc.ClientConn, error)

	Close() error
}
```

这个接口有两个明显特点。

第一，它把“服务发现”和“客户端连接”放在同一个组件里。Genesis 在这里刻意没有做两层抽象，因为对于 gRPC 微服务来说，这两件事天然是同一条主路径：先发现，再连通。

第二，它没有暴露 lease、session、revision 等底层概念。调用方只声明自己要注册什么、要查什么、要监听什么、要连接什么。底层状态机留在内部。

配置也保持最小化：

| 字段 | 说明 |
| --- | --- |
| `Namespace` | Etcd key 前缀，默认 `/genesis/services` |
| `DefaultTTL` | 默认租约时长，默认 `30s`，必须为 `0` 或 `>= 1s` |
| `RetryInterval` | watch / resolver 重试间隔，默认 `1s` |

这里有两个值得单独强调的契约。

其一，进程内只允许一个 active registry。Genesis 接受这个限制，因为这里的设计前提就是“一个进程对应一个 active 服务角色”，而 gRPC resolver 也天然更适合这种进程级状态。

其二，`ServiceInstance.Endpoints` 只接受 gRPC 地址：

```go
type ServiceInstance struct {
	ID        string
	Name      string
	Version   string
	Metadata  map[string]string
	Endpoints []string
}
```

在当前实现里，合法 endpoint 只有两种：

- `grpc://host:port`
- `host:port`

`http://`、`https://` 和其他协议地址会在注册阶段直接报错，而不是拖到 resolver 或 RPC 连接阶段再暴露。

---

## 5 核心概念与数据模型

`registry` 的心智模型可以概括成四个对象：`ServiceInstance`、lease、watch 事件流和 gRPC 连接。

`ServiceInstance` 是注册中心里的单个实例记录。它的 `ID` 唯一标识一个实例，`Name` 表示服务名，`Endpoints` 表示这个实例对外暴露的 gRPC 地址，`Metadata` 则承载附加信息，例如机房、权重、版本标签等。

lease 绑定的是实例存活性，而不是服务定义本身。只要某个实例不再续约，对应 key 就会被 Etcd 自动删除。这让“实例是否存活”成为一个可观测、可自动收敛的状态，而不是依赖额外清理任务。

watch 输出的是 `ServiceEvent`：

```go
type ServiceEvent struct {
	Type    EventType
	Service *ServiceInstance
}
```

它只保留两类事件：`PUT` 和 `DELETE`。Genesis 没有再发明更复杂的事件模型，因为对于服务发现来说，这两类已经足够表达新增、更新和下线。

gRPC 连接则是 `registry` 对客户端最直接的产物。调用方不需要自己把 `GetService` 的结果转换成地址列表再拨号，而是直接通过 `GetConnection` 拿到一个已经接入 resolver 的 `grpc.ClientConn`。

---

## 6 关键实现思路

### 服务注册主链路

`Register` 的流程可以概括为：首先校验实例的有效性，随后在 Etcd 中创建 lease。接着，组件会将序列化后的实例 JSON 数据与 lease 绑定并写入 Etcd，最后在后台启动 keepalive 协程并记录本地的 lease 状态。

这里最关键的动作不是把实例写入 KV，而是把实例记录和 lease 绑在一起。这样一来，只要进程异常退出，lease 失效后实例会自动下线，不需要额外的兜底清理逻辑。

在注册校验上，组件现在明确拒绝非 gRPC endpoint。这个约束看起来收紧了模型，但它换来的是更可预测的连接语义。因为 `GetConnection` 的责任就是返回 gRPC 连接，如果注册中心允许混入 HTTP 地址，那错误就只能推迟到 resolver 或 RPC 层才暴露。

### Watch 与 compaction 恢复

`Watch` 的主链路是顺畅的：首先建立 watch 监听，当收到 `PUT` 或 `DELETE` 事件时，更新本地已知状态，最后向上层发送转换后的事件。

真正体现工程取舍的地方在 compaction。Genesis 不是在遇到 `ErrCompacted` 后直接把 revision 跳到最新值继续监听，而是：当发现 compaction 导致历史 revision 丢失时，组件会主动读取当前 Etcd 的最新快照，并将其与本地已知的实例状态进行 diff 对比。通过计算增量，组件会向消费方补发错失的 `PUT` 或 `DELETE` 事件，最后再从最新的 revision 处恢复 watch 监听。

这个设计的目的不是承诺“绝对不丢事件”，而是尽量恢复事件流语义，让上层状态机不要因为 revision 被 compact 就突然跳变成一个完全不可解释的新世界。

### Resolver 与空实例状态

resolver 维护的是服务名到 gRPC 地址集合的映射。这里有两个实现细节非常重要。

第一，resolver 只消费 gRPC endpoint。它会剥掉 `grpc://` 前缀，保留 `host:port` 形式的地址交给 gRPC；其他协议地址不会进入地址集。

第二，空实例集也是有效状态。当某个服务最后一个实例下线时，resolver 必须把“当前没有地址”这件事显式推给 gRPC，而不是保留旧地址。否则客户端会继续使用陈旧地址，把“服务已空”错当成“连接偶发失败”。

### GetConnection 的 ready 语义

`GetConnection` 的执行过程非常明确：它首先确保全局 resolver 已经注册，然后构造出类似 `etcd:///service-name` 的目标解析地址。基于这个地址，组件会创建 `grpc.ClientConn`，最后根据上下文可选地等待连接进入 `Ready` 状态。

这里的关键在“可选地等待 Ready”。Genesis 不想把所有调用都强行变成阻塞连接，因为这会让没有 deadline 的调用挂得过久，也会让连接初始化语义变得模糊。所以它采用了一个直接但清晰的约定：

- `ctx` 带 deadline：主动尝试连接并等待进入 `Ready`
- `ctx` 不带 deadline：只返回已绑定 resolver 的 `ClientConn`

这个行为没有追求“总是最聪明”，而是优先保证可预测。

### Close 的资源语义

`Close` 不是装饰性 API。它真正承担三件事：

- 停止 keepalive
- 停止 watch
- 尽力撤销 registry 创建的 lease

更重要的是，lease 撤销失败现在会显式返回，而不是只打 warning。对于治理组件来说，“优雅下线失败”不是一个只写日志就算结束的事件。

---

## 7 工程取舍与设计权衡

### 为什么接受单进程单 active registry

如果把 `registry` 做成可以在同一进程里随意创建多个独立实例，理论上更灵活，但现实里会带来 resolver 状态归属、连接目标路由和资源回收的复杂度。Genesis 在这里选择了一条更克制的路：一个进程对应一个 active 服务角色，一个进程对应一个 active registry。这和绝大多数服务进程的实际部署模型是对齐的。

### 为什么不支持通用 endpoint 列表

很多注册中心设计喜欢把 endpoint 做成“什么协议都能塞进去的字符串数组”。这在存储层当然灵活，但一旦组件还要提供 `GetConnection` 这种连接能力，这种灵活性就会反过来污染 API 语义。Genesis 在这里选择牺牲一部分“通用性”，换取更清晰的连接契约。

### 为什么不把 compaction 恢复简化成“跳到最新 revision”

直接跳 revision 的实现最简单，但它会让 watch 消费方完全失去对中间变化的解释能力。Genesis 现在的快照 diff 恢复当然比“纯 watch 流”更重一点，但它把复杂度留在组件内部，换来更稳定的上层语义，这是值得的。

### 为什么 Close 要返回撤销失败

很多基础组件在关闭时喜欢“尽力而为”，失败就记日志然后返回 `nil`。这种做法对普通工具类组件未必有大问题，但对注册中心这种治理组件来说并不够。服务实例下线失败会直接影响流量收敛，所以 Genesis 选择把错误暴露出来，让调用方有机会感知和处理。

---

## 8 适用场景与实践建议

`registry` 适合以下场景：

- 你在用 Etcd 做服务注册发现
- 你的服务调用链主要是 gRPC
- 你希望服务端和客户端都复用同一套注册发现语义

它不适合以下场景：

- 你要做一个同时服务 HTTP、gRPC、MQTT 等多协议的通用目录
- 你需要在同一进程里持有多个 active registry
- 你希望在不引入 Etcd 的前提下抽象出“任意注册中心驱动”

实践上有几条建议。

第一，注册时尽量显式使用 `grpc://host:port`。虽然裸 `host:port` 也支持，但带上协议前缀能让实例信息更自解释。

第二，业务代码里如果把 `GetConnection` 当成同步拨号，请务必传带 timeout 的 `ctx`。否则你拿到的只是一个已绑定 resolver 的连接对象，而不是已经 ready 的连接。

```go
// 推荐用法：使用带超时控制的 context 获取 Ready 状态的连接
ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
defer cancel()

conn, err := reg.GetConnection(ctx, "user-service", grpc.WithTransportCredentials(insecure.NewCredentials()))
if err != nil {
	// 处理连接失败
}
defer conn.Close()
```

第三，调用方要认真处理 `Close()` 的错误返回。它不是形式化的清理动作，而是服务优雅下线链路的一部分。

第四，如果你需要的是服务地址列表而不是 gRPC 连接，就直接用 `GetService` 或 `Watch`，不要为了“统一”而强行绕到 `GetConnection`。

---

## 9 总结

Genesis `registry` 的核心价值，不是把 Etcd client 再包一层，而是把服务注册、lease 生命周期、watch 恢复和 gRPC resolver 集成收敛成一个更稳定的工程契约。它刻意接受一些边界约束，例如单进程单 active registry、只接受 gRPC endpoint、`GetConnection` 的 ready 语义显式依赖 context deadline。代价是少了一些“看起来更通用”的弹性，收益是接口更可预测、行为更一致、排障更直接。

如果你的场景正好是 Etcd + gRPC 的服务发现主路径，这种克制的设计会比一个更“通用”、但语义更模糊的注册中心抽象更合适。
