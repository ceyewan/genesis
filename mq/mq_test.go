package mq

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
)

// ============================================================
// Config 测试
// ============================================================

func TestConfig(t *testing.T) {
	t.Run("setDefaults 设置默认值", func(t *testing.T) {
		cfg := &Config{
			JetStream: &JetStreamConfig{
				StreamPrefix: "",
			},
			RedisStream: &RedisStreamConfig{},
		}
		cfg.setDefaults()

		require.Equal(t, "S-", cfg.JetStream.StreamPrefix)
		require.Equal(t, 30*time.Second, cfg.JetStream.AckWait)
		require.Equal(t, 30*time.Second, cfg.RedisStream.PendingIdle)
	})

	t.Run("validate 验证配置 - 成功", func(t *testing.T) {
		tests := []struct {
			name string
			cfg  *Config
		}{
			{
				name: "NATS JetStream",
				cfg:  &Config{Driver: DriverNATSJetStream},
			},
			{
				name: "Redis Stream",
				cfg:  &Config{Driver: DriverRedisStream},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := tt.cfg.validate()
				require.NoError(t, err)
			})
		}
	})

	t.Run("validate 验证配置 - 失败", func(t *testing.T) {
		t.Run("空驱动", func(t *testing.T) {
			cfg := &Config{}
			err := cfg.validate()
			require.Error(t, err)
		})

		t.Run("不支持的驱动", func(t *testing.T) {
			cfg := &Config{Driver: Driver("unknown")}
			err := cfg.validate()
			require.Error(t, err)
		})
	})
}

// ============================================================
// Driver 常量测试
// ============================================================

func TestDriverConstants(t *testing.T) {
	tests := []struct {
		name   string
		driver Driver
		want   string
	}{
		{"NATS JetStream", DriverNATSJetStream, "nats_jetstream"},
		{"Redis Stream", DriverRedisStream, "redis_stream"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, string(tt.driver))
		})
	}
}

// ============================================================
// New 函数测试
// ============================================================

func TestNew(t *testing.T) {
	t.Run("配置为空", func(t *testing.T) {
		mq, err := New(nil)
		require.Error(t, err)
		require.Nil(t, mq)
	})

	t.Run("驱动不支持", func(t *testing.T) {
		mq, err := New(&Config{Driver: Driver("unknown")})
		require.Error(t, err)
		require.Nil(t, mq)
	})

	t.Run("缺少 NATS 连接器", func(t *testing.T) {
		mq, err := New(&Config{Driver: DriverNATSJetStream})
		require.Error(t, err)
		require.Nil(t, mq)
	})

	t.Run("缺少 Redis 连接器", func(t *testing.T) {
		mq, err := New(&Config{Driver: DriverRedisStream})
		require.Error(t, err)
		require.Nil(t, mq)
	})

	t.Run("成功创建 NATS JetStream", func(t *testing.T) {
		mq, err := New(
			&Config{Driver: DriverNATSJetStream},
			WithNATSConnector(&mockNATSConnector{}),
		)
		require.NoError(t, err)
		require.NotNil(t, mq)
		_ = mq.Close()
	})

	t.Run("成功创建 Redis Stream", func(t *testing.T) {
		mq, err := New(
			&Config{Driver: DriverRedisStream},
			WithRedisConnector(&mockRedisConnector{}),
		)
		require.NoError(t, err)
		require.NotNil(t, mq)
		_ = mq.Close()
	})
}

// ============================================================
// Option 测试
// ============================================================

func TestOptions(t *testing.T) {
	t.Run("WithLogger", func(t *testing.T) {
		logger := clog.Discard()
		mq, err := New(
			&Config{Driver: DriverNATSJetStream},
			WithNATSConnector(&mockNATSConnector{}),
			WithLogger(logger),
		)
		require.NoError(t, err)
		require.NotNil(t, mq)
		_ = mq.Close()
	})

	t.Run("WithMeter", func(t *testing.T) {
		meter := metrics.Discard()
		mq, err := New(
			&Config{Driver: DriverNATSJetStream},
			WithNATSConnector(&mockNATSConnector{}),
			WithMeter(meter),
		)
		require.NoError(t, err)
		require.NotNil(t, mq)
		_ = mq.Close()
	})

	t.Run("默认 Logger 和 Meter", func(t *testing.T) {
		// 不传任何选项，应该使用默认值
		mq, err := New(
			&Config{Driver: DriverNATSJetStream},
			WithNATSConnector(&mockNATSConnector{}),
		)
		require.NoError(t, err)
		require.NotNil(t, mq)
		_ = mq.Close()
	})
}

