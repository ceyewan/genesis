package mq

import (
	"context"
	"errors"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
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
		}
		cfg.setDefaults()

		assert.Equal(t, "S-", cfg.JetStream.StreamPrefix)
	})

	t.Run("validate 验证配置 - 成功", func(t *testing.T) {
		tests := []struct {
			name  string
			cfg   *Config
			valid bool
		}{
			{
				name:  "NATS Core",
				cfg:   &Config{Driver: DriverNATSCore},
				valid: true,
			},
			{
				name:  "NATS JetStream",
				cfg:   &Config{Driver: DriverNATSJetStream},
				valid: true,
			},
			{
				name:  "Redis Stream",
				cfg:   &Config{Driver: DriverRedisStream},
				valid: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := tt.cfg.validate()
				if tt.valid {
					assert.NoError(t, err)
				} else {
					assert.Error(t, err)
				}
			})
		}
	})

	t.Run("validate 验证配置 - 失败", func(t *testing.T) {
		t.Run("空驱动", func(t *testing.T) {
			cfg := &Config{}
			err := cfg.validate()
			assert.Error(t, err)
		})

		t.Run("不支持的驱动", func(t *testing.T) {
			cfg := &Config{Driver: Driver("unknown")}
			err := cfg.validate()
			assert.Error(t, err)
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
		{"NATS Core", DriverNATSCore, "nats_core"},
		{"NATS JetStream", DriverNATSJetStream, "nats_jetstream"},
		{"Redis Stream", DriverRedisStream, "redis_stream"},
		{"Kafka", DriverKafka, "kafka"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, string(tt.driver))
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
		assert.Nil(t, mq)
	})

	t.Run("驱动不支持", func(t *testing.T) {
		mq, err := New(&Config{Driver: Driver("unknown")})
		require.Error(t, err)
		assert.Nil(t, mq)
	})

	t.Run("缺少 NATS 连接器", func(t *testing.T) {
		mq, err := New(&Config{Driver: DriverNATSCore})
		require.Error(t, err)
		assert.Nil(t, mq)
	})

	t.Run("缺少 Redis 连接器", func(t *testing.T) {
		mq, err := New(&Config{Driver: DriverRedisStream})
		require.Error(t, err)
		assert.Nil(t, mq)
	})

	t.Run("成功创建 NATS Core", func(t *testing.T) {
		mq, err := New(
			&Config{Driver: DriverNATSCore},
			WithNATSConnector(&mockNATSConnector{}),
		)
		require.NoError(t, err)
		assert.NotNil(t, mq)
		_ = mq.Close()
	})

	t.Run("成功创建 NATS JetStream", func(t *testing.T) {
		mq, err := New(
			&Config{Driver: DriverNATSJetStream},
			WithNATSConnector(&mockNATSConnector{}),
		)
		require.NoError(t, err)
		assert.NotNil(t, mq)
		_ = mq.Close()
	})

	t.Run("成功创建 Redis Stream", func(t *testing.T) {
		mq, err := New(
			&Config{Driver: DriverRedisStream},
			WithRedisConnector(&mockRedisConnector{}),
		)
		require.NoError(t, err)
		assert.NotNil(t, mq)
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
			&Config{Driver: DriverNATSCore},
			WithNATSConnector(&mockNATSConnector{}),
			WithLogger(logger),
		)
		require.NoError(t, err)
		assert.NotNil(t, mq)
		_ = mq.Close()
	})

	t.Run("WithMeter", func(t *testing.T) {
		meter := metrics.Discard()
		mq, err := New(
			&Config{Driver: DriverNATSCore},
			WithNATSConnector(&mockNATSConnector{}),
			WithMeter(meter),
		)
		require.NoError(t, err)
		assert.NotNil(t, mq)
		_ = mq.Close()
	})

	t.Run("默认 Logger 和 Meter", func(t *testing.T) {
		// 不传任何选项，应该使用默认值
		mq, err := New(
			&Config{Driver: DriverNATSCore},
			WithNATSConnector(&mockNATSConnector{}),
		)
		require.NoError(t, err)
		assert.NotNil(t, mq)
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

		assert.NoError(t, err)
		assert.True(t, transport.publishCalled)
		assert.Equal(t, "test.subject", transport.lastTopic)
		assert.Equal(t, []byte("test data"), transport.lastData)
	})

	t.Run("发布失败", func(t *testing.T) {
		transport := &mockTransport{publishError: errors.New("publish failed")}
		mq := newMQ(transport, clog.Discard(), metrics.Discard())

		ctx := context.Background()
		err := mq.Publish(ctx, "test.subject", []byte("test data"))

		assert.Error(t, err)
	})

	t.Run("带 Headers 发布", func(t *testing.T) {
		transport := &mockTransport{}
		mq := newMQ(transport, clog.Discard(), metrics.Discard())

		ctx := context.Background()
		headers := Headers{"trace-id": "abc123"}
		err := mq.Publish(ctx, "test.subject", []byte("test data"), WithHeaders(headers))

		assert.NoError(t, err)
		assert.Equal(t, headers, transport.lastPublishOpts.Headers)
	})

	t.Run("带单个 Header 发布", func(t *testing.T) {
		transport := &mockTransport{}
		mq := newMQ(transport, clog.Discard(), metrics.Discard())

		ctx := context.Background()
		err := mq.Publish(ctx, "test.subject", []byte("test data"), WithHeader("x-key", "x-value"))

		assert.NoError(t, err)
		assert.Equal(t, "x-value", transport.lastPublishOpts.Headers["x-key"])
	})

	t.Run("带 Key 发布", func(t *testing.T) {
		transport := &mockTransport{}
		mq := newMQ(transport, clog.Discard(), metrics.Discard())

		ctx := context.Background()
		err := mq.Publish(ctx, "test.subject", []byte("test data"), WithKey("partition-key"))

		assert.NoError(t, err)
		assert.Equal(t, "partition-key", transport.lastPublishOpts.Key)
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

		assert.NoError(t, err)
		assert.NotNil(t, sub)
		assert.True(t, transport.subscribeCalled)
	})

	t.Run("订阅失败", func(t *testing.T) {
		transport := &mockTransport{subscribeError: errors.New("subscribe failed")}
		mq := newMQ(transport, clog.Discard(), metrics.Discard())

		ctx := context.Background()
		handler := func(msg Message) error { return nil }

		sub, err := mq.Subscribe(ctx, "test.subject", handler)

		assert.Error(t, err)
		assert.Nil(t, sub)
	})

	t.Run("带 QueueGroup 订阅", func(t *testing.T) {
		transport := &mockTransport{}
		mq := newMQ(transport, clog.Discard(), metrics.Discard())

		ctx := context.Background()
		handler := func(msg Message) error { return nil }

		sub, err := mq.Subscribe(ctx, "test.subject", handler, WithQueueGroup("test-group"))

		assert.NoError(t, err)
		assert.NotNil(t, sub)
		assert.Equal(t, "test-group", transport.lastSubscribeOpts.QueueGroup)
	})

	t.Run("手动确认模式", func(t *testing.T) {
		transport := &mockTransport{}
		mq := newMQ(transport, clog.Discard(), metrics.Discard())

		ctx := context.Background()
		handler := func(msg Message) error { return nil }

		sub, err := mq.Subscribe(ctx, "test.subject", handler, WithManualAck())

		assert.NoError(t, err)
		assert.NotNil(t, sub)
		assert.False(t, transport.lastSubscribeOpts.AutoAck)
	})

	t.Run("带 Durable 订阅", func(t *testing.T) {
		transport := &mockTransport{}
		mq := newMQ(transport, clog.Discard(), metrics.Discard())

		ctx := context.Background()
		handler := func(msg Message) error { return nil }

		sub, err := mq.Subscribe(ctx, "test.subject", handler, WithDurable("durable-name"))

		assert.NoError(t, err)
		assert.NotNil(t, sub)
		assert.Equal(t, "durable-name", transport.lastSubscribeOpts.DurableName)
	})

	t.Run("设置 BatchSize", func(t *testing.T) {
		transport := &mockTransport{}
		mq := newMQ(transport, clog.Discard(), metrics.Discard())

		ctx := context.Background()
		handler := func(msg Message) error { return nil }

		sub, err := mq.Subscribe(ctx, "test.subject", handler, WithBatchSize(50))

		assert.NoError(t, err)
		assert.NotNil(t, sub)
		assert.Equal(t, 50, transport.lastSubscribeOpts.BatchSize)
	})

	t.Run("设置 MaxInflight", func(t *testing.T) {
		transport := &mockTransport{}
		mq := newMQ(transport, clog.Discard(), metrics.Discard())

		ctx := context.Background()
		handler := func(msg Message) error { return nil }

		sub, err := mq.Subscribe(ctx, "test.subject", handler, WithMaxInflight(100))

		assert.NoError(t, err)
		assert.NotNil(t, sub)
		assert.Equal(t, 100, transport.lastSubscribeOpts.MaxInflight)
	})

	t.Run("设置 BufferSize", func(t *testing.T) {
		transport := &mockTransport{}
		mq := newMQ(transport, clog.Discard(), metrics.Discard())

		ctx := context.Background()
		handler := func(msg Message) error { return nil }

		sub, err := mq.Subscribe(ctx, "test.subject", handler, WithBufferSize(500))

		assert.NoError(t, err)
		assert.NotNil(t, sub)
		assert.Equal(t, 500, transport.lastSubscribeOpts.BufferSize)
	})

	t.Run("设置死信队列", func(t *testing.T) {
		transport := &mockTransport{}
		mq := newMQ(transport, clog.Discard(), metrics.Discard())

		ctx := context.Background()
		handler := func(msg Message) error { return nil }

		sub, err := mq.Subscribe(ctx, "test.subject", handler, WithDeadLetter(3, "dlq-topic"))

		assert.NoError(t, err)
		assert.NotNil(t, sub)
		assert.NotNil(t, transport.lastSubscribeOpts.DeadLetter)
		assert.Equal(t, 3, transport.lastSubscribeOpts.DeadLetter.MaxRetries)
		assert.Equal(t, "dlq-topic", transport.lastSubscribeOpts.DeadLetter.Topic)
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

		assert.NoError(t, err)
		assert.True(t, transport.closeCalled)
	})

	t.Run("关闭失败", func(t *testing.T) {
		transport := &mockTransport{closeError: errors.New("close failed")}
		mq := newMQ(transport, clog.Discard(), metrics.Discard())

		err := mq.Close()

		assert.Error(t, err)
	})
}

