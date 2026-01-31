package mq

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/xerrors"
)

// natsJetStreamTransport NATS JetStream 传输层实现
type natsJetStreamTransport struct {
	js     jetstream.JetStream
	cfg    *JetStreamConfig
	logger clog.Logger
}

// newNATSJetStreamTransport 创建 JetStream Transport
func newNATSJetStreamTransport(conn connector.NATSConnector, cfg *JetStreamConfig, logger clog.Logger) (*natsJetStreamTransport, error) {
	js, err := jetstream.New(conn.GetClient())
	if err != nil {
		return nil, xerrors.Wrap(err, "create JetStream context failed")
	}

	if cfg == nil {
		cfg = &JetStreamConfig{
			StreamPrefix: "S-",
		}
	}

	return &natsJetStreamTransport{
		js:     js,
		cfg:    cfg,
		logger: logger,
	}, nil
}

// Publish 发布消息
func (t *natsJetStreamTransport) Publish(ctx context.Context, topic string, data []byte, opts publishOptions) error {
	if len(opts.Headers) == 0 {
		_, err := t.js.Publish(ctx, topic, data)
		return err
	}

	msg := &nats.Msg{
		Subject: topic,
		Data:    data,
		Header:  headersToNATS(opts.Headers),
	}
	_, err := t.js.PublishMsg(ctx, msg)
	return err
}

// Subscribe 订阅消息
func (t *natsJetStreamTransport) Subscribe(ctx context.Context, topic string, handler Handler, opts subscribeOptions) (Subscription, error) {
	// 自动创建/更新 Stream（如果配置开启）
	if t.cfg.AutoCreateStream {
		if err := t.ensureStream(ctx, topic); err != nil {
			return nil, xerrors.Wrapf(err, "ensure stream for %s failed", topic)
		}
	}

	streamName := t.getStreamName(topic)

	// 构造 Consumer 配置
	//
	// JetStream v2 API 的 consumer.Consume() 是 pull-based 消费模式。
	// 负载均衡机制：多个消费者实例使用相同的 Durable 名称时，JetStream 会自动
	// 在它们之间分发消息（每条消息只会被一个实例处理）。
	// 注意：DeliverGroup 仅对 push consumer 生效，pull consumer 不需要设置。
	consumerCfg := jetstream.ConsumerConfig{
		AckPolicy:     jetstream.AckExplicitPolicy,
		FilterSubject: topic,
	}

	// 设置 Durable 名称
	// 同一 Durable 的多个消费者实例会竞争消费（负载均衡）
	if opts.QueueGroup != "" {
		consumerCfg.Durable = sanitizeName(opts.QueueGroup)
	} else if opts.DurableName != "" {
		consumerCfg.Durable = sanitizeName(opts.DurableName)
	}

	// 设置 MaxAckPending（背压控制）
	if opts.MaxInflight > 0 {
		consumerCfg.MaxAckPending = opts.MaxInflight
	}

	// 创建或更新 Consumer
	consumer, err := t.js.CreateOrUpdateConsumer(ctx, streamName, consumerCfg)
	if err != nil {
		return nil, xerrors.Wrapf(err, "create consumer for %s failed", topic)
	}

	// 启动消费
	cons, err := consumer.Consume(func(msg jetstream.Msg) {
		m := &jetStreamMessage{
			msg:     msg,
			ctx:     ctx,
			headers: headersFromNATS(msg.Headers()),
		}
		// 错误已在上层 wrapHandler 中处理
		_ = handler(m)
	})
	if err != nil {
		return nil, xerrors.Wrap(err, "start consuming failed")
	}

	return newJetStreamSubscription(cons, ctx), nil
}

// Capabilities 返回能力描述
func (t *natsJetStreamTransport) Capabilities() Capabilities {
	return CapabilitiesNATSJetStream
}

// Close 关闭 Transport
func (t *natsJetStreamTransport) Close() error {
	return nil
}

// getStreamName 根据 topic 生成 Stream 名称
//
// 策略：取 topic 第一段作为 Stream 基础名（如 orders.created -> S-orders）
// 这样同一业务域的消息可以共享 Stream，但需要配合 ensureStream 动态添加 subjects
func (t *natsJetStreamTransport) getStreamName(topic string) string {
	baseName := strings.Split(topic, ".")[0] // 提取基础名称（去掉通配符部分）
	return t.cfg.StreamPrefix + sanitizeName(baseName)
}

