package mq

import (
	"context"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/connector"
	"github.com/ceyewan/genesis/pkg/metrics"
	"github.com/ceyewan/genesis/pkg/xerrors"
)

// coreClient NATS Core 模式实现
type coreClient struct {
	conn   *nats.Conn
	logger clog.Logger
	meter  metrics.Meter
}

// newCoreClient 创建 NATS Core 客户端
func newCoreClient(conn connector.NATSConnector, logger clog.Logger, meter metrics.Meter) Client {
	return &coreClient{
		conn:   conn.GetClient(),
		logger: logger,
		meter:  meter,
	}
}

func (c *coreClient) Publish(ctx context.Context, subject string, data []byte) error {
	// NATS Core Publish 是发后即忘，非常快
	return c.conn.Publish(subject, data)
}

func (c *coreClient) Subscribe(ctx context.Context, subject string, handler Handler) (Subscription, error) {
	sub, err := c.conn.Subscribe(subject, func(msg *nats.Msg) {
		// 封装消息
		m := &coreMessage{msg: msg}
		// 执行用户处理逻辑
		if err := handler(context.Background(), m); err != nil {
			c.logger.Error("消息处理失败", clog.String("subject", subject), clog.Error(err))
		}
	})
	if err != nil {
		return nil, err
	}
	return &coreSubscription{sub: sub}, nil
}

func (c *coreClient) QueueSubscribe(ctx context.Context, subject string, queue string, handler Handler) (Subscription, error) {
	sub, err := c.conn.QueueSubscribe(subject, queue, func(msg *nats.Msg) {
		m := &coreMessage{msg: msg}
		if err := handler(context.Background(), m); err != nil {
			c.logger.Error("队列消息处理失败", clog.String("subject", subject), clog.String("queue", queue), clog.Error(err))
		}
	})
	if err != nil {
		return nil, err
	}
	return &coreSubscription{sub: sub}, nil
}

func (c *coreClient) Request(ctx context.Context, subject string, data []byte, timeout time.Duration) (Message, error) {
	msg, err := c.conn.Request(subject, data, timeout)
	if err != nil {
		return nil, err
	}
	return &coreMessage{msg: msg}, nil
}

func (c *coreClient) Close() error {
	// 连接由 Connector 管理，这里不需要关闭
	return nil
}

// coreMessage NATS Core 消息封装
type coreMessage struct {
	msg *nats.Msg
}

func (m *coreMessage) Subject() string {
	return m.msg.Subject
}

func (m *coreMessage) Data() []byte {
	return m.msg.Data
}

func (m *coreMessage) Ack() error {
	// Core 模式不支持 Ack
	return nil
}

func (m *coreMessage) Nak() error {
	// Core 模式不支持 Nak
	return nil
}

// coreSubscription NATS Core 订阅封装
type coreSubscription struct {
	sub *nats.Subscription
}

func (s *coreSubscription) Unsubscribe() error {
	return s.sub.Unsubscribe()
}

func (s *coreSubscription) IsValid() bool {
	return s.sub.IsValid()
}

// jetStreamClient NATS JetStream 模式实现
type jetStreamClient struct {
	js     jetstream.JetStream
	cfg    *JetStreamConfig
	logger clog.Logger
	meter  metrics.Meter
}

// newJetStreamClient 创建 NATS JetStream 客户端
func newJetStreamClient(conn connector.NATSConnector, cfg *JetStreamConfig, logger clog.Logger, meter metrics.Meter) (Client, error) {
	js, err := jetstream.New(conn.GetClient())
	if err != nil {
		return nil, xerrors.Wrap(err, "failed to create jetstream context")
	}

	return &jetStreamClient{
		js:     js,
		cfg:    cfg,
		logger: logger,
		meter:  meter,
	}, nil
}

func (c *jetStreamClient) Publish(ctx context.Context, subject string, data []byte) error {
	// 默认使用同步发送以确保持久化
	_, err := c.js.Publish(ctx, subject, data)
	return err
}

