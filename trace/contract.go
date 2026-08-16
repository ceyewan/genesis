package trace

const (
	// Messaging 语义属性键
	AttrMessagingSystem        = "messaging.system"
	AttrMessagingDestination   = "messaging.destination.name"
	AttrMessagingOperation     = "messaging.operation.name"
	AttrMessagingOperationType = "messaging.operation.type"
	AttrMessagingConsumerGroup = "messaging.consumer.group.name"
)

const (
	// 常见的消息系统
	MessagingSystemNATS = "nats"
)

const (
	// 常见的消息操作
	MessagingOperationPublish = "publish"
	MessagingOperationConsume = "consume"
	MessagingOperationProcess = "process"
)

func messagingOperationType(operation string) string {
	switch operation {
	case MessagingOperationPublish:
		return "send"
	case MessagingOperationConsume:
		return "receive"
	case MessagingOperationProcess:
		return "process"
	default:
		return operation
	}
}

// MessagingTraceRelation 表示消费者 Span 与上游消息 Span 的关系建模方式
type MessagingTraceRelation string

const (
	// MessagingTraceRelationLink 使用 Span Link 关联上游（默认，适合异步/批处理/多消费者）
	MessagingTraceRelationLink MessagingTraceRelation = "link"
	// MessagingTraceRelationChildOf 使用 parent/child 关系串成单条 Trace（适合端到端演示）
	MessagingTraceRelationChildOf MessagingTraceRelation = "child_of"
)

// SpanNameMQPublish 返回用于发布到主题/主题的标准 Span Name
func SpanNameMQPublish(destination string) string {
	if destination == "" {
		return "mq.publish"
	}
	return "mq.publish " + destination
}

// SpanNameMQConsume 返回用于从主题/主题消费的标准 Span Name
func SpanNameMQConsume(destination string) string {
	if destination == "" {
		return "mq.consume"
	}
	return "mq.consume " + destination
}
