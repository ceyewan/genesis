package mq

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOptions 测试选项函数
func TestOptions(t *testing.T) {
	t.Run("WithQueueGroup", func(t *testing.T) {
		o := defaultSubscribeOptions()
		WithQueueGroup("test-group")(&o)

		assert.Equal(t, "test-group", o.QueueGroup)
	})

	t.Run("WithDurable", func(t *testing.T) {
		o := defaultSubscribeOptions()
		WithDurable("durable-name")(&o)

		assert.Equal(t, "durable-name", o.DurableName)
	})

	t.Run("WithManualAck", func(t *testing.T) {
		o := defaultSubscribeOptions()
		assert.True(t, o.AutoAck) // 默认自动确认

		WithManualAck()(&o)

		assert.False(t, o.AutoAck)
	})

	t.Run("WithBufferSize", func(t *testing.T) {
		o := defaultSubscribeOptions()
		assert.Equal(t, 100, o.BufferSize) // 默认缓冲大小

		WithBufferSize(500)(&o)

		assert.Equal(t, 500, o.BufferSize)
	})

	t.Run("multiple options", func(t *testing.T) {
		o := defaultSubscribeOptions()

		WithQueueGroup("group")(&o)
		WithDurable("durable")(&o)
		WithManualAck()(&o)
		WithBufferSize(200)(&o)

		assert.Equal(t, "group", o.QueueGroup)
		assert.Equal(t, "durable", o.DurableName)
		assert.False(t, o.AutoAck)
		assert.Equal(t, 200, o.BufferSize)
	})
}

// TestDefaultSubscribeOptions 测试默认订阅选项
func TestDefaultSubscribeOptions(t *testing.T) {
	o := defaultSubscribeOptions()

	assert.True(t, o.AutoAck)
	assert.Equal(t, 100, o.BufferSize)
	assert.Empty(t, o.QueueGroup)
	assert.Empty(t, o.DurableName)
}

// TestNew 测试 MQ 客户端创建
func TestNew(t *testing.T) {
	t.Run("创建带有默认 logger 的客户端", func(t *testing.T) {
		// 创建一个 mock driver
		driver := &mockDriver{}

		client, err := New(driver)

		require.NoError(t, err)
		assert.NotNil(t, client)

		_ = client.Close()
	})

	t.Run("创建带有自定义 logger 的客户端", func(t *testing.T) {
		driver := &mockDriver{}
		logger := clog.Discard()

		client, err := New(driver, WithLogger(logger))

		require.NoError(t, err)
		assert.NotNil(t, client)

		_ = client.Close()
	})

	t.Run("创建带有 logger 和 meter 的客户端", func(t *testing.T) {
		driver := &mockDriver{}
		logger := clog.Discard()
		meter := metrics.Discard()

		client, err := New(driver, WithLogger(logger), WithMeter(meter))

		require.NoError(t, err)
		assert.NotNil(t, client)

		_ = client.Close()
	})
}

// TestMessageInterface 测试消息接口
func TestMessageInterface(t *testing.T) {
	t.Run("coreMessage 实现 Message 接口", func(t *testing.T) {
		msg := &coreMessage{msg: &nats.Msg{}}
		// 编译时检查接口实现
		var _ Message = msg

		assert.Empty(t, msg.Subject())
		assert.Nil(t, msg.Data())
		assert.NoError(t, msg.Ack())
		assert.NoError(t, msg.Nak())
	})

	t.Run("jetStreamMessage 实现 Message 接口", func(t *testing.T) {
		// jetStreamMessage 需要 jetstream.Msg，这里只测试接口存在
		// 实际测试在集成测试中进行
	})
}

// TestSubscriptionInterface 测试订阅接口
func TestSubscriptionInterface(t *testing.T) {
	t.Run("coreSubscription 实现 Subscription 接口", func(t *testing.T) {
		sub := &coreSubscription{}
		// 编译时检查接口实现
		var _ Subscription = sub
	})

	t.Run("redisSubscription 实现 Subscription 接口", func(t *testing.T) {
		sub := &redisSubscription{cancel: func() {}}
		// 编译时检查接口实现
		var _ Subscription = sub
	})

	t.Run("kafkaSubscription 实现 Subscription 接口", func(t *testing.T) {
		sub := &kafkaSubscription{}
		// 编译时检查接口实现
		var _ Subscription = sub
	})
}

// TestClientPublish 测试发布功能
func TestClientPublish(t *testing.T) {
	t.Run("发布成功", func(t *testing.T) {
		driver := &mockDriver{}
		client, _ := New(driver, WithLogger(clog.Discard()))

		ctx := context.Background()
		err := client.Publish(ctx, "test.subject", []byte("test data"))

		assert.NoError(t, err)
		assert.True(t, driver.publishCalled)
	})

	t.Run("发布失败返回错误", func(t *testing.T) {
		driver := &mockDriver{publishError: assert.AnError}
		client, _ := New(driver, WithLogger(clog.Discard()))

		ctx := context.Background()
		err := client.Publish(ctx, "test.subject", []byte("test data"))

		assert.Error(t, err)
	})
}

