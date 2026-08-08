package mq

import "context"

// transport 是底层传输层接口，仅供 mq 包内部实现使用。
//
// 定义了 MQ 后端必须实现的核心能力。不支持的操作应返回 ErrNotSupported。
type transport interface {
	// Publish 发布消息
	Publish(ctx context.Context, topic string, data []byte, opts publishOptions) error

	// Subscribe 订阅消息
	//
	// 实现要求：
	//   - 将 subscribeCtx 传递给 Message.Context()
	//   - 支持 QueueGroup 负载均衡
	Subscribe(subscribeCtx context.Context, topic string, handler Handler, opts subscribeOptions) (Subscription, error)

	// Close 关闭 transport
	//
	// 注意：底层连接由 Connector 管理，此方法仅释放 transport 内部资源。
	Close() error
}
