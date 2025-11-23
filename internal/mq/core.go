package mq

import (
	"context"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/connector"
	"github.com/ceyewan/genesis/pkg/mq/types"
	telemetrytypes "github.com/ceyewan/genesis/pkg/telemetry/types"
)

// coreClient NATS Core 模式实现
type coreClient struct {
	conn   *nats.Conn
	logger clog.Logger
	meter  telemetrytypes.Meter
	tracer telemetrytypes.Tracer
}

// NewCoreClient 创建 NATS Core 客户端
func NewCoreClient(conn connector.NATSConnector, logger clog.Logger, meter telemetrytypes.Meter, tracer telemetrytypes.Tracer) types.Client {
	return &coreClient{
		conn:   conn.GetClient(),
		logger: logger,
		meter:  meter,
		tracer: tracer,
	}
}

func (c *coreClient) Publish(ctx context.Context, subject string, data []byte) error {
	// NATS Core Publish 是发后即忘，非常快
	return c.conn.Publish(subject, data)
}

func (c *coreClient) Subscribe(ctx context.Context, subject string, handler types.Handler) (types.Subscription, error) {
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

func (c *coreClient) QueueSubscribe(ctx context.Context, subject string, queue string, handler types.Handler) (types.Subscription, error) {
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

func (c *coreClient) Request(ctx context.Context, subject string, data []byte, timeout time.Duration) (types.Message, error) {
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