// ============================================================
// Publish 测试
// ============================================================

func TestMQ_Publish(t *testing.T) {
	t.Run("发布成功", func(t *testing.T) {
		transport := &mockTransport{}
		mq := newMQ(transport, clog.Discard(), metrics.Discard())

		ctx := context.Background()
		err := mq.Publish(ctx, "test.subject", []byte("test data"))

		require.NoError(t, err)
		require.True(t, transport.publishCalled)
		require.Equal(t, "test.subject", transport.lastTopic)
		require.Equal(t, []byte("test data"), transport.lastData)
	})

	t.Run("发布失败", func(t *testing.T) {
		transport := &mockTransport{publishError: errors.New("publish failed")}
		mq := newMQ(transport, clog.Discard(), metrics.Discard())

		ctx := context.Background()
		err := mq.Publish(ctx, "test.subject", []byte("test data"))

		require.Error(t, err)
	})

	t.Run("带 Headers 发布", func(t *testing.T) {
		transport := &mockTransport{}
		mq := newMQ(transport, clog.Discard(), metrics.Discard())

		ctx := context.Background()
		headers := Headers{"trace-id": "abc123"}
		err := mq.Publish(ctx, "test.subject", []byte("test data"), WithHeaders(headers))

		require.NoError(t, err)
		require.Equal(t, headers, transport.lastPublishOpts.Headers)
	})

	t.Run("带单个 Header 发布", func(t *testing.T) {
		transport := &mockTransport{}
		mq := newMQ(transport, clog.Discard(), metrics.Discard())

		ctx := context.Background()
		err := mq.Publish(ctx, "test.subject", []byte("test data"), WithHeader("x-key", "x-value"))

		require.NoError(t, err)
		require.Equal(t, "x-value", transport.lastPublishOpts.Headers["x-key"])
	})
}

// ============================================================
// Subscribe 测试
// ============================================================

func TestMQ_Subscribe(t *testing.T) {
	t.Run("订阅成功", func(t *testing.T) {
		transport := &mockTransport{}
		mq := newMQ(transport, clog.Discard(), metrics.Discard())

		ctx := context.Background()
		handler := func(msg Message) error { return nil }

		sub, err := mq.Subscribe(ctx, "test.subject", handler)

		require.NoError(t, err)
		require.NotNil(t, sub)
		require.True(t, transport.subscribeCalled)
	})

	t.Run("订阅失败", func(t *testing.T) {
		transport := &mockTransport{subscribeError: errors.New("subscribe failed")}
		mq := newMQ(transport, clog.Discard(), metrics.Discard())

		ctx := context.Background()
		handler := func(msg Message) error { return nil }

		sub, err := mq.Subscribe(ctx, "test.subject", handler)

		require.Error(t, err)
		require.Nil(t, sub)
	})

	t.Run("带 QueueGroup 订阅", func(t *testing.T) {
		transport := &mockTransport{}
		mq := newMQ(transport, clog.Discard(), metrics.Discard())

		ctx := context.Background()
		handler := func(msg Message) error { return nil }

		sub, err := mq.Subscribe(ctx, "test.subject", handler, WithQueueGroup("test-group"))

		require.NoError(t, err)
		require.NotNil(t, sub)
		require.Equal(t, "test-group", transport.lastSubscribeOpts.QueueGroup)
	})

	t.Run("手动确认模式（默认）", func(t *testing.T) {
		transport := &mockTransport{}
		mq := newMQ(transport, clog.Discard(), metrics.Discard())

		ctx := context.Background()
		handler := func(msg Message) error { return nil }

		sub, err := mq.Subscribe(ctx, "test.subject", handler)

		require.NoError(t, err)
		require.NotNil(t, sub)
		require.False(t, transport.lastSubscribeOpts.AutoAck)
	})

	t.Run("开启自动确认", func(t *testing.T) {
		transport := &mockTransport{}
		mq := newMQ(transport, clog.Discard(), metrics.Discard())

		ctx := context.Background()
		handler := func(msg Message) error { return nil }

		sub, err := mq.Subscribe(ctx, "test.subject", handler, WithAutoAck())

		require.NoError(t, err)
		require.NotNil(t, sub)
		require.True(t, transport.lastSubscribeOpts.AutoAck)
	})

	t.Run("带 Durable 订阅", func(t *testing.T) {
		transport := &mockTransport{}
		mq := newMQ(transport, clog.Discard(), metrics.Discard())

		ctx := context.Background()
		handler := func(msg Message) error { return nil }

		sub, err := mq.Subscribe(ctx, "test.subject", handler, WithDurable("durable-name"))

		require.NoError(t, err)
		require.NotNil(t, sub)
		require.Equal(t, "durable-name", transport.lastSubscribeOpts.DurableName)
	})

	t.Run("设置 BatchSize", func(t *testing.T) {
		transport := &mockTransport{}
		mq := newMQ(transport, clog.Discard(), metrics.Discard())

		ctx := context.Background()
		handler := func(msg Message) error { return nil }

		sub, err := mq.Subscribe(ctx, "test.subject", handler, WithBatchSize(50))

		require.NoError(t, err)
		require.NotNil(t, sub)
		require.Equal(t, 50, transport.lastSubscribeOpts.BatchSize)
	})

	t.Run("设置 MaxInflight", func(t *testing.T) {
		transport := &mockTransport{}
		mq := newMQ(transport, clog.Discard(), metrics.Discard())

		ctx := context.Background()
		handler := func(msg Message) error { return nil }

		sub, err := mq.Subscribe(ctx, "test.subject", handler, WithMaxInflight(100))

		require.NoError(t, err)
		require.NotNil(t, sub)
		require.Equal(t, 100, transport.lastSubscribeOpts.MaxInflight)
	})
}

