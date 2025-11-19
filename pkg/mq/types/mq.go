package types

import (
	"context"
	"time"
)

// Message 消息接口
// 封装了底层消息的细节，提供统一的数据访问和确认机制
type Message interface {
	// Subject 获取消息主题
	Subject() string

	// Data 获取消息内容
	Data() []byte

	// Ack 确认消息处理成功 (仅 JetStream 模式有效，Core 模式下为空操作)
	Ack() error

	// Nak 否认消息，请求重投 (仅 JetStream 模式有效)
	Nak() error
}

// Handler 消息处理函数
type Handler func(ctx context.Context, msg Message) error

// Subscription 订阅句柄
// 用于管理订阅的生命周期（如取消订阅）
type Subscription interface {
	// Unsubscribe 取消订阅
	Unsubscribe() error

	// IsValid 检查订阅是否有效
	IsValid() bool
}

// Client 定义了 MQ 组件的核心能力
type Client interface {
	// Publish 发送消息
	// 在 Core 模式下是发后即忘；在 JetStream 模式下会等待持久化确认
	Publish(ctx context.Context, subject string, data []byte) error

	// Subscribe 广播订阅
	// 所有订阅该 Subject 的消费者都会收到消息
	// 适用于：配置更新通知、缓存失效通知
	Subscribe(ctx context.Context, subject string, handler Handler) (Subscription, error)

	// QueueSubscribe 队列订阅 (负载均衡)
	// 同一个 queue 组内的消费者，每条消息只会被其中一个处理
	// 适用于：任务分发、订单处理
	// 对应 Kafka 的 Consumer Group 概念
	QueueSubscribe(ctx context.Context, subject string, queue string, handler Handler) (Subscription, error)

	// Request 请求-响应 (RPC 模式)
	// 发送消息并等待响应。
	// 注意：此功能是 NATS Core 的强项。如果未来切换 Kafka，此接口可能难以高效实现或不支持。
	Request(ctx context.Context, subject string, data []byte, timeout time.Duration) (Message, error)

	// Close 关闭客户端
	Close() error
}
