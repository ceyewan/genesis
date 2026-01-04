package mq

import (
	"context"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/xerrors"
)

// natsCoreDriver NATS Core 驱动实现
type natsCoreDriver struct {
	conn   *nats.Conn
	logger clog.Logger
}

// newNatsCoreDriver 创建 NATS Core 驱动
func newNatsCoreDriver(conn connector.NATSConnector, logger clog.Logger) *natsCoreDriver {
	return &natsCoreDriver{
		conn:   conn.GetClient(),
		logger: logger,
	}
}

func (d *natsCoreDriver) Publish(ctx context.Context, subject string, data []byte, opts ...PublishOption) error {
	// NATS Core 不支持 PublishOption (如延迟等)
	return d.conn.Publish(subject, data)
}

func (d *natsCoreDriver) Subscribe(ctx context.Context, subject string, handler Handler, opts ...SubscribeOption) (Subscription, error) {
	o := defaultSubscribeOptions()
	for _, opt := range opts {
		opt(&o)
	}

	// 内部回调函数
	cb := func(msg *nats.Msg) {
		m := &coreMessage{msg: msg}
		if err := handler(context.Background(), m); err != nil {
			d.logger.Error("消息处理失败", clog.String("subject", subject), clog.Error(err))
		}
	}

	var sub *nats.Subscription
	var err error

	if o.QueueGroup != "" {
		sub, err = d.conn.QueueSubscribe(subject, o.QueueGroup, cb)
	} else {
		sub, err = d.conn.Subscribe(subject, cb)
	}

	if err != nil {
		return nil, xerrors.Wrapf(err, "subscribe failed for subject %s", subject)
	}
	return &coreSubscription{sub: sub}, nil
}

func (d *natsCoreDriver) Close() error {
	// 连接由 Connector 管理，不需要关闭
	return nil
}

// natsJetStreamDriver NATS JetStream 驱动实现
type natsJetStreamDriver struct {
	js     jetstream.JetStream
	cfg    *JetStreamConfig
	logger clog.Logger
}

// JetStreamConfig JetStream 特有配置
type JetStreamConfig struct {
	// 是否自动创建 Stream (如果不存在)
	AutoCreateStream bool `json:"auto_create_stream" yaml:"auto_create_stream"`
}

// newNatsJetStreamDriver 创建 NATS JetStream 驱动
func newNatsJetStreamDriver(conn connector.NATSConnector, cfg *JetStreamConfig, logger clog.Logger) (*natsJetStreamDriver, error) {
	js, err := jetstream.New(conn.GetClient())
	if err != nil {
		return nil, xerrors.Wrap(err, "failed to create jetstream context")
	}

	return &natsJetStreamDriver{
		js:     js,
		cfg:    cfg,
		logger: logger,
	}, nil
}

func (d *natsJetStreamDriver) Publish(ctx context.Context, subject string, data []byte, opts ...PublishOption) error {
	// 默认使用同步发送
	_, err := d.js.Publish(ctx, subject, data)
	return err
}

func (d *natsJetStreamDriver) Subscribe(ctx context.Context, subject string, handler Handler, opts ...SubscribeOption) (Subscription, error) {
	o := defaultSubscribeOptions()
	for _, opt := range opts {
		opt(&o)
	}

	// 自动创建 Stream (如果配置开启)
	if d.cfg != nil && d.cfg.AutoCreateStream {
		if err := d.ensureStream(ctx, subject); err != nil {
			return nil, xerrors.Wrapf(err, "failed to ensure stream for subject %s", subject)
		}
	}

	// 构造 ConsumerConfig
	consumerConfig := jetstream.ConsumerConfig{
		FilterSubject: subject,
	}

	if o.MaxInflight > 0 {
		consumerConfig.MaxAckPending = o.MaxInflight
	}

	// 设置队列组/持久化
	if o.QueueGroup != "" {
		consumerConfig.Durable = o.QueueGroup
		consumerConfig.AckPolicy = jetstream.AckExplicitPolicy
		// 如果指定了 QueueGroup，通常意味着是负载均衡消费，建议使用 Durable
	} else if o.DurableName != "" {
		consumerConfig.Durable = o.DurableName
		consumerConfig.AckPolicy = jetstream.AckExplicitPolicy
	} else {
		// 临时消费者
	}

	consumer, err := d.js.CreateOrUpdateConsumer(ctx, d.getStreamName(subject), consumerConfig)
	if err != nil {
		return nil, xerrors.Wrap(err, "failed to create consumer")
	}

	cons, err := consumer.Consume(func(msg jetstream.Msg) {
		m := &jetStreamMessage{msg: msg}

		// 执行用户逻辑
		err := handler(context.Background(), m)

		// 自动 Ack/Nak 处理
		if o.AutoAck {
			if err != nil {
				d.logger.Error("消息处理失败，执行 Nak", clog.String("subject", msg.Subject()), clog.Error(err))
				if o.AsyncAck {
					go func() { _ = msg.Nak() }()
				} else {
					if nakErr := msg.Nak(); nakErr != nil {
						d.logger.Error("Nak 失败", clog.Error(nakErr))
					}
				}
			} else {
				if o.AsyncAck {
					go func() { _ = msg.Ack() }()
				} else {
					if ackErr := msg.Ack(); ackErr != nil {
						d.logger.Error("Ack 失败", clog.Error(ackErr))
					}
				}
			}
		}
		// 如果 AutoAck 为 false，由用户在 handler 中手动 Ack/Nak
	})

	if err != nil {
		return nil, xerrors.Wrap(err, "failed to start consuming messages")
	}

	return &jetStreamSubscription{cons: cons}, nil
}

func (d *natsJetStreamDriver) Close() error {
	return nil
}

func (d *natsJetStreamDriver) getStreamName(subject string) string {
	// 简单实现：将 subject 中的非法字符替换，或直接作为 Stream 名 (NATS Stream 名有限制)
	// 示例中我们直接使用 subject 作为 Stream 名（假设它符合规范）
	return "S-" + subject
}

func (d *natsJetStreamDriver) ensureStream(ctx context.Context, subject string) error {
	streamName := d.getStreamName(subject)
	_, err := d.js.Stream(ctx, streamName)
	if err == nil {
		// 检查 subject 是否在 stream 的 subjects 中
		return nil
	}

	// 自动创建 Stream，覆盖该 subject
	_, err = d.js.CreateStream(ctx, jetstream.StreamConfig{
		Name:     streamName,
		Subjects: []string{subject},
	})
	return err
}

// -----------------------------------------------------------
// 消息与订阅封装
// -----------------------------------------------------------

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
	return nil
}

func (m *coreMessage) Nak() error {
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
	return true
}
