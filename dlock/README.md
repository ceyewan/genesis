# dlock

`dlock` 是 Genesis 的 L2 分布式锁组件，提供统一的 `Locker` 接口，支持 Redis 和 Etcd 两种后端。它解决的问题不是“实现所有锁模型”，而是把任务竞选、资源互斥、跨实例串行化这类常见场景收敛成一组稳定、可预测的 API。

## 组件定位

- 提供 `Lock` / `TryLock` / `Unlock` / `Close` 四个核心方法
- 支持 Redis token 校验解锁和 Etcd lease/mutex 两种实现
- 在锁持有期间自动续期，减少长任务执行时的锁过期风险
- `Close()` 会停止续期，并尽力释放当前 `Locker` 已持有的锁
- 支持通过 `WithTTL(...)` 覆盖单次加锁 TTL

`dlock` 不提供可重入锁、读写锁、公平锁、锁诊断平台或死锁检测。如果你需要非常定制化的锁协议，应该直接使用底层 Redis 或 Etcd 客户端。

## 快速开始

```go
redisConn, err := connector.NewRedis(&cfg.Redis, connector.WithLogger(logger))
if err != nil {
    return err
}
defer redisConn.Close()

locker, err := dlock.New(&dlock.Config{
    Driver:        dlock.DriverRedis,
    Prefix:        "myapp:lock:",
    DefaultTTL:    10 * time.Second,
    RetryInterval: 100 * time.Millisecond,
}, dlock.WithRedisConnector(redisConn), dlock.WithLogger(logger))
if err != nil {
    return err
}
defer locker.Close()

ctx := context.Background()
if err := locker.Lock(ctx, "inventory:42"); err != nil {
    return err
}
defer locker.Unlock(ctx, "inventory:42")

// critical section
```

## 核心接口

```go
type Locker interface {
    Lock(ctx context.Context, key string, opts ...LockOption) error
    TryLock(ctx context.Context, key string, opts ...LockOption) (bool, error)
    Unlock(ctx context.Context, key string) error
    Close() error
}
```

`Lock` 适合“拿不到锁就不能继续”的场景，内部按 `RetryInterval` 重试；`TryLock` 适合任务竞选这类“拿不到就跳过”的场景；`Unlock` 只允许持有者释放；`Close` 用于结束当前 `Locker` 生命周期，停止续期并清理它持有的锁。

## TTL 语义

`WithTTL(...)` 看起来是统一选项，但两种后端的精度并不完全一样：

- Redis 直接使用原生 `time.Duration`
- Etcd 基于 lease，TTL 是秒级

因此 Etcd 的 `DefaultTTL` 和 `WithTTL(...)` 都必须满足：

- 至少 `1*time.Second`
- 必须是整秒，例如 `5*time.Second`

非法 TTL 会返回 `ErrInvalidTTL`，不会再静默回退到默认值。

## 推荐场景

### 任务竞选

```go
ok, err := locker.TryLock(ctx, "jobs:daily-settlement")
if err != nil {
    return err
}
if !ok {
    return nil
}
defer locker.Unlock(ctx, "jobs:daily-settlement")

runSettlement()
```

### 短事务串行化

```go
key := fmt.Sprintf("inventory:%d", productID)
if err := locker.Lock(ctx, key, dlock.WithTTL(30*time.Second)); err != nil {
    return err
}
defer locker.Unlock(ctx, key)

return updateInventory(ctx, productID)
```

## 错误语义

常见错误包括：

- `ErrLockAlreadyHeld`：当前 `Locker` 已在本地持有同一个 key
- `ErrLockNotHeld`：尝试释放一个当前 `Locker` 没持有的锁
- `ErrOwnershipLost`：远端锁已经不属于当前持有者
- `ErrInvalidTTL`：TTL 非法，常见于 Etcd 子秒级 TTL

业务代码通常只需要区分“锁冲突”“所有权丢失”和“底层异常”三类场景。

## 日志与资源释放

通过 `WithLogger` 注入 `clog.Logger` 后，组件会自动附加 `component=dlock` 字段，并在加锁、解锁、续期失败、所有权丢失等关键事件上输出结构化日志。

`dlock` 不拥有底层 Redis / Etcd 连接，因此：

- `locker.Close()` 负责清理当前 `Locker` 自己持有的锁和续期状态
- `redisConn.Close()` / `etcdConn.Close()` 仍然由调用方负责

## 相关文档

- [包文档](https://pkg.go.dev/github.com/ceyewan/genesis/dlock)
- [组件设计博客](../docs/genesis-dlock-blog.md)
- [完整示例](../examples/dlock/main.go)
