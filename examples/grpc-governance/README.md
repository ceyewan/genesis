# gRPC 服务发现与调用治理示例

运行 `make up` 后执行 `make example-grpc-governance`。示例将一个 gRPC health 服务注册到
Etcd，调用方通过 `registry.GetConnection` 完成发现，并将 `breaker` 安装在 gRPC 客户端侧。

示例先验证正常调用，再停止实例并发送失败调用，输出当前 breaker 状态。它用于展示服务
发现与故障隔离的组合关系；实例摘除和恢复后的负载均衡可继续参考 `examples/grpc-registry`。
