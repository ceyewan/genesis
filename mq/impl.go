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
	initializing  map[*subscriptionInit]struct{}
	initWG        sync.WaitGroup
	forced        bool // Protected by mu; also applies to subscriptions that return late.
	cleanupErrors []error
	lifecycleOnce sync.Once
	lifecycleDone chan struct{}
	lifecycleErr  error
	forceOnce     sync.Once
	forceErr      error

	publishCounter   metrics.Counter
	consumeCounter   metrics.Counter
	publishHistogram metrics.Histogram
	handleHistogram  metrics.Histogram
}

type subscriptionInit struct{ cancel context.CancelFunc }

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
		if opt != nil {
			opt(&o)
		}
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
		if opt != nil {
			opt(&o)
		}
	}

	if ctx == nil {
		return nil, xerrors.Wrap(ErrInvalidConfig, "subscribe context is nil")
	}
	ctx, cancel := context.WithCancel(ctx)
	pending := &subscriptionInit{cancel: cancel}
	m.mu.Lock()
	if m.closed.Load() {
		m.mu.Unlock()
		cancel()
		return nil, ErrClosed
	}
	if m.initializing == nil {
		m.initializing = make(map[*subscriptionInit]struct{})
	}
	m.initializing[pending] = struct{}{}
	// Register before transport I/O: delivery can start before Subscribe returns.
	m.initWG.Add(1)
	m.mu.Unlock()
	defer m.initWG.Done()

	sub, err := m.transport.Subscribe(ctx, topic, m.wrapHandler(topic, handler, o), o)
	m.mu.Lock()
	delete(m.initializing, pending)
	closed, forced := m.closed.Load(), m.forced
	if sub == nil {
		m.mu.Unlock()
		cancel()
		if err == nil {
			err = xerrors.Wrap(ErrInvalidConfig, "transport returned a nil subscription")
		}
		if closed {
			err = xerrors.Combine(ErrClosed, err)
		}
		return nil, err
	}
	// Also register late subscriptions so a concurrent Close can escalate a
	// graceful late Drain. initWG prevents cleanupWG.Wait from racing this Add.
	m.subscriptions[sub] = struct{}{}
	m.cleanupWG.Add(1)
	m.mu.Unlock()
	go func() {
		defer m.cleanupWG.Done()
		defer cancel()
		<-sub.Done()
		m.mu.Lock()
		delete(m.subscriptions, sub)
		m.mu.Unlock()
	}()
	if closed || err != nil {
		var cleanupErr error
		if forced || err != nil {
			cancel()
			cleanupErr = sub.Unsubscribe()
		} else {
			cleanupErr = sub.Drain(context.Background())
		}
		<-sub.Done()
		if cleanupErr != nil {
			m.mu.Lock()
			if m.closed.Load() {
				m.cleanupErrors = append(m.cleanupErrors, cleanupErr)
			}
			m.mu.Unlock()
		}
		if closed {
			err = xerrors.Combine(ErrClosed, err)
		}
		return nil, xerrors.Combine(err, cleanupErr)
	}
	return sub, nil
}

// Close 关闭 MQ（幂等）
func (m *mq) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Force-close remains available after any Drain caller has timed out.
	m.forceOnce.Do(func() {
		m.mu.Lock()
		m.closed.Store(true)
		m.forced = true
		cancels := make([]context.CancelFunc, 0, len(m.initializing))
		for pending := range m.initializing {
			cancels = append(cancels, pending.cancel)
		}
		subs := make([]Subscription, 0, len(m.subscriptions))
		for sub := range m.subscriptions {
			subs = append(subs, sub)
		}
		m.mu.Unlock()
		for _, cancel := range cancels {
			cancel()
		}
		var errs []error
		for _, sub := range subs {
			if err := sub.Unsubscribe(); err != nil {
				errs = append(errs, err)
			}
		}
		m.forceErr = xerrors.Combine(errs...)
	})
	return xerrors.Combine(m.forceErr, m.shutdown(ctx))
}

func (m *mq) Drain(ctx context.Context) error {
	if ctx == nil {
		return xerrors.Wrap(ErrInvalidConfig, "mq drain context is nil")
	}
	return m.shutdown(ctx)
}

func (m *mq) shutdown(ctx context.Context) error {
	m.lifecycleOnce.Do(func() {
		m.mu.Lock()
		m.closed.Store(true)
		// Capture ready subscriptions at the admission barrier. Initializers
		// that finish later see closed and perform their own tracked cleanup.
		var subs []Subscription
		if !m.forced {
			for sub := range m.subscriptions {
				subs = append(subs, sub)
			}
		}
		m.mu.Unlock()
		go func() {
			// The first caller's deadline must not control shared cleanup.
			m.lifecycleErr = m.shutdownSubscriptions(subs)
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

func (m *mq) shutdownSubscriptions(subs []Subscription) error {
	var errs []error
	var wg sync.WaitGroup
	var errsMu sync.Mutex
	for _, sub := range subs {
		wg.Go(func() {
			if err := sub.Drain(context.Background()); err != nil {
				errsMu.Lock()
				errs = append(errs, err)
				errsMu.Unlock()
			}
		})
	}
	// Stop existing intake first: a slow initializer must not delay every
	// ready subscription's Drain. Late results clean themselves up, and all
	// cleanupWG.Add calls happen before their matching initWG.Done.
	m.initWG.Wait()
	wg.Wait()
	m.cleanupWG.Wait()
	m.mu.Lock()
	errs = append(errs, m.cleanupErrors...)
	m.mu.Unlock()
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

// initMetrics 在构造时缓存 instrument；可观测性失败不阻断消息收发。
func (m *mq) initMetrics() {
	m.publishCounter, _ = m.meter.Counter(MetricPublishTotal, "Total number of messages published")
	m.consumeCounter, _ = m.meter.Counter(MetricConsumeTotal, "Total number of messages consumed")
	m.publishHistogram, _ = m.meter.Histogram(MetricPublishDuration, "Publish latency in seconds", metrics.WithUnit("s"))
	m.handleHistogram, _ = m.meter.Histogram(MetricHandleDuration, "Message handler duration in seconds", metrics.WithUnit("s"))
}

// recordPublishMetrics 记录发布指标
func (m *mq) recordPublishMetrics(ctx context.Context, topic string, err error, duration time.Duration) {
	status := "success"
	if err != nil {
		status = "error"
	}
	driver := string(m.driver)

	if m.publishCounter != nil {
		m.publishCounter.Inc(ctx, metrics.L(LabelTopic, topic), metrics.L(LabelStatus, status), metrics.L(LabelDriver, driver))
	}

	if m.publishHistogram != nil {
		m.publishHistogram.Record(ctx, duration.Seconds(), metrics.L(LabelTopic, topic), metrics.L(LabelDriver, driver))
	}
}

// recordConsumeMetrics 记录消费指标（含处理结果和驱动维度）
func (m *mq) recordConsumeMetrics(ctx context.Context, topic string, err error) {
	status := "success"
	if err != nil {
		status = "error"
	}
	if m.consumeCounter != nil {
		m.consumeCounter.Inc(ctx, metrics.L(LabelTopic, topic), metrics.L(LabelStatus, status), metrics.L(LabelDriver, string(m.driver)))
	}
}

// recordHandleDuration 记录处理耗时
func (m *mq) recordHandleDuration(ctx context.Context, topic string, duration time.Duration) {
	if m.handleHistogram != nil {
		m.handleHistogram.Record(ctx, duration.Seconds(), metrics.L(LabelTopic, topic), metrics.L(LabelDriver, string(m.driver)))
	}
}