// ensureStream 确保 Stream 存在并包含指定 topic
//
// 如果 Stream 已存在但不包含当前 topic，会更新 Stream 配置添加该 topic
func (t *natsJetStreamTransport) ensureStream(ctx context.Context, topic string) error {
	streamName := t.getStreamName(topic)

	// 检查 Stream 是否已存在
	stream, err := t.js.Stream(ctx, streamName)
	if err == nil {
		// Stream 存在，检查是否需要添加新的 subject
		info, infoErr := stream.Info(ctx)
		if infoErr != nil {
			return xerrors.Wrap(infoErr, "get stream info failed")
		}

		// 检查 topic 是否已在 subjects 中
		for _, sub := range info.Config.Subjects {
			if sub == topic || matchesWildcard(sub, topic) {
				return nil // 已包含该 topic
			}
		}

		// 需要添加新的 subject
		// 重要：使用原有配置的全量拷贝，只修改 Subjects，避免其他配置（Storage/Retention/MaxMsgs等）被重置
		updatedConfig := info.Config
		updatedConfig.Subjects = append(updatedConfig.Subjects, topic)
		_, err = t.js.UpdateStream(ctx, updatedConfig)
		if err != nil {
			return xerrors.Wrapf(err, "update stream %s to add subject %s failed", streamName, topic)
		}
		t.logger.Info("added subject to existing stream",
			clog.String("stream", streamName),
			clog.String("subject", topic),
		)
		return nil
	}

	// Stream 不存在，创建新 Stream
	_, err = t.js.CreateStream(ctx, jetstream.StreamConfig{
		Name:     streamName,
		Subjects: []string{topic},
	})
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return err
	}
	return nil
}

// matchesWildcard 检查通配符 subject 是否匹配 topic
// 例如 "orders.*" 匹配 "orders.created"
func matchesWildcard(pattern, topic string) bool {
	// 简单实现：处理 * 和 > 通配符
	if pattern == topic {
		return true
	}
	patternParts := strings.Split(pattern, ".")
	topicParts := strings.Split(topic, ".")

	for i, p := range patternParts {
		if p == ">" {
			return true // > 匹配剩余所有
		}
		if i >= len(topicParts) {
			return false
		}
		if p != "*" && p != topicParts[i] {
			return false
		}
	}
	return len(patternParts) == len(topicParts)
}

// sanitizeName 清理名称，移除不合法字符
var invalidChars = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

func sanitizeName(name string) string {
	return invalidChars.ReplaceAllString(name, "_")
}

// ==================== Message 实现 ====================

// jetStreamMessage JetStream 消息实现
type jetStreamMessage struct {
	msg     jetstream.Msg
	ctx     context.Context
	headers Headers
}

func (m *jetStreamMessage) Context() context.Context {
	if m.ctx == nil {
		return context.Background()
	}
	return m.ctx
}

func (m *jetStreamMessage) Topic() string {
	return m.msg.Subject()
}

func (m *jetStreamMessage) Data() []byte {
	return m.msg.Data()
}

func (m *jetStreamMessage) Headers() Headers {
	return m.headers.Clone()
}

func (m *jetStreamMessage) Ack() error {
	return m.msg.Ack()
}

func (m *jetStreamMessage) Nak() error {
	return m.msg.Nak()
}

func (m *jetStreamMessage) ID() string {
	meta, err := m.msg.Metadata()
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%s:%d", meta.Stream, meta.Sequence.Stream)
}

// ==================== Subscription 实现 ====================

// jetStreamSubscription JetStream 订阅实现
type jetStreamSubscription struct {
	cons   jetstream.ConsumeContext
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

func newJetStreamSubscription(cons jetstream.ConsumeContext, parentCtx context.Context) *jetStreamSubscription {
	ctx, cancel := context.WithCancel(parentCtx)
	s := &jetStreamSubscription{
		cons:   cons,
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
	}

	go func() {
		<-ctx.Done()
		s.cons.Stop()
		<-s.cons.Closed()
		s.once.Do(func() { close(s.done) })
	}()

	return s
}

func (s *jetStreamSubscription) Unsubscribe() error {
	s.cancel()
	return nil
}

func (s *jetStreamSubscription) Done() <-chan struct{} {
	return s.done
}