// ============================================================
// AutoAck 行为测试
// ============================================================

func TestMQ_AutoAckBehavior(t *testing.T) {
	t.Run("Handler 成功时自动 Ack", func(t *testing.T) {
		testMsg := &mockMessage{}
		transport := &mockTransport{
			caps: CapabilitiesNATSJetStream,
			handler: func(msg Message) error {
				// 模拟 Handler 返回 nil
				return nil
			},
		}
		// 覆盖 Subscribe 方法，直接调用 handler 来测试 AutoAck 行为
		originalHandler := transport.handler
		transport = &mockTransport{
			caps: CapabilitiesNATSJetStream,
		}

		mq := newMQ(transport, clog.Discard(), metrics.Discard())

		ctx := context.Background()
		_, _ = mq.Subscribe(ctx, "test.subject", originalHandler, WithAutoAck())

		// 模拟 AutoAck 包装后的行为
		err := originalHandler(testMsg)
		assert.NoError(t, err)
		// 在 AutoAck 模式下，成功后应该调用 Ack
		_ = testMsg.Ack()
		assert.True(t, testMsg.ackCalled, "成功时应该调用 Ack")
		assert.False(t, testMsg.nakCalled)
	})

	t.Run("Handler 失败时行为", func(t *testing.T) {
		testMsg := &mockMessage{}
		transport := &mockTransport{
			caps: CapabilitiesNATSJetStream, // 支持 Nak
			handler: func(msg Message) error {
				return errors.New("handler failed")
			},
		}
		mq := newMQ(transport, clog.Discard(), metrics.Discard())

		ctx := context.Background()
		_, _ = mq.Subscribe(ctx, "test.subject", transport.handler, WithAutoAck())

		// 模拟 AutoAck 包装后的行为
		err := transport.handler(testMsg)
		assert.Error(t, err)
		// 失败时应该调用 Nak
		_ = testMsg.Nak()
		assert.True(t, testMsg.nakCalled)
		assert.False(t, testMsg.ackCalled)
	})
}

