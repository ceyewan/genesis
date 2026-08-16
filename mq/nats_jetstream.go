package mq

import (
	"context"
	"crypto/sha256"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

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
	if t.cfg.AutoCreateStream {
		if err := t.ensureStream(ctx, topic); err != nil {
			return xerrors.Wrapf(err, "ensure stream for %s failed", topic)
		}
	}

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

// headersToNATS 将 Headers 转换为 nats.Header
func headersToNATS(h Headers) nats.Header {
	if len(h) == 0 {
		return nil
	}
	nh := make(nats.Header, len(h))
	for k, v := range h {
		nh.Set(k, v)
	}
	return nh
}

// headersFromNATS 将 nats.Header 转换为 Headers
func headersFromNATS(nh nats.Header) Headers {
	if len(nh) == 0 {
		return nil
	}
	h := make(Headers, len(nh))
	for k := range nh {
		h[k] = nh.Get(k)
	}
	return h
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
		MaxDeliver:    t.cfg.MaxDeliver,
	}
	switch opts.StartID {
	case "", "0":
		consumerCfg.DeliverPolicy = jetstream.DeliverAllPolicy
	case "$":
		consumerCfg.DeliverPolicy = jetstream.DeliverNewPolicy
	default:
		parts := strings.Split(opts.StartID, ":")
		sequence, parseErr := strconv.ParseUint(parts[len(parts)-1], 10, 64)
		if parseErr != nil || sequence == 0 {
			return nil, xerrors.Wrapf(ErrInvalidConfig, "invalid JetStream start ID: %s", opts.StartID)
		}
		consumerCfg.DeliverPolicy = jetstream.DeliverByStartSequencePolicy
		consumerCfg.OptStartSeq = sequence
	}

	// 设置 Durable 名称
	// 同一 Durable 的多个消费者实例会竞争消费（负载均衡）
	if opts.QueueGroup != "" {
		consumerCfg.Durable = durableConsumerName(opts.QueueGroup, topic)
	} else if opts.DurableName != "" {
		consumerCfg.Durable = durableConsumerName(opts.DurableName, topic)
	}

	// 设置 AckWait（等待 Ack 的超时时间）
	if t.cfg.AckWait > 0 {
		consumerCfg.AckWait = t.cfg.AckWait
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
	subCtx, cancel := context.WithCancel(ctx)
	consumeOpts := make([]jetstream.PullConsumeOpt, 0, 1)
	if opts.batchSizeSet {
		consumeOpts = append(consumeOpts, jetstream.PullMaxMessages(opts.BatchSize))
	}
	cons, err := consumer.Consume(func(msg jetstream.Msg) {
		m := &jetStreamMessage{
			msg:     msg,
			ctx:     subCtx,
			headers: headersFromNATS(msg.Headers()),
		}
		// 错误已在上层 wrapHandler 中处理
		_ = handler(m)
	}, consumeOpts...)
	if err != nil {
		cancel()
		return nil, xerrors.Wrap(err, "start consuming failed")
	}

	return newJetStreamSubscription(cons, subCtx, cancel), nil
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
		return t.ensureStreamSubject(ctx, stream, streamName, topic)
	}
	if !xerrors.Is(err, jetstream.ErrStreamNotFound) {
		return xerrors.Wrapf(err, "get stream %s failed", streamName)
	}

	// Stream 不存在，创建新 Stream
	_, err = t.js.CreateStream(ctx, jetstream.StreamConfig{
		Name:      streamName,
		Subjects:  []string{topic},
		Retention: natsRetention(t.cfg.Retention),
		Storage:   natsStorage(t.cfg.Storage),
		MaxAge:    t.cfg.MaxAge,
		MaxBytes:  t.cfg.MaxBytes,
		Replicas:  t.cfg.Replicas,
	})
	if err == nil {
		return nil
	}
	if !xerrors.Is(err, jetstream.ErrStreamNameAlreadyInUse) {
		return err
	}

	// 另一个实例可能刚刚创建了同名 Stream。创建竞争的失败方仍需重新读取，
	// 并确保新 Stream 包含自己当前使用的 subject。
	stream, err = t.js.Stream(ctx, streamName)
	if err != nil {
		return xerrors.Wrapf(err, "get concurrently created stream %s failed", streamName)
	}
	return t.ensureStreamSubject(ctx, stream, streamName, topic)
}

