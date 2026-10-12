# 常见问题排查

先用 `errors.Is` / `errors.As` 分类，结合 request/trace ID、组件日志与后端状态定位。
不要把完整凭证、请求体或任意用户 ID 放进日志和指标标签。

| 现象 | 首先检查 | 处理方向 |
| :--- | :--- | :--- |
| 消息重复执行 | Ack 是否失败、AckWait/PendingIdle 是否短于处理时间、消费身份是否稳定 | 幂等消费 + 数据库唯一约束；JetStream 长处理使用 ProgressMessage；不要承诺 exactly-once |
| Redis 消息长期 pending | consumer group、处理错误与 XAUTOCLAIM 时间 | 暂时失败等待认领；永久失败由应用决定终止/DLQ，Nak 不支持不等于服务异常 |
| 缓存远端 miss，本地仍命中 | LocalTTL、BackfillTTL、是否有其他实例直接更新 | Multi 允许陈旧读取；权限/库存选择权威数据源，不能只缩短 TTL 就宣称强一致 |
| 缓存重建仍多次回源 | 是否只有 Local、锁后是否重新检查、进程内同 Locker 是否并发重入 | 本地请求合并；共享缓存 + 每实例独立 Locker；正确处理 ErrLockAlreadyHeld |
| 幂等 key 冲突 | endpoint、身份作用域、请求 fingerprint | 同 key 不应代表不同请求；跨身份不要共享结果域 |
| 幂等成功后又执行 | TTL、Memory 重启、结果大小超限、结果提交是否失败 | 改用 Redis 持久去重；核对容量；用业务存储约束保护副作用 |
| 锁或 WorkerID 丢失 | Lost/KeepAlive 通道、后端连接、lease、暂停与 TTL | 立即停止依赖该所有权的工作；下游使用 CAS/version fence |
| 熔断始终不打开 | 实际错误类型、最小样本、失败率、key | 业务错误不统计为故障；观察稳定服务 key，超时不是熔断器替代品 |
| 实例变化没有生效 | 注册 namespace、Watch、lease 失败、gRPC 连接状态 | 观察注册和 resolver；允许传播时间；区分发现、负载均衡和服务级熔断 |
| 指标端口启动失败 | ErrListen、端口占用、绑定地址 | 启动时显式失败，不忽略监听错误；不把 metrics 端口当业务健康检查 |
| Redis TLS 失败 | CA、证书有效期、ServerName 与 ACL 凭证 | 修正信任链和主机名；不要以跳过证书验证作为长期修复 |
| config 热更新未通知 | Load 是否成功、Watch 是否启动、是否修改文件而不是环境变量 | 环境变量不被 Watch；新配置不能隐式改变已创建组件 |

## 最小复现与测试

运行目标组件测试即可自动启动对应 Testcontainers，不需要 `make up`。
Docker 不可用导致失败时先修复 Docker，不通过 Skip 掩盖真实集成路径。

```bash
go test -count=1 ./mq
go test -race -count=1 ./cache
```

运行场景示例才需要开发后端。记录 Go/后端版本、稳定 key/consumer 身份、超时及
关闭顺序；避免把重复投递、可容忍陈旧值等已说明的语义误判为随机故障。
更多边界见[选型指南](./component-selection.md)和[生产接入](./production.md)。
