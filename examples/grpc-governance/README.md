# gRPC 服务发现与调用治理

运行 `make up` 后执行 `make example-grpc-governance`。Etcd 默认地址为
`127.0.0.1:2379`，可通过 `ETCD_ENDPOINTS` 配置逗号分隔的地址。
依赖或断言失败时非零退出；每次运行有独立注册 namespace。

示例开启真实 Registry Watch，并使用带服务级 breaker 的 gRPC 客户端：

1. 注册第一个 health 实例并验证调用。
2. 动态加入第二个实例，显式使用 gRPC `round_robin`，验证响应头中的实例身份。
3. 注销并停止第一个实例，验证第二个实例继续可用。
4. 以原实例 ID 和新地址重新注册，验证两个实例再次被调用。

等待事件与调用结果都有有限 deadline，不靠固定 Sleep 推测注册已经生效。客户端
连接由调用方关闭；注册、Watch 和租约由 Registry 释放，最后关闭借用的 Etcd Connector。

## 自动验收

```bash
go test -race -count=1 ./examples/grpc-governance
```

测试自动启动 Etcd Testcontainers，不需要 `make up`。Registry resolver 是进程全局
状态，测试初始化不能并行；验收结束检查注册清理与 lease 事件通道关闭。

服务发现、负载均衡和熔断是三件事。Registry 默认仍是 gRPC `pick_first`；这个场景
显式选择 round-robin。实例变化是最终可见，不保证瞬时摘除。服务级 breaker 不提供
单实例健康隔离；系统故障与半开恢复的独立验收见[弹性保护场景](../resilient-service/)。
明文 gRPC 仅用于本机示例，不是生产传输配置。
