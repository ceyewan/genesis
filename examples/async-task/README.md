# 可靠异步任务

运行 `make up` 后执行 `make example-async-task`，需要 NATS JetStream 和 Redis。
默认地址可通过 `NATS_URL`、`REDIS_ADDR` 覆盖。依赖或断言失败时非零退出。

第一阶段发布两条同任务 ID 的消息，第一次处理模拟临时失败并请求重投。消费者使用
Redis `idem.Consume`，等待两条消息成功处理并 Drain 后断言业务执行次数为 1。
第二阶段重新创建消费者和幂等组件，再次发布重复任务，断言新增业务执行次数为 0。
每次运行使用独立 topic/group/幂等命名空间，消息与记录有保留期限。

## 自动验收

```bash
go test -race -count=1 ./examples/async-task
```

测试自动启动 NATS 与 Redis Testcontainers，不需要 `make up`。它验证临时失败重投、
重复消费与组件重建后的持久去重，不将第一次业务成功打印误认为整个案例完成。

这不是 exactly-once。业务副作用与幂等记录提交不属于同一事务；真实邮件/支付等
业务仍需唯一约束、条件更新或外部幂等接口。Memory 去重不能跨实例或重启保留。
永久解码错误显式 Ack，并传播 Ack 失败；业务 DLQ 与补偿策略仍由应用决定。
