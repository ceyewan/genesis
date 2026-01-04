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

	// Ack 确认消息处理成功
	// - NATS Core: 空操作
	// - JetStream: 发送 Ack
	// - Redis Stream: 仅在 Consumer Group 模式下有效
	Ack() error

	// Nak 否认消息，请求重投
	// - JetStream: 发送 Nak
	// - Redis Stream: 无原生 Nak，默认空操作
	// - NATS Core: 空操作
	Nak() error
}

// Handler 消息处理函数
type Handler func(ctx context.Context, msg Message) error

// Subscription 订阅句柄
// 用于管理订阅的生命周期（如取消订阅）
type Subscription interface {
	// Unsubscribe 取消订阅
	// 说明：该操作尽力停止后续消息投递，不保证等待当前 Handler 完成，具体行为依赖驱动实现。
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
	//
	// 注意：当 Channel 缓冲区满时会丢弃消息并返回错误给内部 handler。
	// 若需要手动 Ack/Nak（尤其是 SubscribeChan 模式），请显式设置 WithManualAck，
	// 并在消费端处理成功后调用 msg.Ack()，以获得“至少一次投递”的语义。
	SubscribeChan(ctx context.Context, subject string, opts ...SubscribeOption) (<-chan Message, Subscription, error)

	// Close 关闭客户端
	Close() error
}

// New 创建 MQ 客户端（配置驱动）
//
// 通过 cfg.Driver 选择底层驱动，依赖通过 Option 注入。
// 必需的依赖：
//   - DriverNatsCore / DriverNatsJetStream: WithNATSConnector
//   - DriverRedis: WithRedisConnector
//
// 若缺失对应依赖，将返回错误。
// 使用示例:
//
//	client, _ := mq.New(&mq.Config{
//	    Driver: mq.DriverNatsCore,
//	}, mq.WithNATSConnector(natsConn), mq.WithLogger(logger))
func New(cfg *Config, opts ...Option) (Client, error) {
	if cfg == nil {
		return nil, xerrors.New("config is nil")
	}

	cfg.setDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	opt, err := applyOptions(opts...)
	if err != nil {
		return nil, err
	}

	var driver driver
	switch cfg.Driver {
	case DriverNatsCore:
		if opt.NATSConnector == nil {
			return nil, xerrors.New("nats connector is required, use WithNATSConnector")
		}
		driver = newNatsCoreDriver(opt.NATSConnector, opt.Logger)
	case DriverNatsJetStream:
		if opt.NATSConnector == nil {
			return nil, xerrors.New("nats connector is required, use WithNATSConnector")
		}
		jsDriver, err := newNatsJetStreamDriver(opt.NATSConnector, cfg.JetStream, opt.Logger)
		if err != nil {
			return nil, xerrors.Wrap(err, "failed to create nats jetstream driver")
		}
		driver = jsDriver
	case DriverRedis:
		if opt.RedisConnector == nil {
			return nil, xerrors.New("redis connector is required, use WithRedisConnector")
		}
		driver = newRedisDriver(opt.RedisConnector, opt.Logger)
	default:
		return nil, xerrors.New("unsupported driver: " + string(cfg.Driver))
	}

	return newClient(driver, opt.Logger, opt.Meter), nil
}

func applyOptions(opts ...Option) (options, error) {
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
			return opt, xerrors.Wrapf(err, "failed to create default logger")
		}
		opt.Logger = logger
	}

	return opt, nil
}
