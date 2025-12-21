// Package mq 提供消息队列组件，支持 NATS Core 和 JetStream 两种模式。
//
// MQ 组件是 Genesis 微服务组件库的消息中间件抽象层，提供了统一的发布-订阅语义。
// 支持 Core 模式（高性能、发后即忘）和 JetStream 模式（持久化、可靠投递）。
//
// 基本使用：
//
//	natsConn, _ := connector.NewNATS(natsConfig)
//	mqClient, _ := mq.New(natsConn, &mq.Config{
//	    Driver: mq.DriverNatsCore,
//	}, mq.WithLogger(logger))
//
//	// 发布消息
//	err := mqClient.Publish(ctx, "orders.created", data)
//
//	// 订阅消息
//	sub, _ := mqClient.Subscribe(ctx, "orders.created", func(ctx context.Context, msg mq.Message) error {
//	    fmt.Printf("收到消息: %s\n", string(msg.Data()))
//	    return nil
//	})
package mq

import (
	"context"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/xerrors"
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
	// Publish 发布消息
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

	// Close 关闭客户端
	Close() error
}

// New 创建 MQ 客户端
//
// 参数:
//   - conn: NATS 连接器
//   - cfg: MQ 配置
//   - opts: 可选参数 (Logger, Meter)
func New(conn connector.NATSConnector, cfg *Config, opts ...Option) (Client, error) {
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

	return newClient(conn, cfg, opt.Logger, opt.Meter)
}

// newClient 内部工厂函数
func newClient(conn connector.NATSConnector, cfg *Config, logger clog.Logger, meter metrics.Meter) (Client, error) {
	if cfg == nil {
		return nil, xerrors.New("mq config is required")
	}

	// Logger 应该由 New 确保，不应该为 nil
	if logger == nil {
		return nil, xerrors.New("logger is required, should be provided by New")
	}

	switch cfg.Driver {
	case DriverNatsCore:
		return newCoreClient(conn, logger, meter), nil
	case DriverNatsJetStream:
		return newJetStreamClient(conn, cfg.JetStream, logger, meter)
	default:
		return nil, xerrors.Wrapf(xerrors.ErrInvalidInput, "unsupported mq driver: %s", cfg.Driver)
	}
}