// ============================================================
// Close 测试
// ============================================================

func TestMQ_Close(t *testing.T) {
	t.Run("关闭成功", func(t *testing.T) {
		transport := &mockTransport{}
		mq := newMQ(transport, clog.Discard(), metrics.Discard())

		err := mq.Close()

		require.NoError(t, err)
		require.True(t, transport.closeCalled)
	})

	t.Run("关闭失败", func(t *testing.T) {
		transport := &mockTransport{closeError: errors.New("close failed")}
		mq := newMQ(transport, clog.Discard(), metrics.Discard())

		err := mq.Close()

		require.Error(t, err)
	})

	t.Run("Close 幂等", func(t *testing.T) {
		transport := &mockTransport{}
		mq := newMQ(transport, clog.Discard(), metrics.Discard())

		require.NoError(t, mq.Close())
		require.NoError(t, mq.Close()) // 第二次关闭不应报错
	})

	t.Run("关闭后 Publish 返回 ErrClosed", func(t *testing.T) {
		transport := &mockTransport{}
		mq := newMQ(transport, clog.Discard(), metrics.Discard())

		require.NoError(t, mq.Close())
		err := mq.Publish(context.Background(), "topic", []byte("data"))
		require.ErrorIs(t, err, ErrClosed)
	})

	t.Run("关闭后 Subscribe 返回 ErrClosed", func(t *testing.T) {
		transport := &mockTransport{}
		mq := newMQ(transport, clog.Discard(), metrics.Discard())

		require.NoError(t, mq.Close())
		_, err := mq.Subscribe(context.Background(), "topic", func(msg Message) error { return nil })
		require.ErrorIs(t, err, ErrClosed)
	})
}

// ============================================================
// AutoAck 行为测试
// ============================================================