// TestClientSubscribe 测试订阅功能
func TestClientSubscribe(t *testing.T) {
	t.Run("订阅成功", func(t *testing.T) {
		driver := &mockDriver{}
		client, _ := New(driver, WithLogger(clog.Discard()))

		ctx := context.Background()
		handler := func(ctx context.Context, msg Message) error {
			return nil
		}

		sub, err := client.Subscribe(ctx, "test.subject", handler)

		assert.NoError(t, err)
		assert.NotNil(t, sub)
		assert.True(t, driver.subscribeCalled)
	})

	t.Run("订阅失败返回错误", func(t *testing.T) {
		driver := &mockDriver{subscribeError: assert.AnError}
		client, _ := New(driver, WithLogger(clog.Discard()))

		ctx := context.Background()
		handler := func(ctx context.Context, msg Message) error {
			return nil
		}

		sub, err := client.Subscribe(ctx, "test.subject", handler)

		assert.Error(t, err)
		assert.Nil(t, sub)
	})
}

// TestClientClose 测试关闭功能
func TestClientClose(t *testing.T) {
	t.Run("关闭客户端", func(t *testing.T) {
		driver := &mockDriver{}
		client, _ := New(driver)

		err := client.Close()

		assert.NoError(t, err)
	})
}

// TestSubscribeChan 测试 Channel 模式订阅
func TestSubscribeChan(t *testing.T) {
	t.Run("Channel 模式订阅成功", func(t *testing.T) {
		driver := &mockDriver{}
		client, _ := New(driver, WithLogger(clog.Discard()))

		ctx := context.Background()
		ch, sub, err := client.SubscribeChan(ctx, "test.subject")

		assert.NoError(t, err)
		assert.NotNil(t, ch)
		assert.NotNil(t, sub)
		assert.True(t, driver.subscribeCalled)

		_ = sub.Unsubscribe()
	})

	t.Run("订阅失败时 Channel 被关闭", func(t *testing.T) {
		driver := &mockDriver{subscribeError: fmt.Errorf("subscribe failed")}
		client, _ := New(driver, WithLogger(clog.Discard()))

		ctx := context.Background()
		ch, sub, err := client.SubscribeChan(ctx, "test.subject")

		require.Error(t, err)
		assert.Nil(t, sub)
		// Channel 应该被关闭
		select {
		case _, ok := <-ch:
			assert.False(t, ok, "channel should be closed")
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for channel to close")
		}
	})

	t.Run("使用自定义缓冲区大小", func(t *testing.T) {
		driver := &mockDriver{}
		client, _ := New(driver, WithLogger(clog.Discard()))

		ctx := context.Background()
		ch, sub, err := client.SubscribeChan(ctx, "test.subject", WithBufferSize(10))

		assert.NoError(t, err)
		assert.NotNil(t, ch)
		assert.NotNil(t, sub)

		_ = sub.Unsubscribe()
	})
}

// TestDriverType 测试驱动类型常量
func TestDriverType(t *testing.T) {
	tests := []struct {
		name   string
		driver DriverType
		want   string
	}{
		{"NATS Core", DriverNatsCore, "nats_core"},
		{"NATS JetStream", DriverNatsJetStream, "nats_jetstream"},
		{"Redis", DriverRedis, "redis"},
		{"Kafka", DriverKafka, "kafka"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, string(tt.driver))
		})
	}
}

// TestJetStreamConfig 测试 JetStream 配置
func TestJetStreamConfig(t *testing.T) {
	t.Run("默认配置", func(t *testing.T) {
		cfg := &JetStreamConfig{}

		assert.False(t, cfg.AutoCreateStream)
	})

	t.Run("启用自动创建 Stream", func(t *testing.T) {
		cfg := &JetStreamConfig{
			AutoCreateStream: true,
		}

		assert.True(t, cfg.AutoCreateStream)
	})
}

// TestConfig 测试 MQ 配置
func TestConfig(t *testing.T) {
	t.Run("默认配置", func(t *testing.T) {
		cfg := &Config{}

		assert.Empty(t, cfg.Driver)
		assert.Nil(t, cfg.JetStream)
	})

	t.Run("完整配置", func(t *testing.T) {
		cfg := &Config{
			Driver: DriverNatsCore,
			JetStream: &JetStreamConfig{
				AutoCreateStream: true,
			},
		}

		assert.Equal(t, DriverNatsCore, cfg.Driver)
		assert.NotNil(t, cfg.JetStream)
		assert.True(t, cfg.JetStream.AutoCreateStream)
	})
}

// -----------------------------------------------------------
// Mock Driver for Testing
// -----------------------------------------------------------

type mockDriver struct {
	publishCalled   bool
	subscribeCalled bool
	publishError    error
	subscribeError  error
}

func (m *mockDriver) Publish(ctx context.Context, subject string, data []byte, opts ...PublishOption) error {
	m.publishCalled = true
	return m.publishError
}

func (m *mockDriver) Subscribe(ctx context.Context, subject string, handler Handler, opts ...SubscribeOption) (Subscription, error) {
	m.subscribeCalled = true
	if m.subscribeError != nil {
		return nil, m.subscribeError
	}
	return &mockSubscription{}, nil
}

func (m *mockDriver) Close() error {
	return nil
}

type mockSubscription struct{}

func (m *mockSubscription) Unsubscribe() error {
	return nil
}

func (m *mockSubscription) IsValid() bool {
	return true
}
