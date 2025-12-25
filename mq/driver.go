package mq

import (
	"context"
)

// Driver 定义底层 MQ 驱动的行为
// 所有具体的 MQ 实现（NATS, Kafka, Redis 等）都必须实现此接口
type Driver interface {
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

// PublishOption 发布选项
type PublishOption func(*publishOptions)

type publishOptions struct {
	// 预留给未来扩展，如 Delay, Priority 等
}

// SubscribeOption 订阅选项
type SubscribeOption func(*subscribeOptions)

type subscribeOptions struct {
	QueueGroup  string // 负载均衡组 (对应 NATS Queue, Kafka Consumer Group)
	AutoAck     bool   // 是否自动确认 (默认 true)
	DurableName string // 持久化订阅名 (JetStream/Redis Group)
	BufferSize  int    // Channel 模式的缓冲区大小
}

// defaultSubscribeOptions 返回默认订阅选项
func defaultSubscribeOptions() subscribeOptions {
	return subscribeOptions{
		AutoAck:    true,
		BufferSize: 100, // 默认缓冲大小
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

// WithManualAck 关闭自动确认，用户需要在 Handler 中手动调用 msg.Ack()
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