func TestMQ_AutoAckBehavior(t *testing.T) {
	t.Run("AutoAck 模式 Handler 成功时自动 Ack", func(t *testing.T) {
		testMsg := &mockMessage{}
		m := &mq{logger: clog.Discard(), meter: metrics.Discard(), driver: DriverNATSJetStream}
		wrapped := m.wrapHandler("test.topic", func(msg Message) error {
			return nil
		}, subscribeOptions{AutoAck: true})

		err := wrapped(testMsg)
		require.NoError(t, err)
		require.True(t, testMsg.ackCalled, "成功时应该调用 Ack")
		require.False(t, testMsg.nakCalled)
	})

	t.Run("AutoAck 模式 Handler 失败时自动 Nak", func(t *testing.T) {
		testMsg := &mockMessage{}
		m := &mq{logger: clog.Discard(), meter: metrics.Discard(), driver: DriverNATSJetStream}
		wrapped := m.wrapHandler("test.topic", func(msg Message) error {
			return errors.New("handler failed")
		}, subscribeOptions{AutoAck: true})

		err := wrapped(testMsg)
		require.Error(t, err)
		require.True(t, testMsg.nakCalled, "失败时应该调用 Nak")
		require.False(t, testMsg.ackCalled)
	})

	t.Run("ManualAck 模式不自动调用 Ack/Nak", func(t *testing.T) {
		testMsg := &mockMessage{}
		m := &mq{logger: clog.Discard(), meter: metrics.Discard(), driver: DriverNATSJetStream}
		wrapped := m.wrapHandler("test.topic", func(msg Message) error {
			return nil
		}, subscribeOptions{AutoAck: false})

		err := wrapped(testMsg)
		require.NoError(t, err)
		require.False(t, testMsg.ackCalled, "ManualAck 模式不应自动 Ack")
		require.False(t, testMsg.nakCalled)
	})

	t.Run("AutoAck 模式 Nak 返回 ErrNotSupported 时不应记录错误", func(t *testing.T) {
		// 模拟 Redis 消息，Nak 返回 ErrNotSupported
		testMsg := &mockMessageNakNotSupported{}
		m := &mq{logger: clog.Discard(), meter: metrics.Discard(), driver: DriverRedisStream}
		wrapped := m.wrapHandler("test.topic", func(msg Message) error {
			return errors.New("handler failed")
		}, subscribeOptions{AutoAck: true})

		// 不应 panic，ErrNotSupported 应被静默忽略
		err := wrapped(testMsg)
		require.Error(t, err)
	})
}

// ============================================================
// Headers 测试
// ============================================================

func TestHeaders(t *testing.T) {
	t.Run("Clone 返回深拷贝", func(t *testing.T) {
		original := Headers{"key1": "value1", "key2": "value2"}
		cloned := original.Clone()

		require.Equal(t, original, cloned)

		// 修改克隆不影响原始
		cloned["key1"] = "modified"
		require.Equal(t, "value1", original["key1"])
		require.Equal(t, "modified", cloned["key1"])
	})

	t.Run("nil Headers Clone 返回 nil", func(t *testing.T) {
		var h Headers
		cloned := h.Clone()
		require.Nil(t, cloned)
	})

	t.Run("Get 获取值", func(t *testing.T) {
		h := Headers{"key": "value"}
		require.Equal(t, "value", h.Get("key"))
		require.Equal(t, "", h.Get("nonexistent"))
	})

	t.Run("nil Headers Get 返回空字符串", func(t *testing.T) {
		var h Headers
		require.Equal(t, "", h.Get("key"))
	})

	t.Run("Set 设置值", func(t *testing.T) {
		h := Headers{}
		h.Set("key", "value")
		require.Equal(t, "value", h["key"])

		h.Set("key", "new-value")
		require.Equal(t, "new-value", h["key"])
	})
}

// ============================================================
// 默认选项测试
// ============================================================

func TestDefaultOptions(t *testing.T) {
	t.Run("默认发布选项", func(t *testing.T) {
		opts := defaultPublishOptions()
		require.Nil(t, opts.Headers)
	})

	t.Run("默认订阅选项", func(t *testing.T) {
		opts := defaultSubscribeOptions()
		require.False(t, opts.AutoAck) // 默认手动确认
		require.Equal(t, 10, opts.BatchSize)
		require.Empty(t, opts.QueueGroup)
		require.Empty(t, opts.DurableName)
		require.Equal(t, 0, opts.MaxInflight)
	})
}

// ============================================================
// 指标常量测试
// ============================================================

func TestMetricConstants(t *testing.T) {
	require.Equal(t, "mq.publish.total", MetricPublishTotal)
	require.Equal(t, "mq.publish.duration", MetricPublishDuration)
	require.Equal(t, "mq.consume.total", MetricConsumeTotal)
	require.Equal(t, "mq.handle.duration", MetricHandleDuration)
}