func (c *jetStreamClient) Subscribe(ctx context.Context, subject string, handler Handler) (Subscription, error) {
	// 自动创建 Stream (如果配置开启)
	if c.cfg != nil && c.cfg.AutoCreateStream {
		if err := c.ensureStream(ctx, subject); err != nil {
			return nil, err
		}
	}

	// 创建 Consumer
	// 注意：这里使用 OrderedConsumer 来模拟广播订阅的效果可能比较复杂
	// 简单起见，我们使用 Ephemeral Consumer (临时消费者)
	consumer, err := c.js.CreateOrUpdateConsumer(ctx, c.getStreamName(subject), jetstream.ConsumerConfig{
		FilterSubject: subject,
		// 临时消费者不需要 Durable Name
	})
	if err != nil {
		return nil, xerrors.Wrap(err, "failed to create consumer")
	}

	return c.consume(ctx, consumer, handler)
}

func (c *jetStreamClient) QueueSubscribe(ctx context.Context, subject string, queue string, handler Handler) (Subscription, error) {
	if c.cfg != nil && c.cfg.AutoCreateStream {
		if err := c.ensureStream(ctx, subject); err != nil {
			return nil, err
		}
	}

	// 使用 Durable Consumer 实现队列订阅 (负载均衡)
	// Durable Name 对应 Queue Group Name
	consumer, err := c.js.CreateOrUpdateConsumer(ctx, c.getStreamName(subject), jetstream.ConsumerConfig{
		Durable:       queue,
		FilterSubject: subject,
		AckPolicy:     jetstream.AckExplicitPolicy, // 显式确认
	})
	if err != nil {
		return nil, xerrors.Wrap(err, "failed to create durable consumer")
	}

	return c.consume(ctx, consumer, handler)
}

func (c *jetStreamClient) consume(_ context.Context, consumer jetstream.Consumer, handler Handler) (Subscription, error) {
	cons, err := consumer.Consume(func(msg jetstream.Msg) {
		m := &jetStreamMessage{msg: msg}

		// 执行用户逻辑
		err := handler(context.Background(), m)

		// 自动 Ack/Nak 机制
		if err != nil {
			c.logger.Error("消息处理失败，执行 Nak", clog.String("subject", msg.Subject()), clog.Error(err))
			if nakErr := msg.Nak(); nakErr != nil {
				c.logger.Error("Nak 失败", clog.Error(nakErr))
			}
		} else {
			if ackErr := msg.Ack(); ackErr != nil {
				c.logger.Error("Ack 失败", clog.Error(ackErr))
			}
		}
	})

	if err != nil {
		return nil, err
	}

	return &jetStreamSubscription{cons: cons}, nil
}

func (c *jetStreamClient) Request(ctx context.Context, subject string, data []byte, timeout time.Duration) (Message, error) {
	// JetStream 模式不推荐使用 Request/Reply，但为了接口兼容，可以回退到 Core NATS 的 Request
	// 或者直接报错。这里选择报错以明确语义。
	return nil, xerrors.New("request/reply pattern is not supported in JetStream mode")
}

func (c *jetStreamClient) Close() error {
	return nil
}

// 辅助方法：根据 Subject 推断 Stream Name
// 简单规则：取第一个点之前的部分大写，例如 "orders.created" -> "ORDERS"
func (c *jetStreamClient) getStreamName(subject string) string {
	// 这里需要更复杂的逻辑或者配置映射
	// 暂时硬编码一个默认值或者简单的提取逻辑
	// 实际生产中应该通过配置指定 Stream
	return "EVENTS" // 简化处理，假设所有事件都在 EVENTS 流中
}

func (c *jetStreamClient) ensureStream(ctx context.Context, subject string) error {
	streamName := c.getStreamName(subject)
	_, err := c.js.Stream(ctx, streamName)
	if err == nil {
		return nil // Stream 已存在
	}

	// 创建 Stream
	_, err = c.js.CreateStream(ctx, jetstream.StreamConfig{
		Name:     streamName,
		Subjects: []string{subject}, // 或者使用通配符
	})
	return err
}

// jetStreamMessage JetStream 消息封装
type jetStreamMessage struct {
	msg jetstream.Msg
}

func (m *jetStreamMessage) Subject() string {
	return m.msg.Subject()
}

func (m *jetStreamMessage) Data() []byte {
	return m.msg.Data()
}

func (m *jetStreamMessage) Ack() error {
	return m.msg.Ack()
}

func (m *jetStreamMessage) Nak() error {
	return m.msg.Nak()
}

// jetStreamSubscription JetStream 订阅封装
type jetStreamSubscription struct {
	cons jetstream.ConsumeContext
}

func (s *jetStreamSubscription) Unsubscribe() error {
	s.cons.Stop()
	return nil
}

func (s *jetStreamSubscription) IsValid() bool {
	return true // 简化处理
}
