# Web 与 RPC 弹性保护

运行 `make example-resilient-service` 后，Gateway 监听 `127.0.0.1:8083`，Inventory
作为进程内真实 gRPC health 服务启动。使用单机限流和 Memory 幂等，不需要 Redis。

```text
Recovery → RateLimit → Idempotency → POST /orders → gRPC Client Breaker → Inventory
```

入口按 IP + 路由限流，默认每秒 2 次、Burst 3。幂等命中也消耗入口配额；失败响应不被
默认成功缓存策略复用。若增加认证，应放在幂等之前，并配置可信 identity scope。

## 手动验证

```bash
curl -i -X POST http://127.0.0.1:8083/orders -H 'X-Idempotency-Key: order-1'
curl -X POST http://127.0.0.1:8083/debug/inventory/unavailable
curl http://127.0.0.1:8083/debug/breaker
curl -X POST http://127.0.0.1:8083/debug/inventory/available
curl -X POST http://127.0.0.1:8083/debug/inventory/business-error
```

持续系统错误返回 503 并计入熔断；业务拒绝返回 409，不计入系统故障。限流返回 429。
故障请求要使用新的幂等 key，避免命中故障发生前已经完成的成功结果。
调试端点不受订单限流保护，仅用于本机演示，不能直接搬到生产。

## 自动验收

```bash
go test -race -count=1 ./examples/resilient-service
```

测试不需要 Docker，使用独立资源及真实 gRPC，检查：

- 20 个同 key 并发请求响应一致，订单与下游调用都只成功执行一次。
- 重复请求不能绕过限流，令牌恢复后再次成功。
- 系统错误触发熔断，拒绝请求不再到达下游。
- 冷却后进入半开，成功探测恢复闭合。
- 业务错误不打开熔断器。

收到 SIGINT/SIGTERM 时停止 HTTP 接收，给请求有限的完成时间，再关闭客户端、幂等、
限流和 gRPC 服务。HTTP 与下游调用均有超时；breaker 不代替 deadline。

这是无数据库的教学场景。Memory 状态不跨实例/重启保留，幂等不承诺 exactly-once。
真实订单业务仍需要数据库唯一约束与事务；服务级熔断也不等同于实例摘除或负载均衡。
