package mq

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/metrics"
)

func TestNewRejectsUnconnectedConnectors(t *testing.T) {
	t.Parallel()

	redisConn, err := connector.NewRedis(&connector.RedisConfig{Addr: "127.0.0.1:6379"})
	require.NoError(t, err)
	_, err = New(&Config{Driver: DriverRedisStream}, WithRedisConnector(redisConn))
	require.ErrorIs(t, err, connector.ErrClientNil)

	natsConn, err := connector.NewNATS(&connector.NATSConfig{URL: "nats://127.0.0.1:4222"})
	require.NoError(t, err)
	_, err = New(&Config{Driver: DriverNATSJetStream}, WithNATSConnector(natsConn))
	require.ErrorIs(t, err, connector.ErrClientNil)
}

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
		require.Equal(t, 5, cfg.JetStream.MaxDeliver)
		require.Equal(t, StreamRetentionLimits, cfg.JetStream.Retention)
		require.Equal(t, StreamStorageFile, cfg.JetStream.Storage)
		require.Equal(t, 1, cfg.JetStream.Replicas)
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
			require.ErrorIs(t, err, ErrInvalidConfig)
		})

		t.Run("不支持的驱动", func(t *testing.T) {
			cfg := &Config{Driver: Driver("unknown")}
			err := cfg.validate()
			require.Error(t, err)
		})

		t.Run("invalid JetStream limits", func(t *testing.T) {
			cfg := &Config{Driver: DriverNATSJetStream, JetStream: &JetStreamConfig{MaxDeliver: -1}}
			cfg.setDefaults()
			require.ErrorIs(t, cfg.validate(), ErrInvalidConfig)
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

	t.Run("构造器复制配置后应用默认值", func(t *testing.T) {
		jsCfg := &JetStreamConfig{AutoCreateStream: true}
		cfg := &Config{Driver: DriverNATSJetStream, JetStream: jsCfg}
		q, err := New(cfg, WithNATSConnector(&mockNATSConnector{}))
		require.NoError(t, err)
		t.Cleanup(func() { _ = q.Close() })

		require.Empty(t, jsCfg.StreamPrefix)
		require.Zero(t, jsCfg.AckWait)
		require.Zero(t, jsCfg.MaxDeliver)
		internal := q.(*mq).transport.(*natsJetStreamTransport).cfg
		require.Equal(t, "S-", internal.StreamPrefix)
		require.Equal(t, 5, internal.MaxDeliver)
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
	t.Run("空 topic 返回分类错误", func(t *testing.T) {
		transport := &mockTransport{}
		q := newMQ(transport, clog.Discard(), metrics.Discard())

		err := q.Publish(context.Background(), "", []byte("test data"))
		require.ErrorIs(t, err, ErrInvalidConfig)
		require.False(t, transport.publishCalled)
	})

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
	t.Run("空 topic 返回分类错误", func(t *testing.T) {
		transport := &mockTransport{}
		q := newMQ(transport, clog.Discard(), metrics.Discard())

		sub, err := q.Subscribe(context.Background(), "", func(Message) error { return nil })
		require.ErrorIs(t, err, ErrInvalidConfig)
		require.Nil(t, sub)
		require.False(t, transport.subscribeCalled)
	})

	t.Run("nil Handler 返回分类错误", func(t *testing.T) {
		transport := &mockTransport{}
		q := newMQ(transport, clog.Discard(), metrics.Discard())

		sub, err := q.Subscribe(context.Background(), "test.subject", nil)
		require.ErrorIs(t, err, ErrInvalidConfig)
		require.Nil(t, sub)
		require.False(t, transport.subscribeCalled)
	})

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

func TestMQCloseDoesNotCloseBorrowedConnector(t *testing.T) {
	conn := &mockRedisConnector{}
	q, err := New(
		&Config{Driver: DriverRedisStream},
		WithRedisConnector(conn),
	)
	require.NoError(t, err)

	require.NoError(t, q.Close())
	require.NoError(t, q.Close())
	require.Zero(t, conn.closeCalls.Load())
	require.NotNil(t, conn.GetClient())
}

func TestMQ_DrainConcurrent(t *testing.T) {
	t.Parallel()

	transport := &mockTransport{}
	q := newMQ(transport, clog.Discard(), metrics.Discard())
	_, err := q.Subscribe(context.Background(), "topic", func(Message) error { return nil })
	require.NoError(t, err)

	const callers = 32
	var wg sync.WaitGroup
	errs := make(chan error, callers)
	wg.Add(callers)
	for range callers {
		go func() {
			defer wg.Done()
			errs <- q.Drain(context.Background())
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.True(t, transport.closeCalled)
	require.ErrorIs(t, q.Publish(context.Background(), "topic", nil), ErrClosed)
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

	t.Run("Ack 失败返回错误并记录消费失败", func(t *testing.T) {
		ackErr := errors.New("ack failed")
		testMsg := &mockMessage{ackError: ackErr}
		meter := newConsumeRecordingMeter()
		m := &mq{logger: clog.Discard(), meter: meter, driver: DriverNATSJetStream}
		wrapped := m.wrapHandler("test.topic", func(Message) error {
			return nil
		}, subscribeOptions{AutoAck: true})

		err := wrapped(testMsg)
		require.ErrorIs(t, err, ackErr)
		require.True(t, testMsg.ackCalled)
		require.Equal(t, []string{"error"}, meter.recordedStatuses())
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

func TestSubscribeStartOptions(t *testing.T) {
	opts := defaultSubscribeOptions()
	require.Equal(t, "0", opts.StartID)
	FromLatest()(&opts)
	require.Equal(t, "$", opts.StartID)
	FromID("123-0")(&opts)
	require.Equal(t, "123-0", opts.StartID)
	FromBeginning()(&opts)
	require.Equal(t, "0", opts.StartID)
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
		require.False(t, opts.batchSizeSet)
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

// mockTransport 是 transport 的 mock 实现
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

func (m *mockSubscription) Drain(ctx context.Context) error {
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
	ackError  error
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
	return m.ackError
}

func (m *mockMessage) Nak() error {
	m.nakCalled = true
	return nil
}

func (m *mockMessage) NakWithDelay(delay time.Duration) error {
	m.nakCalled = true
	return nil
}

func (m *mockMessage) ID() string {
	return "msg-123"
}

// mockNATSConnector 是 NATSConnector 的 mock 实现
type mockNATSConnector struct {
	closeCalls atomic.Int32
}

func (m *mockNATSConnector) Connect(ctx context.Context) error {
	return nil
}

func (m *mockNATSConnector) Close() error {
	m.closeCalls.Add(1)
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
type mockRedisConnector struct {
	closeCalls atomic.Int32
}

func (m *mockRedisConnector) Connect(ctx context.Context) error {
	return nil
}

func (m *mockRedisConnector) Close() error {
	m.closeCalls.Add(1)
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

type consumeRecordingMeter struct {
	metrics.Meter
	mu       sync.Mutex
	statuses []string
}

func newConsumeRecordingMeter() *consumeRecordingMeter {
	return &consumeRecordingMeter{Meter: metrics.Discard()}
}

func (m *consumeRecordingMeter) Counter(name, desc string, opts ...metrics.MetricOption) (metrics.Counter, error) {
	if name == MetricConsumeTotal {
		return consumeRecordingCounter{meter: m}, nil
	}
	return m.Meter.Counter(name, desc, opts...)
}

func (m *consumeRecordingMeter) recordedStatuses() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.statuses...)
}

type consumeRecordingCounter struct {
	meter *consumeRecordingMeter
}

func (c consumeRecordingCounter) Inc(ctx context.Context, labels ...metrics.Label) {
	c.Add(ctx, 1, labels...)
}

func (c consumeRecordingCounter) Add(_ context.Context, _ float64, labels ...metrics.Label) {
	for _, label := range labels {
		if label.Key == LabelStatus {
			c.meter.mu.Lock()
			c.meter.statuses = append(c.meter.statuses, label.Value)
			c.meter.mu.Unlock()
			return
		}
	}
}

// newMQ 创建一个用于测试的 MQ 实例
func newMQ(transport transport, logger clog.Logger, meter metrics.Meter) MQ {
	return &mq{
		transport:     transport,
		logger:        logger,
		meter:         meter,
		driver:        DriverNATSJetStream,
		subscriptions: make(map[Subscription]struct{}),
		lifecycleDone: make(chan struct{}),
	}
}
