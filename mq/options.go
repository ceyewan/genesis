package mq

// ==================== 发布选项 ====================

// PublishOption 发布选项
type PublishOption func(*publishOptions)

// publishOptions 发布选项（内部使用）
type publishOptions struct {
	// Headers 消息头
	Headers Headers

	// Key 消息 Key（用于分区路由，Kafka 场景预留）
	Key string
}

// defaultPublishOptions 返回默认发布选项
func defaultPublishOptions() publishOptions {
	return publishOptions{}
}

// WithHeaders 设置消息头
//
// 示例：
//
//	mq.Publish(ctx, "topic", data, mq.WithHeaders(mq.Headers{
//	    "trace-id": "abc123",
//	}))
func WithHeaders(h Headers) PublishOption {
	return func(o *publishOptions) {
		o.Headers = h.Clone()
	}
}

// WithHeader 设置单个消息头
func WithHeader(key, value string) PublishOption {
	return func(o *publishOptions) {
		if o.Headers == nil {
			o.Headers = make(Headers)
		}
		o.Headers[key] = value
	}
}

// WithKey 设置消息 Key（用于分区路由）
//
// 注意：当前仅预留，NATS/Redis 不使用此选项。
// Kafka 场景下用于保证相同 Key 的消息路由到同一分区。
func WithKey(key string) PublishOption {
	return func(o *publishOptions) {
		o.Key = key
	}
}

// ==================== 订阅选项 ====================

// SubscribeOption 订阅选项
type SubscribeOption func(*subscribeOptions)

// subscribeOptions 订阅选项（内部使用）
type subscribeOptions struct {
	// QueueGroup 队列组名称（用于负载均衡）
	// 同一组内的消费者竞争消费消息
	QueueGroup string

	// AutoAck 是否自动确认
	// true: Handler 返回 nil 自动 Ack，返回 error 自动 Nak
	// false: 用户在 Handler 中手动调用 msg.Ack()/Nak()
	AutoAck bool

	// DurableName 持久化订阅名称
	// 用于 JetStream/Redis Stream 的持久化订阅
	DurableName string

	// BatchSize 批量拉取大小
	// Redis Stream: XREADGROUP COUNT 参数
	// JetStream: Fetch batch size
	BatchSize int

	// MaxInflight 最大在途消息数
	// JetStream: MaxAckPending
	MaxInflight int

	// BufferSize Channel 缓冲区大小（用于内部消息分发）
	BufferSize int

	// DeadLetter 死信队列配置
	DeadLetter *DeadLetterConfig
}

// DeadLetterConfig 死信队列配置
//
// 注意：当前为预留配置，各驱动暂未实现。
// 未来实现计划：
//   - JetStream: 利用 RedeliveryPolicy + 自定义逻辑
//   - Redis Stream: 基于 Pending 列表 + XCLAIM 实现
//   - Kafka: 发送到 error topic
type DeadLetterConfig struct {
	// MaxRetries 最大重试次数，超过后进入死信队列
	MaxRetries int

	// Topic 死信队列主题
	Topic string
}

// defaultSubscribeOptions 返回默认订阅选项
func defaultSubscribeOptions() subscribeOptions {
	return subscribeOptions{
		AutoAck:    true,
		BatchSize:  10,
		BufferSize: 100,
	}
}

// WithQueueGroup 设置队列组（用于负载均衡）
//
// 同一队列组内的消费者竞争消费消息，实现负载均衡。
// 不同队列组独立消费，实现广播。
//
// 对应关系：
//   - NATS: Queue Subscribe
//   - Redis Stream: Consumer Group
//   - Kafka (未来): Consumer Group
func WithQueueGroup(name string) SubscribeOption {
	return func(o *subscribeOptions) {
		o.QueueGroup = name
	}
}

// WithManualAck 关闭自动确认
//
// 启用后需要在 Handler 中手动调用 msg.Ack() 或 msg.Nak()。
// 适用于需要精确控制确认时机的场景。
func WithManualAck() SubscribeOption {
	return func(o *subscribeOptions) {
		o.AutoAck = false
	}
}

// WithAutoAck 开启自动确认（默认行为）
func WithAutoAck() SubscribeOption {
	return func(o *subscribeOptions) {
		o.AutoAck = true
	}
}

// WithDurable 设置持久化订阅名称
//
// 持久化订阅会记录消费进度，重启后继续消费。
// 仅 JetStream / Redis Stream 有效。
func WithDurable(name string) SubscribeOption {
	return func(o *subscribeOptions) {
		o.DurableName = name
	}
}

// WithBatchSize 设置批量拉取大小
//
// 影响单次拉取的消息数量，适当增大可提升吞吐量。
// 默认值：10
func WithBatchSize(size int) SubscribeOption {
	return func(o *subscribeOptions) {
		if size > 0 {
			o.BatchSize = size
		}
	}
}

// WithMaxInflight 设置最大在途消息数
//
// 限制未确认消息的数量，用于背压控制。
// 仅 JetStream 有效（对应 MaxAckPending）。
func WithMaxInflight(n int) SubscribeOption {
	return func(o *subscribeOptions) {
		if n > 0 {
			o.MaxInflight = n
		}
	}
}

// WithBufferSize 设置内部缓冲区大小
//
// 默认值：100
func WithBufferSize(size int) SubscribeOption {
	return func(o *subscribeOptions) {
		if size > 0 {
			o.BufferSize = size
		}
	}
}

// WithDeadLetter 设置死信队列配置
//
// 注意：当前为预留配置，各驱动暂未实现。
// 调用此选项不会报错，但不会生效。
func WithDeadLetter(maxRetries int, topic string) SubscribeOption {
	return func(o *subscribeOptions) {
		o.DeadLetter = &DeadLetterConfig{
			MaxRetries: maxRetries,
			Topic:      topic,
		}
	}
}
