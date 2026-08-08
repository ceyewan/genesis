# idgen

[![Go Reference](https://pkg.go.dev/badge/github.com/ceyewan/genesis/idgen.svg)](https://pkg.go.dev/github.com/ceyewan/genesis/idgen)

`idgen` 是 Genesis 的 ID 生成组件，位于业务层（L2）。它不只提供一种 ID，而是把四类常见能力收敛到同一个包里：

- `Generator`：本地 Snowflake 风格 64bit ID
- `UUID()`：返回 UUID v7 字符串 ID 和熵源错误
- `Sequencer`：基于 Redis 的按键递增序列号
- `Allocator`：基于 Redis/Etcd 的 WorkerID 自动分配

如果你需要的是“数据库主键”“会话内消息序号”“实例唯一 WorkerID”这几类不同问题，`idgen` 提供的是一组可组合的解法，而不是单一算法。

接口边界和设计取舍见 [Genesis IDGen：多种 ID 生成能力的设计与取舍](../docs/genesis-idgen-blog.md)，完整 API 文档见 `go doc -all ./idgen`。

## 适用场景

- 用 `Generator` 生成趋势递增的整数主键，例如订单 ID、支付流水 ID。
- 用 `UUID()` 生成跨系统传递的字符串唯一标识。
- 用 `Sequencer` 为同一业务键生成严格递增序列，例如会话消息序号。
- 用 `Allocator` 为多个服务实例自动分配唯一 WorkerID，再交给 `Generator` 使用。

不适合的场景：

- 需要按业务规则可逆解析的 ID。
- 需要强撤销、会话态或持久化审计的“租约中心”。
- 需要跨机房无限扩展位宽的自定义 Snowflake 协议。

## 快速开始

### 1. Snowflake 风格 ID

```go
gen, err := idgen.NewGenerator(&idgen.GeneratorConfig{
	Mode:         idgen.GeneratorModeMultiDC,
	WorkerID:     1,
	DatacenterID: 0,
})
if err != nil {
	panic(err)
}

id, err := gen.Next()
if err != nil {
	panic(err)
}
```

`Generator` 当前支持两种位布局模式，并统一使用自定义 epoch `2024-01-01T00:00:00Z`。

- `single_dc`：`41bit 时间戳 + 10bit worker + 12bit sequence`
- `multi_dc`：`41bit 时间戳 + 5bit datacenter + 5bit worker + 12bit sequence`

### 2. UUID v7

```go
id, err := idgen.UUID()
if err != nil {
	return err
}
```

当你更在意跨系统唯一性和字符串兼容性，而不是整数主键和位结构时，直接用 `UUID()` 更合适。

### 3. Sequencer

```go
seq, err := idgen.NewSequencer(&idgen.SequencerConfig{
	Driver:    "redis",
	KeyPrefix: "order:seq",
	Step:      1,
	TTL:       24 * time.Hour,
}, idgen.WithRedisConnector(redisConn))
if err != nil {
	panic(err)
}

nextID, err := seq.Next(ctx, "20260327")
if err != nil {
	panic(err)
}
```

`Sequencer` 当前只支持 Redis，适合“同一个业务键下递增”的场景，不适合代替全局主键。

### 4. Allocator + Generator

```go
allocator, err := idgen.NewAllocator(&idgen.AllocatorConfig{
	Driver:    "etcd",
	KeyPrefix: "myapp:worker",
	MaxID:     512,
	TTL:       30 * time.Second,
}, idgen.WithEtcdConnector(etcdConn))
if err != nil {
	panic(err)
}
defer allocator.Stop()

workerID, err := allocator.Allocate(ctx)
if err != nil {
	panic(err)
}

go func() {
	if err := <-allocator.KeepAlive(ctx); err != nil {
		panic(err)
	}
}()

gen, err := idgen.NewGenerator(&idgen.GeneratorConfig{
	Mode:     idgen.GeneratorModeSingleDC,
	WorkerID: workerID,
})
if err != nil {
	panic(err)
}
```

这是 `idgen` 在分布式环境中的典型用法：Allocator 负责实例唯一 WorkerID，Generator 负责本地高吞吐生成 64bit ID。

## 选型建议

- 优先用 `Generator`：需要整数主键、趋势递增、低延迟本地生成。
- 优先用 `UUID()`：需要字符串 ID、跨系统传递、无需数值解析。
- 优先用 `Sequencer`：需要“同一个 key 下严格递增”。
- 优先用 `Allocator`：WorkerID 不想手工配置，或实例数量会动态变化。

## 使用边界

- `Generator.Next()` 和 `NextString()` 会显式返回 error，不要忽略。
- `GeneratorConfig.Mode` 决定 Snowflake 位布局，不要再用 `DatacenterID == 0` 隐式推断模式。
- `single_dc` 模式下 `WorkerID` 范围是 `0..1023`，且 `DatacenterID` 必须为 `0`。
- `multi_dc` 模式下 `WorkerID` 范围是 `0..31`，`DatacenterID` 范围是 `0..31`。
- `Sequencer` 当前不支持 Etcd。
- `SequencerConfig.TTL`、`AllocatorConfig.TTL` 都是 `time.Duration`；不要再传裸整数秒。
- `MaxValue` 是耗尽边界；超过时返回 `ErrSequenceExhausted`，Redis 中的值保持不变，永不回绕。
- `Allocator.KeyPrefix` 是所有可能同时生成 Snowflake ID 的实例共享的 worker namespace；不同独立 worker 池必须使用不同前缀。
- `Allocator.KeepAlive()` 只能在成功 `Allocate` 后启动一次。错误通道在停止或 context 取消后关闭；任何收到的错误都表示不能继续安全使用该 WorkerID。
- `Allocator.Stop()` 可并发重复调用，会先停止并等待保活 goroutine，再按 ownership token/lease 释放 WorkerID。
