package mq

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
)

// mq 是 MQ 接口的实现
type mq struct {
	transport Transport
	logger    clog.Logger
	meter     metrics.Meter
	driver    Driver
	closed    atomic.Bool
}

// Publish 发布消息
func (m *mq) Publish(ctx context.Context, topic string, data []byte, opts ...PublishOption) error {
	if m.closed.Load() {
		return ErrClosed
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

	// 应用选项
	o := defaultSubscribeOptions()
	for _, opt := range opts {
		opt(&o)
	}

	wrappedHandler := m.wrapHandler(topic, handler, o)
	return m.transport.Subscribe(ctx, topic, wrappedHandler, o)
}

// Close 关闭 MQ（幂等）
func (m *mq) Close() error {
	if m.closed.Swap(true) {
		return nil // 已经关闭，幂等
	}
	return m.transport.Close()
}

// wrapHandler 包装 Handler，添加统一的指标、日志和自动确认逻辑
func (m *mq) wrapHandler(topic string, handler Handler, opts subscribeOptions) Handler {
	return func(msg Message) error {
		start := time.Now()
		// 执行用户 Handler
		err := handler(msg)
		// 在 handler 执行后记录指标，才能带上处理结果
		m.recordConsumeMetrics(msg.Context(), topic, err)
		m.recordHandleDuration(msg.Context(), topic, time.Since(start))

		// 自动确认逻辑（统一在上层处理）
		if opts.AutoAck {
			if err == nil {
				if ackErr := msg.Ack(); ackErr != nil {
					m.logger.Error("auto ack failed",
						clog.String("topic", topic),
						clog.String("msg_id", msg.ID()),
						clog.Error(ackErr),
					)
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
				}
			}
		}
		return err
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