func (t *natsJetStreamTransport) ensureStreamSubject(
	ctx context.Context,
	stream jetstream.Stream,
	streamName string,
	topic string,
) error {
	info, err := stream.Info(ctx)
	if err != nil {
		return xerrors.Wrap(err, "get stream info failed")
	}
	for _, subject := range info.Config.Subjects {
		if subject == topic || matchesWildcard(subject, topic) {
			return nil
		}
	}

	// 使用原有配置的全量拷贝，只修改 Subjects，避免覆盖运维配置的保留策略。
	updatedConfig := info.Config
	updatedConfig.Subjects = append(updatedConfig.Subjects, topic)
	if _, err := t.js.UpdateStream(ctx, updatedConfig); err != nil {
		return xerrors.Wrapf(err, "update stream %s to add subject %s failed", streamName, topic)
	}
	t.logger.Info("Added subject to existing stream",
		clog.String("stream", streamName),
		clog.String("subject", topic),
	)
	return nil
}

func natsRetention(retention StreamRetention) jetstream.RetentionPolicy {
	switch retention {
	case StreamRetentionInterest:
		return jetstream.InterestPolicy
	case StreamRetentionWorkQueue:
		return jetstream.WorkQueuePolicy
	default:
		return jetstream.LimitsPolicy
	}
}

func natsStorage(storage StreamStorage) jetstream.StorageType {
	if storage == StreamStorageMemory {
		return jetstream.MemoryStorage
	}
	return jetstream.FileStorage
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

// durableConsumerName scopes a logical queue/durable identity to its topic.
// JetStream durable names are stream-scoped, while Genesis streams may contain
// multiple topics. Without the suffix, subscribing the same logical identity to
// a second topic updates the first consumer's FilterSubject.
func durableConsumerName(name, topic string) string {
	hash := sha256.Sum256([]byte(topic))
	return fmt.Sprintf("%s-%x", sanitizeName(name), hash[:8])
}

// ==================== Message 实现 ====================

// jetStreamMessage JetStream 消息实现
type jetStreamMessage struct {
	msg     jetstream.Msg
	ctx     context.Context
	headers Headers
	ackMu   sync.Mutex
	acked   bool
}

var _ ProgressMessage = (*jetStreamMessage)(nil)

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
	m.ackMu.Lock()
	defer m.ackMu.Unlock()
	if m.acked {
		return nil
	}
	if err := m.msg.Ack(); err != nil {
		return err
	}
	m.acked = true
	return nil
}

func (m *jetStreamMessage) Nak() error {
	return m.msg.Nak()
}

func (m *jetStreamMessage) NakWithDelay(delay time.Duration) error {
	if delay < 0 {
		return xerrors.Wrap(ErrInvalidConfig, "nak delay must not be negative")
	}
	return m.msg.NakWithDelay(delay)
}

func (m *jetStreamMessage) InProgress() error {
	m.ackMu.Lock()
	defer m.ackMu.Unlock()
	if m.acked {
		return nil
	}
	return m.msg.InProgress()
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
	cons      jetstream.ConsumeContext
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	doneOnce  sync.Once
	stopOnce  sync.Once
	drainOnce sync.Once
}

func newJetStreamSubscription(cons jetstream.ConsumeContext, ctx context.Context, cancel context.CancelFunc) *jetStreamSubscription {
	s := &jetStreamSubscription{
		cons:   cons,
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
	}

	go func() {
		select {
		case <-ctx.Done():
			s.cons.Stop()
		case <-s.cons.Closed():
		}
		<-s.cons.Closed()
		s.cancel()
		s.doneOnce.Do(func() { close(s.done) })
	}()

	return s
}

func (s *jetStreamSubscription) Unsubscribe() error {
	s.stopOnce.Do(func() {
		s.cancel()
		s.cons.Stop()
	})
	return nil
}

func (s *jetStreamSubscription) Drain(ctx context.Context) error {
	if ctx == nil {
		return xerrors.New("mq subscription drain context is nil")
	}
	s.drainOnce.Do(s.cons.Drain)
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		_ = s.Unsubscribe()
		return xerrors.Wrap(ctx.Err(), "drain jetstream subscription")
	}
}

func (s *jetStreamSubscription) Done() <-chan struct{} {
	return s.done
}
