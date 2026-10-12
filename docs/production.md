# 最小生产接入

Genesis 不需要 DI 容器。应用按“配置 → 日志/可观测性 → Connector → 业务组件 →
服务器”的顺序显式装配，按反向顺序关闭。组件选型见[选型指南](./component-selection.md)。

## 最小装配片段

下面是可放入应用初始化函数的公开 API 片段；`ctx`、`logger` 和 `redisConfig`
由调用方提供。完整可运行代码见[Connector 示例](../examples/connector/main.go)与
[缓存查询场景](../examples/cached-query/main.go)。

```go
func checkCache(ctx context.Context, logger clog.Logger, redisConfig *connector.RedisConfig) (err error) {
    conn, err := connector.NewRedis(redisConfig, connector.WithLogger(logger))
    if err != nil {
        return err
    }
    defer func() { err = xerrors.Join(err, conn.Close()) }()

    connectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
    defer cancel()
    if err := conn.Connect(connectCtx); err != nil {
        return err
    }
    values, err := cache.NewDistributed(&cache.DistributedConfig{
        KeyPrefix: "myapp:v1:",
        DefaultTTL: time.Minute,
    }, cache.WithRedisConnector(conn), cache.WithLogger(logger))
    if err != nil {
        return err
    }
    requestCtx, requestCancel := context.WithTimeout(ctx, time.Second)
    defer requestCancel()
    return values.Set(requestCtx, "bootstrap-check", "ready", time.Minute)
}
```

需要导入标准库 `context`、`time` 与 Genesis 的 `cache`、`clog`、`connector`、
`xerrors`。Distributed 借用连接，其 Close 不关闭 Connector；不需要为它额外关闭连接。

## 配置与安全

- 使用 `config.Loader` 加载文件和环境变量，然后 Unmarshal 到业务配置结构。
  文件 Watch 不会自动重新创建已经构造的组件；热更新必须由应用决定替换与释放策略。
- 构造后修改配置结构不能更新存活组件。不要输出完整配置、DSN、JWT 或密码。
- 开发示例的明文连接不是生产建议。配置可信 CA、主机名校验、ACL 与网络访问控制。
- Metrics 暴露端口应绑定受控地址；不要直接向公网暴露运行指标或示例调试接口。
- 多租户入口先认证，再设置幂等 identity scope；请求 key 必须有长度与容量边界。

## 超时与退出

启动使用有限 Connect deadline；每个 I/O 使用请求 deadline。不要在生产请求链路
中无限使用 `context.Background()`。后台任务单独持有可取消的生命周期 context。

收到退出信号后：

1. 停止 HTTP/gRPC 接收新请求，并给已接收请求有限的完成时间。
2. `MQ.Drain(ctx)` 停止拉取并等待已交付处理；超时后按策略 Close。
3. 停止 Watch、WorkerID KeepAlive、锁续期和清理任务，再关闭它们借用的 Connector。
4. 关闭调用方拥有的 gRPC 连接；registry 不代替你关闭这些连接。
5. 使用新的有限 cleanup context 关闭 trace/metrics，最后关闭根 logger 与 Loader。

已取消的请求 context 不应复用为清理预算。派生日志器不拥有根 writer。
忽略 context 的 Handler 不能被 Go 强制终止；Drain 超时并不证明业务执行已经结束。

## 部署前验证

在应用自己的环境验证凭证、TLS、后端拓扑、数据持久化、重启恢复及容量。Genesis
的容器测试不等同于你的业务验收；发布过程和扫描策略分别见[发布流程](./release.md)
与[安全政策](./v1-security-and-dependencies.md)。
