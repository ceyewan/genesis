package mq

import (
	"context"
	"sync"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/xerrors"
)

type client struct {
	driver driver
	logger clog.Logger
	meter  metrics.Meter
}

func newClient(driver driver, logger clog.Logger, meter metrics.Meter) Client {
	return &client{
		driver: driver,
		logger: logger,
		meter:  meter,
	}
}

func (c *client) Publish(ctx context.Context, subject string, data []byte, opts ...PublishOption) error {
	// 记录发布指标
	if c.meter != nil {
		publishCounter, err := c.meter.Counter("mq.publish.total", "Total number of messages published")
		if err == nil {
			publishCounter.Inc(ctx, metrics.L("subject", subject))
		}
	}

	err := c.driver.Publish(ctx, subject, data, opts...)
	if err != nil && c.meter != nil {
		errorCounter, err := c.meter.Counter("mq.publish.errors", "Total number of publish errors")
		if err == nil {
			errorCounter.Inc(ctx,
				metrics.L("subject", subject),
				metrics.L("error", "publish_failed"),
			)
		}
	}
	return err
}

func (c *client) Subscribe(ctx context.Context, subject string, handler Handler, opts ...SubscribeOption) (Subscription, error) {
	// 这里可以添加全局中间件
	return c.driver.Subscribe(ctx, subject, handler, opts...)
}

func (c *client) SubscribeChan(ctx context.Context, subject string, opts ...SubscribeOption) (<-chan Message, Subscription, error) {
	// 解析选项以获取 buffer size
	o := defaultSubscribeOptions()
	for _, opt := range opts {
		opt(&o)
	}

	ch := make(chan Message, o.BufferSize)

	// 定义 Handler 将消息转发到 Channel
	handler := func(ctx context.Context, msg Message) error {
		select {
		case ch <- msg:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		default:
			// 如果 Channel 满了，记录警告并丢弃消息（或者阻塞，取决于需求）
			// 这里选择丢弃并记录警告，避免阻塞 Driver 的回调协程
			c.logger.Warn("SubscribeChan buffer full, message dropped", clog.String("subject", subject))
			return xerrors.New("buffer full")
		}
	}

	// 订阅
	sub, err := c.driver.Subscribe(ctx, subject, handler, opts...)
	if err != nil {
		close(ch)
		return ch, nil, err
	}

	// 封装 Subscription 以在 Unsubscribe 时关闭 Channel
	return ch, &chanSubscription{
		Subscription: sub,
		ch:           ch,
	}, nil
}

func (c *client) Close() error {
	return c.driver.Close()
}

// chanSubscription 封装 Subscription，增加关闭 Channel 的能力
type chanSubscription struct {
	Subscription
	ch   chan Message
	once sync.Once
}

func (s *chanSubscription) Unsubscribe() error {
	err := s.Subscription.Unsubscribe()
	// 安全关闭 Channel，确保 range 能够结束
	s.once.Do(func() {
		close(s.ch)
	})
	return err
}
