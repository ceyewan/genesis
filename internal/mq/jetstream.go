package mq

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/connector"
	"github.com/ceyewan/genesis/pkg/mq/types"
	telemetrytypes "github.com/ceyewan/genesis/pkg/telemetry/types"
)

// jetStreamClient NATS JetStream 模式实现
type jetStreamClient struct {
	js     jetstream.JetStream
	cfg    *types.JetStreamConfig
	logger clog.Logger
	meter  telemetrytypes.Meter
	tracer telemetrytypes.Tracer
}

// NewJetStreamClient 创建 NATS JetStream 客户端
func NewJetStreamClient(conn connector.NATSConnector, cfg *types.JetStreamConfig, logger clog.Logger, meter telemetrytypes.Meter, tracer telemetrytypes.Tracer) (types.Client, error) {
	js, err := jetstream.New(conn.GetClient())
	if err != nil {
		return nil, fmt.Errorf("failed to create jetstream context: %w", err)
	}

	return &jetStreamClient{
		js:     js,
		cfg:    cfg,
		logger: logger,
		meter:  meter,
		tracer: tracer,
	}, nil
}

func (c *jetStreamClient) Publish(ctx context.Context, subject string, data []byte) error {
	// 默认使用同步发送以确保持久化
	_, err := c.js.Publish(ctx, subject, data)
	return err
}

func (c *jetStreamClient) Subscribe(ctx context.Context, subject string, handler types.Handler) (types.Subscription, error) {
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
		return nil, fmt.Errorf("failed to create consumer: %w", err)
	}

	return c.consume(ctx, consumer, handler)
}

func (c *jetStreamClient) QueueSubscribe(ctx context.Context, subject string, queue string, handler types.Handler) (types.Subscription, error) {
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
		return nil, fmt.Errorf("failed to create durable consumer: %w", err)
	}

	return c.consume(ctx, consumer, handler)
}

func (c *jetStreamClient) consume(_ context.Context, consumer jetstream.Consumer, handler types.Handler) (types.Subscription, error) {
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

func (c *jetStreamClient) Request(ctx context.Context, subject string, data []byte, timeout time.Duration) (types.Message, error) {
	// JetStream 模式不推荐使用 Request/Reply，但为了接口兼容，可以回退到 Core NATS 的 Request
	// 或者直接报错。这里选择报错以明确语义。
	return nil, fmt.Errorf("request/reply pattern is not supported in JetStream mode")
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
