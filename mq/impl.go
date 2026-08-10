package mq

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/xerrors"
)

// mq 是 MQ 接口的实现
type mq struct {
	transport transport
	logger    clog.Logger
	meter     metrics.Meter
	driver    Driver
	closed    atomic.Bool

	mu            sync.Mutex
	subscriptions map[Subscription]struct{}
	cleanupWG     sync.WaitGroup
	lifecycleOnce sync.Once
	lifecycleDone chan struct{}
	lifecycleErr  error
}

// Publish 发布消息
func (m *mq) Publish(ctx context.Context, topic string, data []byte, opts ...PublishOption) error {
	if m.closed.Load() {
		return ErrClosed
	}
	if topic == "" {
		return xerrors.Wrap(ErrInvalidConfig, "publish topic is empty")
	}

	// 应用选项
	o := defaultPublishOptions()
	for _, opt := range opts {
		opt(&o)
	}

	// 发布消息
	start := time.Now()
	err := m.transport.Publish(ctx, topic, data, o)

	// 记录指标
	m.recordPublishMetrics(ctx, topic, err, time.Since(start))

	return err
}

// Subscribe 订阅消息
func (m *mq) Subscribe(ctx context.Context, topic string, handler Handler, opts ...SubscribeOption) (Subscription, error) {
	if m.closed.Load() {
		return nil, ErrClosed
	}
	if topic == "" {
		return nil, xerrors.Wrap(ErrInvalidConfig, "subscribe topic is empty")
	}
	if handler == nil {
		return nil, xerrors.Wrap(ErrInvalidConfig, "subscribe handler is nil")
	}

	// 应用选项
	o := defaultSubscribeOptions()
	for _, opt := range opts {
		opt(&o)
	}

	wrappedHandler := m.wrapHandler(topic, handler, o)
	sub, err := m.transport.Subscribe(ctx, topic, wrappedHandler, o)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	if m.closed.Load() {
		m.mu.Unlock()
		_ = sub.Unsubscribe()
		return nil, ErrClosed
	}
	m.subscriptions[sub] = struct{}{}
	m.cleanupWG.Add(1)
	m.mu.Unlock()
	go func() {
		defer m.cleanupWG.Done()
		<-sub.Done()
		m.mu.Lock()
		delete(m.subscriptions, sub)
		m.mu.Unlock()
	}()
	return sub, nil
}

// Close 关闭 MQ（幂等）
func (m *mq) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return m.shutdown(ctx, false)
}

func (m *mq) Drain(ctx context.Context) error {
	if ctx == nil {
		return xerrors.New("mq drain context is nil")
	}
	return m.shutdown(ctx, true)
}

func (m *mq) shutdown(ctx context.Context, graceful bool) error {
	m.lifecycleOnce.Do(func() {
		m.closed.Store(true)
		go func() {
			m.lifecycleErr = m.shutdownSubscriptions(ctx, graceful)
			close(m.lifecycleDone)
		}()
	})

	select {
	case <-m.lifecycleDone:
		return m.lifecycleErr
	case <-ctx.Done():
		return xerrors.Wrap(ctx.Err(), "mq shutdown canceled")
	}
}

func (m *mq) shutdownSubscriptions(ctx context.Context, graceful bool) error {
	m.mu.Lock()
	subs := make([]Subscription, 0, len(m.subscriptions))
	for sub := range m.subscriptions {
		subs = append(subs, sub)
	}
	m.mu.Unlock()

	var errs []error
	for _, sub := range subs {
		if graceful {
			if err := sub.Drain(ctx); err != nil {
				errs = append(errs, err)
			}
			continue
		}
		if err := sub.Unsubscribe(); err != nil {
			errs = append(errs, err)
		}
	}
	if !graceful {
		for _, sub := range subs {
			select {
			case <-sub.Done():
			case <-ctx.Done():
				errs = append(errs, xerrors.Wrap(ctx.Err(), "wait for mq subscription"))
			}
		}
	}
	m.cleanupWG.Wait()
	if err := m.transport.Close(); err != nil {
		errs = append(errs, err)
	}
	return xerrors.Combine(errs...)
}

// wrapHandler 包装 Handler，添加统一的指标、日志和自动确认逻辑
func (m *mq) wrapHandler(topic string, handler Handler, opts subscribeOptions) Handler {
	return func(msg Message) (resultErr error) {
		msgCtx := msg.Context()
		start := time.Now()
		// 执行用户 Handler
		resultErr = handler(msg)
		m.recordHandleDuration(msgCtx, topic, time.Since(start))

		// AutoAck 的 Ack 结果也是消费结果的一部分。延迟到确认逻辑结束后记录，
		// 避免 Handler 成功但 Ack 失败时误报 success。
		defer func() {
			m.recordConsumeMetrics(msgCtx, topic, resultErr)
		}()

		// 自动确认逻辑（统一在上层处理）
		if opts.AutoAck {
			if resultErr == nil {
				if ackErr := msg.Ack(); ackErr != nil {
					m.logger.Error("auto ack failed",
						clog.String("topic", topic),
						clog.String("msg_id", msg.ID()),
						clog.Error(ackErr),
					)
					resultErr = ackErr
					return resultErr
				}
			} else {
				// Handler 返回错误时调用 Nak 触发重新投递
				// 注意：Redis Stream 的 Nak 返回 ErrNotSupported，这是预期行为，不记录错误
				if nakErr := msg.Nak(); nakErr != nil && !errors.Is(nakErr, ErrNotSupported) {
					m.logger.Error("auto nak failed",
						clog.String("topic", topic),
						clog.String("msg_id", msg.ID()),
						clog.Error(nakErr),
					)
					resultErr = errors.Join(resultErr, nakErr)
					return resultErr
				}
			}
		}
		return resultErr
	}
}

// recordPublishMetrics 记录发布指标
func (m *mq) recordPublishMetrics(ctx context.Context, topic string, err error, duration time.Duration) {
	status := "success"
	if err != nil {
		status = "error"
	}
	driver := string(m.driver)

	if counter, counterErr := m.meter.Counter(MetricPublishTotal, "Total number of messages published"); counterErr == nil {
		counter.Inc(ctx, metrics.L(LabelTopic, topic), metrics.L(LabelStatus, status), metrics.L(LabelDriver, driver))
	}

	if histogram, histErr := m.meter.Histogram(MetricPublishDuration, "Publish latency in seconds", metrics.WithUnit("s")); histErr == nil {
		histogram.Record(ctx, duration.Seconds(), metrics.L(LabelTopic, topic), metrics.L(LabelDriver, driver))
	}
}

// recordConsumeMetrics 记录消费指标（含处理结果和驱动维度）
func (m *mq) recordConsumeMetrics(ctx context.Context, topic string, err error) {
	status := "success"
	if err != nil {
		status = "error"
	}
	if counter, counterErr := m.meter.Counter(MetricConsumeTotal, "Total number of messages consumed"); counterErr == nil {
		counter.Inc(ctx, metrics.L(LabelTopic, topic), metrics.L(LabelStatus, status), metrics.L(LabelDriver, string(m.driver)))
	}
}

// recordHandleDuration 记录处理耗时
func (m *mq) recordHandleDuration(ctx context.Context, topic string, duration time.Duration) {
	if histogram, err := m.meter.Histogram(MetricHandleDuration, "Message handler duration in seconds", metrics.WithUnit("s")); err == nil {
		histogram.Record(ctx, duration.Seconds(), metrics.L(LabelTopic, topic), metrics.L(LabelDriver, string(m.driver)))
	}
}
