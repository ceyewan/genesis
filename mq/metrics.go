package mq

// MQ 指标命名常量，便于外部对接监控系统。
const (
	// MetricPublishTotal 发布成功的消息总数
	MetricPublishTotal = "mq.publish.total"

	// MetricConsumeTotal 消费处理的消息总数
	MetricConsumeTotal = "mq.consume.total"

	// MetricHandleDuration 单条消息处理耗时（单位：秒）
	MetricHandleDuration = "mq.handle.duration"
)

// 指标标签名
const (
	MetricLabelSubject = "subject"
)
