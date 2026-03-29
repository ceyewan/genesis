// Package mq 提供消息队列组件，支持 NATS JetStream 和 Redis Stream 两种后端。
//
// MQ 组件是 Genesis L2 业务层组件，提供统一的发布-订阅接入方式，但不伪装成
// 两个驱动完全一致的语义。
// 设计原则：
//   - 简单优于复杂：核心接口精简，通过 Option 扩展能力
//   - 显式优于隐式：不做自动注入，用户完全掌控消息流
//   - 语义明确：两个驱动都提供持久化和 At-least-once 投递，但 Ack/Nak、
//     QueueGroup、Durable、BatchSize 等细节保留各自差异
package mq

import (
	"context"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/xerrors"
)

// MQ 消息队列核心接口
//
// 提供统一的发布订阅入口，并保留底层驱动的语义差异。
// 当前支持的后端：NATS JetStream、Redis Stream。
// 两者均提供持久化和 At-least-once 投递，但 Nak 语义不同，详见 Message.Nak()。
type MQ interface {
	// Publish 发布消息到指定主题
	//
	// 参数：
	//   - ctx: 上下文，用于超时控制和取消
	//   - topic: 消息主题（NATS subject / Redis stream key）
	//   - data: 消息体
	//   - opts: 发布选项（Headers 等）
	Publish(ctx context.Context, topic string, data []byte, opts ...PublishOption) error

	// Subscribe 订阅主题并处理消息
	//
	// Handler 签名：func(msg Message) error
	// 通过 msg.Context() 获取上下文，避免参数冗余。
	//
	// 参数：
	//   - ctx: 订阅生命周期上下文，取消时自动停止订阅
	//   - topic: 订阅主题
	//   - handler: 消息处理函数
	//   - opts: 订阅选项（QueueGroup、AutoAck 等）
	Subscribe(ctx context.Context, topic string, handler Handler, opts ...SubscribeOption) (Subscription, error)

	// Close 关闭 MQ 客户端
	// 注意：底层连接由 Connector 管理，此方法仅释放 MQ 内部资源
	Close() error
}

// New 创建 MQ 实例
//
// 根据 Config.Driver 选择底层 Transport 实现。
// 必需依赖通过 Option 注入：
//   - NATS JetStream: WithNATSConnector
//   - Redis Stream: WithRedisConnector
//
// 示例：
//
//	mq, err := mq.New(&mq.Config{
//	    Driver: mq.DriverNATSJetStream,
//	}, mq.WithNATSConnector(natsConn), mq.WithLogger(logger))
func New(cfg *Config, opts ...Option) (MQ, error) {
	if cfg == nil {
		return nil, xerrors.New("config is nil")
	}

	cfg.setDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	o := applyOptions(opts...)

	// 创建 Transport
	transport, err := newTransport(cfg, o)
	if err != nil {
		return nil, err
	}

	return &mq{
		transport: transport,
		logger:    o.logger,
		meter:     o.meter,
		driver:    cfg.Driver,
	}, nil
}

// newTransport 根据配置创建对应的 Transport 实现
func newTransport(cfg *Config, o *options) (Transport, error) {
	switch cfg.Driver {
	case DriverNATSJetStream:
		if o.natsConnector == nil {
			return nil, xerrors.New("NATS connector required, use WithNATSConnector")
		}
		return newNATSJetStreamTransport(o.natsConnector, cfg.JetStream, o.logger)

	case DriverRedisStream:
		if o.redisConnector == nil {
			return nil, xerrors.New("Redis connector required, use WithRedisConnector")
		}
		return newRedisStreamTransport(o.redisConnector, cfg.RedisStream, o.logger), nil

	default:
		return nil, xerrors.Wrapf(ErrInvalidConfig, "unsupported driver: %s", cfg.Driver)
	}
}

// applyOptions 应用选项并设置默认值
func applyOptions(opts ...Option) *options {
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}

	// 设置默认 Logger
	if o.logger == nil {
		o.logger = clog.Discard()
	}

	// 设置默认 Meter
	if o.meter == nil {
		o.meter = metrics.Discard()
	}

	return o
}

// Option MQ 配置选项
type Option func(*options)

type options struct {
	logger         clog.Logger
	meter          metrics.Meter
	natsConnector  connector.NATSConnector
	redisConnector connector.RedisConnector
}

// WithLogger 注入日志记录器
func WithLogger(l clog.Logger) Option {
	return func(o *options) {
		if l != nil {
			o.logger = l.WithNamespace("mq")
		}
	}
}

// WithMeter 注入指标收集器
func WithMeter(m metrics.Meter) Option {
	return func(o *options) {
		if m != nil {
			o.meter = m
		}
	}
}

// WithNATSConnector 注入 NATS 连接器（用于 NATS Core / JetStream）
func WithNATSConnector(conn connector.NATSConnector) Option {
	return func(o *options) {
		o.natsConnector = conn
	}
}

// WithRedisConnector 注入 Redis 连接器（用于 Redis Stream）
func WithRedisConnector(conn connector.RedisConnector) Option {
	return func(o *options) {
		o.redisConnector = conn
	}
}
