package mq

import (
	"context"
)

// driver 定义底层 MQ 驱动的行为（内部使用）
// 所有具体的 MQ 实现（NATS, Redis 等）都必须实现此接口
type driver interface {
	// Publish 发布消息
	// data: 消息体
	// opts: 发布选项（如延迟发送、优先级等，取决于实现）
	Publish(ctx context.Context, subject string, data []byte, opts ...PublishOption) error

	// Subscribe 订阅消息
	// handler: 收到消息后的回调
	// opts: 订阅选项（QueueGroup, AutoAck, etc.）
	Subscribe(ctx context.Context, subject string, handler Handler, opts ...SubscribeOption) (Subscription, error)

	// Close 关闭驱动资源
	Close() error
}

// PublishOption 发布选项（预留扩展，当前无对外可用参数）
type PublishOption func(*publishOptions)

type publishOptions struct {
	Key string // 可选消息 Key，用于部分驱动的路由
}

// SubscribeOption 订阅选项
type SubscribeOption func(*subscribeOptions)

type subscribeOptions struct {
	QueueGroup  string // 负载均衡组 (对应 NATS Queue, Redis Consumer Group)
	AutoAck     bool   // 是否自动确认 (默认 true)。手动 Ack/Nak 请用 WithManualAck 关闭自动确认
	DurableName string // 持久化订阅名 (JetStream/Redis Group)
	BufferSize  int    // Channel 模式的缓冲区大小

	// 优化选项
	BatchSize   int  // 批量拉取大小 (Redis COUNT)
	MaxInflight int  // 最大在途消息数 (JetStream)
	AsyncAck    bool // 是否异步确认 (提升吞吐量)

	// 死信队列 (DLQ) 选项
	MaxDeliver int    // 最大投递次数 (超过后进入死信队列)
	DeadLetter string // 死信队列主题
}

// defaultSubscribeOptions 返回默认订阅选项
func defaultSubscribeOptions() subscribeOptions {
	return subscribeOptions{
		AutoAck:    true,
		BufferSize: 100, // 默认缓冲大小
		BatchSize:  10,  // 默认批量大小
		AsyncAck:   false,
	}
}

// WithQueueGroup 设置队列组（用于负载均衡）
func WithQueueGroup(name string) SubscribeOption {
	return func(o *subscribeOptions) {
		o.QueueGroup = name
	}
}

// WithDurable 设置持久化订阅名称（用于 JetStream/Redis Stream）
func WithDurable(name string) SubscribeOption {
	return func(o *subscribeOptions) {
		o.DurableName = name
	}
}

// WithManualAck 关闭自动确认，适用于 Handler 内部显式 Ack/Nak 的场景
// 例如 SubscribeChan 模式下，用户在消费端手动调用 msg.Ack() 时应启用此选项，避免重复确认。
func WithManualAck() SubscribeOption {
	return func(o *subscribeOptions) {
		o.AutoAck = false
	}
}

// WithBufferSize 设置 Channel 模式的缓冲区大小
func WithBufferSize(size int) SubscribeOption {
	return func(o *subscribeOptions) {
		o.BufferSize = size
	}
}

// WithBatchSize 设置批量拉取消息的数量 (Redis 适用)
func WithBatchSize(size int) SubscribeOption {
	return func(o *subscribeOptions) {
		o.BatchSize = size
	}
}

// WithMaxInflight 设置最大在途消息数 (JetStream 适用)
func WithMaxInflight(num int) SubscribeOption {
	return func(o *subscribeOptions) {
		o.MaxInflight = num
	}
}

// WithAsyncAck 开启异步确认 (提升吞吐，但可能降低可靠性)
func WithAsyncAck() SubscribeOption {
	return func(o *subscribeOptions) {
		o.AsyncAck = true
	}
}

// WithDeadLetter 设置死信队列配置
// maxDeliver: 最大尝试次数
// subject: 死信消息发送到的主题
//
// 注意：当前为预留配置，NATS Core / Redis Stream 驱动不会处理该选项。
func WithDeadLetter(maxDeliver int, subject string) SubscribeOption {
	return func(o *subscribeOptions) {
		o.MaxDeliver = maxDeliver
		o.DeadLetter = subject
	}
}
