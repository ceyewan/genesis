// Package mq 提供消息队列组件，支持 NATS Core, JetStream, Redis Stream 等多种模式。
//
// MQ 组件是 Genesis 微服务组件库的消息中间件抽象层，提供了统一的发布-订阅语义。
package mq

import (
	"context"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/xerrors"
)

// Message 消息接口
// 封装了底层消息的细节，提供统一的数据访问和确认机制
type Message interface {
	// Subject 获取消息主题
	Subject() string

	// Data 获取消息内容
	Data() []byte

	// Ack 确认消息处理成功 (仅 JetStream/Redis 模式有效，Core 模式下为空操作)
	Ack() error

	// Nak 否认消息，请求重投 (仅 JetStream/Redis 模式有效)
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
	// Publish 发布消息
	Publish(ctx context.Context, subject string, data []byte, opts ...PublishOption) error

	// Subscribe 订阅消息
	// 支持普通订阅和队列订阅（通过 WithQueueGroup 选项）
	Subscribe(ctx context.Context, subject string, handler Handler, opts ...SubscribeOption) (Subscription, error)

	// SubscribeChan Channel 模式订阅
	// 返回一个只读 Channel，用户可以通过 range 遍历消息
	// 必须调用 Subscription.Unsubscribe 来关闭 Channel 和释放资源
	SubscribeChan(ctx context.Context, subject string, opts ...SubscribeOption) (<-chan Message, Subscription, error)

	// Close 关闭客户端
	Close() error
}

// New 创建 MQ 客户端
//
// 参数:
//   - driver: 底层驱动实现 (如 NatsDriver)
//   - opts: 可选参数 (Logger, Meter)
func New(driver Driver, opts ...Option) (Client, error) {
	// 应用选项
	opt := options{}
	for _, o := range opts {
		o(&opt)
	}

	// 如果没有提供 Logger，创建默认实例
	if opt.Logger == nil {
		logger, err := clog.New(&clog.Config{
			Level:  "info",
			Format: "json",
			Output: "stdout",
		})
		if err != nil {
			return nil, xerrors.Wrapf(err, "failed to create default logger")
		}
		opt.Logger = logger
	}

	return newClient(driver, opt.Logger, opt.Meter), nil
}
