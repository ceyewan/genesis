package mqv2

import (
	"context"
	"sync"

	"github.com/nats-io/nats.go"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/xerrors"
)

// natsCoreTransport NATS Core 传输层实现
type natsCoreTransport struct {
	conn   *nats.Conn
	logger clog.Logger
}

// newNATSCoreTransport 创建 NATS Core Transport
func newNATSCoreTransport(conn connector.NATSConnector, logger clog.Logger) *natsCoreTransport {
	return &natsCoreTransport{
		conn:   conn.GetClient(),
		logger: logger,
	}
}

// Publish 发布消息
func (t *natsCoreTransport) Publish(ctx context.Context, topic string, data []byte, opts publishOptions) error {
	// NATS Core 不支持 context 超时控制，这里做简单检查
	if err := ctx.Err(); err != nil {
		return err
	}

	if len(opts.Headers) == 0 {
		return t.conn.Publish(topic, data)
	}

	msg := &nats.Msg{
		Subject: topic,
		Data:    data,
		Header:  headersToNATS(opts.Headers),
	}
	return t.conn.PublishMsg(msg)
}

// Subscribe 订阅消息
func (t *natsCoreTransport) Subscribe(ctx context.Context, topic string, handler Handler, opts subscribeOptions) (Subscription, error) {
	// 创建内部回调
	cb := func(msg *nats.Msg) {
		m := &natsCoreMessage{
			ctx:     ctx,
			msg:     msg,
			headers: headersFromNATS(msg.Header),
		}
		// 错误已在上层 wrapHandler 中处理
		_ = handler(m)
	}

	var sub *nats.Subscription
	var err error

	if opts.QueueGroup != "" {
		sub, err = t.conn.QueueSubscribe(topic, opts.QueueGroup, cb)
	} else {
		sub, err = t.conn.Subscribe(topic, cb)
	}

	if err != nil {
		return nil, xerrors.Wrapf(err, "subscribe to %s failed", topic)
	}

	return newNATSCoreSubscription(sub, ctx), nil
}

// Close 关闭 Transport
func (t *natsCoreTransport) Close() error {
	// 连接由 Connector 管理，这里不关闭
	return nil
}

// Capabilities 返回能力描述
func (t *natsCoreTransport) Capabilities() Capabilities {
	return CapabilitiesNATSCore
}

// ==================== Message 实现 ====================

// natsCoreMessage NATS Core 消息实现
type natsCoreMessage struct {
	ctx     context.Context
	msg     *nats.Msg
	headers Headers
}

func (m *natsCoreMessage) Context() context.Context {
	if m.ctx == nil {
		return context.Background()
	}
	return m.ctx
}

func (m *natsCoreMessage) Topic() string {
	return m.msg.Subject
}

func (m *natsCoreMessage) Data() []byte {
	return m.msg.Data
}

func (m *natsCoreMessage) Headers() Headers {
	return m.headers.Clone()
}

func (m *natsCoreMessage) Ack() error {
	// NATS Core 无持久化语义，Ack 为空操作
	return nil
}

func (m *natsCoreMessage) Nak() error {
	// NATS Core 不支持 Nak
	return nil
}

func (m *natsCoreMessage) ID() string {
	// NATS Core 没有消息 ID
	return ""
}

// ==================== Subscription 实现 ====================

// natsCoreSubscription NATS Core 订阅实现
type natsCoreSubscription struct {
	sub    *nats.Subscription
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

func newNATSCoreSubscription(sub *nats.Subscription, parentCtx context.Context) *natsCoreSubscription {
	ctx, cancel := context.WithCancel(parentCtx)
	s := &natsCoreSubscription{
		sub:    sub,
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
	}

	// 监听 context 取消
	go func() {
		<-ctx.Done()
		_ = s.sub.Unsubscribe()
		s.once.Do(func() { close(s.done) })
	}()

	return s
}

func (s *natsCoreSubscription) Unsubscribe() error {
	s.cancel()
	return nil
}

func (s *natsCoreSubscription) Done() <-chan struct{} {
	return s.done
}

// ==================== 辅助函数 ====================

// headersToNATS 将 Headers 转换为 NATS Header
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

// headersFromNATS 从 NATS Header 提取 Headers
func headersFromNATS(nh nats.Header) Headers {
	if len(nh) == 0 {
		return nil
	}
	h := make(Headers, len(nh))
	for k, v := range nh {
		if len(v) > 0 {
			h[k] = v[0] // 只取第一个值
		}
	}
	return h
}