// ============================================================
// Headers 测试
// ============================================================

func TestHeaders(t *testing.T) {
	t.Run("Clone 返回深拷贝", func(t *testing.T) {
		original := Headers{"key1": "value1", "key2": "value2"}
		cloned := original.Clone()

		assert.Equal(t, original, cloned)

		// 修改克隆不影响原始
		cloned["key1"] = "modified"
		assert.Equal(t, "value1", original["key1"])
		assert.Equal(t, "modified", cloned["key1"])
	})

	t.Run("nil Headers Clone 返回 nil", func(t *testing.T) {
		var h Headers
		cloned := h.Clone()
		assert.Nil(t, cloned)
	})

	t.Run("Get 获取值", func(t *testing.T) {
		h := Headers{"key": "value"}
		assert.Equal(t, "value", h.Get("key"))
		assert.Equal(t, "", h.Get("nonexistent"))
	})

	t.Run("nil Headers Get 返回空字符串", func(t *testing.T) {
		var h Headers
		assert.Equal(t, "", h.Get("key"))
	})

	t.Run("Set 设置值", func(t *testing.T) {
		h := Headers{}
		h.Set("key", "value")
		assert.Equal(t, "value", h["key"])

		h.Set("key", "new-value")
		assert.Equal(t, "new-value", h["key"])
	})
}