func TestLabelConstants(t *testing.T) {
	require.Equal(t, "topic", LabelTopic)
	require.Equal(t, "status", LabelStatus)
	require.Equal(t, "driver", LabelDriver)
}

// ============================================================
// Mock 实现（用于测试）
// ============================================================

// mockTransport 是 Transport 的 mock 实现
type mockTransport struct {
	publishCalled     bool
	subscribeCalled   bool
	closeCalled       bool
	publishError      error
	subscribeError    error
	closeError        error
	lastTopic         string
	lastData          []byte
	lastPublishOpts   publishOptions
	lastSubscribeOpts subscribeOptions
	handler           Handler
}

func (m *mockTransport) Publish(ctx context.Context, topic string, data []byte, opts publishOptions) error {
	m.publishCalled = true
	m.lastTopic = topic
	m.lastData = data
	m.lastPublishOpts = opts
	return m.publishError
}

func (m *mockTransport) Subscribe(subscribeCtx context.Context, topic string, handler Handler, opts subscribeOptions) (Subscription, error) {
	m.subscribeCalled = true
	m.handler = handler
	m.lastSubscribeOpts = opts
	if m.subscribeError != nil {
		return nil, m.subscribeError
	}
	return &mockSubscription{}, nil
}

func (m *mockTransport) Close() error {
	m.closeCalled = true
	return m.closeError
}

// mockSubscription 是 Subscription 的 mock 实现
type mockSubscription struct {
	unsubscribed bool
}

func (m *mockSubscription) Unsubscribe() error {
	m.unsubscribed = true
	return nil
}

func (m *mockSubscription) Done() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

// mockMessage 是 Message 的 mock 实现
type mockMessage struct {
	ackCalled bool
	nakCalled bool
}

func (m *mockMessage) Context() context.Context {
	return context.Background()
}

func (m *mockMessage) Topic() string {
	return "test.topic"
}

func (m *mockMessage) Data() []byte {
	return []byte("test data")
}

func (m *mockMessage) Headers() Headers {
	return Headers{"trace-id": "abc123"}
}

func (m *mockMessage) Ack() error {
	m.ackCalled = true
	return nil
}

func (m *mockMessage) Nak() error {
	m.nakCalled = true
	return nil
}

func (m *mockMessage) ID() string {
	return "msg-123"
}

// mockNATSConnector 是 NATSConnector 的 mock 实现
type mockNATSConnector struct{}

func (m *mockNATSConnector) Connect(ctx context.Context) error {
	return nil
}

func (m *mockNATSConnector) Close() error {
	return nil
}

func (m *mockNATSConnector) HealthCheck(ctx context.Context) error {
	return nil
}

func (m *mockNATSConnector) IsHealthy() bool {
	return true
}

func (m *mockNATSConnector) Name() string {
	return "mock-nats"
}

func (m *mockNATSConnector) GetClient() *nats.Conn {
	return &nats.Conn{}
}

// mockRedisConnector 是 RedisConnector 的 mock 实现
type mockRedisConnector struct{}

func (m *mockRedisConnector) Connect(ctx context.Context) error {
	return nil
}

func (m *mockRedisConnector) Close() error {
	return nil
}

func (m *mockRedisConnector) HealthCheck(ctx context.Context) error {
	return nil
}

func (m *mockRedisConnector) IsHealthy() bool {
	return true
}

func (m *mockRedisConnector) Name() string {
	return "mock-redis"
}

func (m *mockRedisConnector) GetClient() *redis.Client {
	return &redis.Client{}
}

// ============================================================
// 辅助函数
// ============================================================

// mockMessageNakNotSupported 模拟 Nak 返回 ErrNotSupported 的消息（如 Redis Stream）
type mockMessageNakNotSupported struct {
	mockMessage
}

func (m *mockMessageNakNotSupported) Nak() error {
	return ErrNotSupported
}

// newMQ 创建一个用于测试的 MQ 实例
func newMQ(transport Transport, logger clog.Logger, meter metrics.Meter) MQ {
	return &mq{
		transport: transport,
		logger:    logger,
		meter:     meter,
		driver:    DriverNATSJetStream,
	}
}
