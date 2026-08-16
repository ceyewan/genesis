# 可靠异步任务示例

运行 `make up` 后执行 `make example-async-task`。示例向 JetStream 连续发布两条
具有相同任务 ID 的消息，并在消费者侧使用 `idem.Consume` 去重；最终业务执行次数应为 1。

它演示的是 at-least-once 投递下的幂等消费，不承诺 exactly-once。临时错误由 MQ 的
自动确认策略请求重投；不可解析消息则显式确认，避免无限重试。