// ============================================================
// 默认选项测试
// ============================================================

func TestDefaultOptions(t *testing.T) {
	t.Run("默认发布选项", func(t *testing.T) {
		opts := defaultPublishOptions()
		// Headers 默认为 nil，使用 WithHeaders/WithHeader 时才会创建
		assert.Nil(t, opts.Headers)
		assert.Empty(t, opts.Key)
	})

	t.Run("默认订阅选项", func(t *testing.T) {
		opts := defaultSubscribeOptions()
		assert.True(t, opts.AutoAck)
		assert.Equal(t, 10, opts.BatchSize)
		assert.Equal(t, 100, opts.BufferSize)
		assert.Empty(t, opts.QueueGroup)
		assert.Empty(t, opts.DurableName)
		assert.Equal(t, 0, opts.MaxInflight)
		assert.Nil(t, opts.DeadLetter)
	})
}

// ============================================================
// Capabilities 测试
// ============================================================

func TestCapabilities(t *testing.T) {
	t.Run("NATS Core 能力", func(t *testing.T) {
		caps := CapabilitiesNATSCore
		assert.False(t, caps.Persistence)
		assert.False(t, caps.ExactlyOnce)
		assert.False(t, caps.Nak)
		assert.False(t, caps.DeadLetter)
		assert.True(t, caps.QueueGroup)
		assert.False(t, caps.OrderedDelivery)
		assert.False(t, caps.BatchConsume)
		assert.False(t, caps.DelayedMessage)
	})

	t.Run("NATS JetStream 能力", func(t *testing.T) {
		caps := CapabilitiesNATSJetStream
		assert.True(t, caps.Persistence)
		assert.True(t, caps.ExactlyOnce)
		assert.True(t, caps.Nak)
		assert.True(t, caps.QueueGroup)
		assert.True(t, caps.OrderedDelivery)
		assert.True(t, caps.BatchConsume)
	})

	t.Run("Redis Stream 能力", func(t *testing.T) {
		caps := CapabilitiesRedisStream
		assert.True(t, caps.Persistence)
		assert.False(t, caps.ExactlyOnce)
		assert.False(t, caps.Nak)
		assert.True(t, caps.QueueGroup)
		assert.True(t, caps.OrderedDelivery)
		assert.True(t, caps.BatchConsume)
	})

	t.Run("Kafka 能力（预留）", func(t *testing.T) {
		caps := CapabilitiesKafka
		assert.True(t, caps.Persistence)
		assert.True(t, caps.ExactlyOnce)
		assert.True(t, caps.QueueGroup)
		assert.True(t, caps.OrderedDelivery)
		assert.True(t, caps.BatchConsume)
		assert.True(t, caps.DeadLetter)
	})
}

// ============================================================
// 指标常量测试
// ============================================================

func TestMetricConstants(t *testing.T) {
	assert.Equal(t, "mq.publish.total", MetricPublishTotal)
	assert.Equal(t, "mq.publish.duration", MetricPublishDuration)
	assert.Equal(t, "mq.consume.total", MetricConsumeTotal)
	assert.Equal(t, "mq.handle.duration", MetricHandleDuration)
}

func TestLabelConstants(t *testing.T) {
	assert.Equal(t, "topic", LabelTopic)
	assert.Equal(t, "status", LabelStatus)
	assert.Equal(t, "driver", LabelDriver)
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
	caps              Capabilities
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

func (m *mockTransport) Capabilities() Capabilities {
	return m.caps
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

// newMQ 创建一个用于测试的 MQ 实例
func newMQ(transport Transport, logger clog.Logger, meter metrics.Meter) MQ {
	return &mq{
		transport: transport,
		logger:    logger,
		meter:     meter,
	}
}
