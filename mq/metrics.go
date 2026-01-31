package mq

// 指标名称常量
const (
	// MetricPublishTotal 发布消息总数
	MetricPublishTotal = "mq.publish.total"

	// MetricPublishDuration 发布延迟（秒）
	MetricPublishDuration = "mq.publish.duration"

	// MetricConsumeTotal 消费消息总数
	MetricConsumeTotal = "mq.consume.total"

	// MetricHandleDuration 消息处理耗时（秒）
	MetricHandleDuration = "mq.handle.duration"
)

// 标签名称常量
const (
	// LabelTopic 主题标签
	LabelTopic = "topic"

	// LabelStatus 状态标签（success/error）
	LabelStatus = "status"

	// LabelDriver 驱动标签
	LabelDriver = "driver"
)
