package mqv2

import "context"

// Transport 底层传输层接口（内部使用）
//
// 定义了 MQ 后端必须实现的核心能力。
// 设计考量：
//   - 接口精简，只包含必需方法
//   - 扩展能力通过 Option 传递，不污染接口签名
//   - 为未来 Kafka 等重量级 MQ 预留扩展空间
type Transport interface {
	// Publish 发布消息
	//
	// 实现要求：
	//   - 支持 Headers 透传
	//   - 错误应包含足够上下文信息
	Publish(ctx context.Context, topic string, data []byte, opts publishOptions) error

	// Subscribe 订阅消息
	//
	// 实现要求：
	//   - 将 subscribeCtx 传递给 Message.Context()
	//   - 根据 opts.AutoAck 决定是否自动确认
	//   - 支持 QueueGroup 负载均衡
	Subscribe(subscribeCtx context.Context, topic string, handler Handler, opts subscribeOptions) (Subscription, error)

	// Close 关闭 Transport
	//
	// 注意：底层连接由 Connector 管理，此方法仅释放 Transport 内部资源。
	Close() error

	// Capabilities 返回该 Transport 支持的能力
	//
	// 用于运行时能力检查，避免用户使用不支持的功能。
	Capabilities() Capabilities
}

// Capabilities 描述 Transport 支持的能力
//
// 不同后端能力差异较大，通过此结构暴露给上层：
//   - NATS Core: 最简单，无持久化
//   - NATS JetStream: 持久化、重投、死信队列
//   - Redis Stream: 持久化、Consumer Group
//   - Kafka (未来): 持久化、分区、事务
type Capabilities struct {
	// Persistence 是否支持消息持久化
	Persistence bool

	// ExactlyOnce 是否支持精确一次语义
	ExactlyOnce bool

	// Nak 是否支持消息拒绝重投
	Nak bool

	// DeadLetter 是否支持死信队列
	DeadLetter bool

	// QueueGroup 是否支持队列组（负载均衡）
	QueueGroup bool

	// OrderedDelivery 是否保证顺序投递
	OrderedDelivery bool

	// BatchConsume 是否支持批量消费
	BatchConsume bool

	// DelayedMessage 是否支持延迟消息
	DelayedMessage bool
}

// CapabilitiesNATSCore NATS Core 的能力描述
var CapabilitiesNATSCore = Capabilities{
	Persistence:     false,
	ExactlyOnce:     false,
	Nak:             false,
	DeadLetter:      false,
	QueueGroup:      true,
	OrderedDelivery: false,
	BatchConsume:    false,
	DelayedMessage:  false,
}

// CapabilitiesNATSJetStream NATS JetStream 的能力描述
var CapabilitiesNATSJetStream = Capabilities{
	Persistence:     true,
	ExactlyOnce:     true, // 通过 message deduplication
	Nak:             true,
	DeadLetter:      false, // 需要额外配置，标记为不支持
	QueueGroup:      true,
	OrderedDelivery: true, // 单 consumer 时保证
	BatchConsume:    true,
	DelayedMessage:  false,
}

// CapabilitiesRedisStream Redis Stream 的能力描述
var CapabilitiesRedisStream = Capabilities{
	Persistence:     true,
	ExactlyOnce:     false, // at-least-once
	Nak:             false, // 无原生支持
	DeadLetter:      false, // 需要额外实现
	QueueGroup:      true,  // Consumer Group
	OrderedDelivery: true,  // 单 stream 保证
	BatchConsume:    true,  // XREADGROUP COUNT
	DelayedMessage:  false,
}

// CapabilitiesKafka Kafka 的能力描述（预留）
var CapabilitiesKafka = Capabilities{
	Persistence:     true,
	ExactlyOnce:     true, // 事务支持
	Nak:             false,
	DeadLetter:      true, // 通过 error topic 实现
	QueueGroup:      true, // Consumer Group
	OrderedDelivery: true, // 分区内保证
	BatchConsume:    true,
	DelayedMessage:  false,
}
