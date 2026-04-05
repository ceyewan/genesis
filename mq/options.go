package mq

// ==================== 发布选项 ====================

// PublishOption 发布选项
type PublishOption func(*publishOptions)

// publishOptions 发布选项（内部使用）
type publishOptions struct {
	// Headers 消息头
	Headers Headers
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
	// 默认为 false（手动确认）
	AutoAck bool

	// DurableName 持久化订阅名称
	// 用于 JetStream/Redis Stream 的持久化订阅
	DurableName string

	// BatchSize 批量拉取大小
	// Redis Stream: XREADGROUP COUNT / XREAD COUNT 参数
	// JetStream: 当前实现使用 consumer.Consume() 推送模式，此参数无效
	BatchSize int

	// MaxInflight 最大在途消息数
	// JetStream: MaxAckPending
	MaxInflight int
}

// defaultSubscribeOptions 返回默认订阅选项
func defaultSubscribeOptions() subscribeOptions {
	return subscribeOptions{
		AutoAck:   false, // 默认手动确认
		BatchSize: 10,
	}
}

// WithQueueGroup 设置消费组（用于竞争消费/负载均衡）
//
// 同一消费组内的消费者竞争消费消息，实现负载均衡。
// 不设置时为独立消费（广播）模式。
//
// 驱动映射（语义有差异）：
//   - NATS JetStream: 映射为 durable consumer 名称，多实例共享同一 durable 实现负载均衡
//   - Redis Stream: 映射为 consumer group 名称，组是持久化进度的承载体
//
// 注意：两者"持久化"的载体不同，JetStream 持久化在 durable consumer，Redis 持久化在 group。
func WithQueueGroup(name string) SubscribeOption {
	return func(o *subscribeOptions) {
		o.QueueGroup = name
	}
}

// WithManualAck 关闭自动确认（默认行为）
//
// 启用后需要在 Handler 中手动调用 msg.Ack() 或 msg.Nak()。
// 适用于需要精确控制确认时机的场景。
//
// 重要说明（JetStream）：
//   - 调用 msg.Nak() 会触发消息重新投递
//   - 对于无法恢复的错误，应该 Ack() 而非 Nak()，避免无限循环
//   - 建议配合 WithRetry 中间件在应用层重试
func WithManualAck() SubscribeOption {
	return func(o *subscribeOptions) {
		o.AutoAck = false
	}
}

// WithAutoAck 开启自动确认
//
// 启用后 Handler 返回 nil 时自动 Ack，返回 error 时自动 Nak。
// 注意：这会改变默认行为，确保业务逻辑能正确处理。
func WithAutoAck() SubscribeOption {
	return func(o *subscribeOptions) {
		o.AutoAck = true
	}
}

// WithDurable 设置消费者实例名称
//
// 驱动映射（语义不同，请注意区分）：
//   - NATS JetStream: 映射为 durable consumer 名称，是消费进度游标的身份标识；
//     未设置 WithQueueGroup 时生效，设置后 WithQueueGroup 优先。
//   - Redis Stream: 映射为 consumer name，是同一 group 内消费者实例的标识；
//     需与 WithQueueGroup 配合使用，单独设置无持久化效果。
//
// 如需跨驱动共享消费进度，请使用 WithQueueGroup。
func WithDurable(name string) SubscribeOption {
	return func(o *subscribeOptions) {
		o.DurableName = name
	}
}

// WithBatchSize 设置批量拉取大小
//
// 影响单次拉取的消息数量，适当增大可提升吞吐量。
// 默认值：10
//
// 驱动支持情况：
//   - Redis Stream：有效，对应 XREADGROUP COUNT / XREAD COUNT 参数。
//   - JetStream：当前实现使用 consumer.Consume() 推送模式，此参数无效。
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
