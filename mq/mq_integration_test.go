//go:build integration
// +build integration

package mq

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 辅助函数：获取测试用 Logger
func getTestLogger() clog.Logger {
	logger, err := clog.New(clog.NewDevDefaultConfig("mq-test"))
	if err != nil {
		return clog.Discard()
	}
	return logger
}

// 辅助函数：生成唯一测试主题
func testSubject(prefix string) string {
	return fmt.Sprintf("test.%s.%d", prefix, time.Now().UnixNano())
}

// 辅助函数：生成唯一测试组名
func testGroup(prefix string) string {
	return fmt.Sprintf("test-group-%s-%d", prefix, time.Now().UnixNano())
}

// TestNatsCorePublishSubscribe 测试 NATS Core 发布订阅
func TestNatsCorePublishSubscribe(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	natsURL := getEnvOrDefault("NATS_URL", "nats://localhost:4222")

	t.Run("NATS Core 基本发布订阅", func(t *testing.T) {
		// 创建连接器
		natsConn, err := connector.NewNATS(&connector.NATSConfig{
			URL:  natsURL,
			Name: "test-nats-core",
		}, connector.WithLogger(getTestLogger()))
		require.NoError(t, err)

		ctx := context.Background()
		err = natsConn.Connect(ctx)
		if err != nil {
			t.Skip("NATS 服务不可用")
		}
		defer natsConn.Close()

		// 创建客户端
		client, err := New(&Config{
			Driver: DriverNatsCore,
		}, WithNATSConnector(natsConn), WithLogger(getTestLogger()))
		require.NoError(t, err)
		defer client.Close()

		subject := testSubject("basic")
		var receivedMsg string
		var wg sync.WaitGroup
		wg.Add(1)

		// 订阅
		sub, err := client.Subscribe(ctx, subject, func(ctx context.Context, msg Message) error {
			receivedMsg = string(msg.Data())
			wg.Done()
			return nil
		})
		require.NoError(t, err)
		defer sub.Unsubscribe()

		// 等待订阅生效
		time.Sleep(100 * time.Millisecond)

		// 发布
		testData := "Hello from NATS Core"
		err = client.Publish(ctx, subject, []byte(testData))
		require.NoError(t, err)

		// 等待接收
		wg.Wait()

		assert.Equal(t, testData, receivedMsg)
	})

	t.Run("NATS Core 队列订阅", func(t *testing.T) {
		natsConn, err := connector.NewNATS(&connector.NATSConfig{
			URL:  natsURL,
			Name: "test-nats-queue",
		}, connector.WithLogger(getTestLogger()))
		require.NoError(t, err)

		ctx := context.Background()
		err = natsConn.Connect(ctx)
		if err != nil {
			t.Skip("NATS 服务不可用")
		}
		defer natsConn.Close()

		client, err := New(&Config{
			Driver: DriverNatsCore,
		}, WithNATSConnector(natsConn), WithLogger(getTestLogger()))
		require.NoError(t, err)
		defer client.Close()

		subject := testSubject("queue")
		group := testGroup("queue")

		// 多个订阅者订阅同一个队列组
		var messages []string
		var mu sync.Mutex
		var wg sync.WaitGroup

		for i := 0; i < 3; i++ {
			wg.Add(1)
			sub, err := client.Subscribe(ctx, subject, func(ctx context.Context, msg Message) error {
				mu.Lock()
				messages = append(messages, string(msg.Data()))
				mu.Unlock()
				wg.Done()
				return nil
			}, WithQueueGroup(group))
			require.NoError(t, err)
			defer sub.Unsubscribe()
		}

		time.Sleep(100 * time.Millisecond)

		// 发布多条消息
		for i := 1; i <= 5; i++ {
			err = client.Publish(ctx, subject, []byte(fmt.Sprintf("message-%d", i)))
			require.NoError(t, err)
		}

		// 等待所有消息被接收
		wg.Wait()

		// 在队列组模式下，每条消息只被一个订阅者接收
		assert.Len(t, messages, 5)
	})

	t.Run("NATS Core Channel 模式订阅", func(t *testing.T) {
		natsConn, err := connector.NewNATS(&connector.NATSConfig{
			URL:  natsURL,
			Name: "test-nats-chan",
		}, connector.WithLogger(getTestLogger()))
		require.NoError(t, err)

		ctx := context.Background()
		err = natsConn.Connect(ctx)
		if err != nil {
			t.Skip("NATS 服务不可用")
		}
		defer natsConn.Close()

		client, err := New(&Config{
			Driver: DriverNatsCore,
		}, WithNATSConnector(natsConn), WithLogger(getTestLogger()))
		require.NoError(t, err)
		defer client.Close()

		subject := testSubject("channel")

		ch, sub, err := client.SubscribeChan(ctx, subject, WithBufferSize(10))
		require.NoError(t, err)
		defer sub.Unsubscribe()

		time.Sleep(100 * time.Millisecond)

		// 发布消息
		testData := "Hello via channel"
		err = client.Publish(ctx, subject, []byte(testData))
		require.NoError(t, err)

		// 从 channel 接收
		select {
		case msg := <-ch:
			assert.Equal(t, testData, string(msg.Data()))
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for message")
		}
	})
}

// TestRedisStreamPublishSubscribe 测试 Redis Stream 发布订阅
func TestRedisStreamPublishSubscribe(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	redisAddr := getEnvOrDefault("REDIS_ADDR", "localhost:6379")

	t.Run("Redis Stream 基本发布订阅（广播模式）", func(t *testing.T) {
		// 创建连接器
		redisConn, err := connector.NewRedis(&connector.RedisConfig{
			Addr:     redisAddr,
			Name:     "test-redis-stream",
			DB:       1,
			PoolSize: 10,
		}, connector.WithLogger(getTestLogger()))
		require.NoError(t, err)

		ctx := context.Background()
		err = redisConn.Connect(ctx)
		if err != nil {
			t.Skip("Redis 服务不可用")
		}
		defer redisConn.Close()

		// 清理测试数据
		testStream := testSubject("redis-basic")
		defer redisConn.GetClient().Del(ctx, testStream)

		// 创建客户端
		client, err := New(&Config{
			Driver: DriverRedis,
		}, WithRedisConnector(redisConn), WithLogger(getTestLogger()))
		require.NoError(t, err)
		defer client.Close()

		var receivedMsg string
		var wg sync.WaitGroup
		wg.Add(1)

		// 订阅
		sub, err := client.Subscribe(ctx, testStream, func(ctx context.Context, msg Message) error {
			receivedMsg = string(msg.Data())
			wg.Done()
			return nil
		})
		require.NoError(t, err)
		defer sub.Unsubscribe()

		time.Sleep(200 * time.Millisecond)

		// 发布
		testData := "Hello from Redis Stream"
		err = client.Publish(ctx, testStream, []byte(testData))
		require.NoError(t, err)

		// 等待接收
		wg.Wait()

		assert.Equal(t, testData, receivedMsg)
	})

	t.Run("Redis Stream 消费者组模式", func(t *testing.T) {
		redisConn, err := connector.NewRedis(&connector.RedisConfig{
			Addr:     redisAddr,
			Name:     "test-redis-group",
			DB:       1,
			PoolSize: 10,
		}, connector.WithLogger(getTestLogger()))
		require.NoError(t, err)

		ctx := context.Background()
		err = redisConn.Connect(ctx)
		if err != nil {
			t.Skip("Redis 服务不可用")
		}
		defer redisConn.Close()

		testStream := testSubject("redis-group")
		group := testGroup("redis")
		defer redisConn.GetClient().Del(ctx, testStream)

		client, err := New(&Config{
			Driver: DriverRedis,
		}, WithRedisConnector(redisConn), WithLogger(getTestLogger()))
		require.NoError(t, err)
		defer client.Close()

		var messages []string
		var mu sync.Mutex
		var wg sync.WaitGroup

		// 创建消费者组订阅
		wg.Add(3)
		for i := 0; i < 3; i++ {
			sub, err := client.Subscribe(ctx, testStream, func(ctx context.Context, msg Message) error {
				mu.Lock()
				messages = append(messages, string(msg.Data()))
				mu.Unlock()
				wg.Done()
				return nil
			}, WithQueueGroup(group))
			require.NoError(t, err)
			defer sub.Unsubscribe()
		}

		time.Sleep(200 * time.Millisecond)

		// 发布消息
		for i := 1; i <= 3; i++ {
			err = client.Publish(ctx, testStream, []byte(fmt.Sprintf("redis-message-%d", i)))
			require.NoError(t, err)
		}

		wg.Wait()

		// 消费者组模式下，消息会被分发给不同的消费者
		assert.Len(t, messages, 3)
	})

	t.Run("Redis Stream 手动 Ack", func(t *testing.T) {
		redisConn, err := connector.NewRedis(&connector.RedisConfig{
			Addr:     redisAddr,
			Name:     "test-redis-ack",
			DB:       1,
			PoolSize: 10,
		}, connector.WithLogger(getTestLogger()))
		require.NoError(t, err)

		ctx := context.Background()
		err = redisConn.Connect(ctx)
		if err != nil {
			t.Skip("Redis 服务不可用")
		}
		defer redisConn.Close()

		testStream := testSubject("redis-ack")
		group := testGroup("redis-ack")
		defer redisConn.GetClient().Del(ctx, testStream)

		client, err := New(&Config{
			Driver: DriverRedis,
		}, WithRedisConnector(redisConn), WithLogger(getTestLogger()))
		require.NoError(t, err)
		defer client.Close()

		ackReceived := false
		var wg sync.WaitGroup
		wg.Add(1)

		// 订阅时禁用自动确认
		sub, err := client.Subscribe(ctx, testStream, func(ctx context.Context, msg Message) error {
			// 手动确认
			err := msg.Ack()
			assert.NoError(t, err)
			ackReceived = true
			wg.Done()
			return nil
		}, WithQueueGroup(group), WithManualAck())
		require.NoError(t, err)
		defer sub.Unsubscribe()

		time.Sleep(200 * time.Millisecond)

		err = client.Publish(ctx, testStream, []byte("test message"))
		require.NoError(t, err)

		wg.Wait()
		assert.True(t, ackReceived)
	})
}

// TestMultipleMessages 测试批量消息
func TestMultipleMessages(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	natsURL := getEnvOrDefault("NATS_URL", "nats://localhost:4222")

	t.Run("NATS 批量消息", func(t *testing.T) {
		natsConn, err := connector.NewNATS(&connector.NATSConfig{
			URL:  natsURL,
			Name: "test-nats-batch",
		}, connector.WithLogger(getTestLogger()))
		require.NoError(t, err)

		ctx := context.Background()
		err = natsConn.Connect(ctx)
		if err != nil {
			t.Skip("NATS 服务不可用")
		}
		defer natsConn.Close()

		client, err := New(&Config{
			Driver: DriverNatsCore,
		}, WithNATSConnector(natsConn), WithLogger(getTestLogger()))
		require.NoError(t, err)
		defer client.Close()

		subject := testSubject("batch")
		messageCount := 100

		var receivedCount int
		var mu sync.Mutex
		var wg sync.WaitGroup
		wg.Add(messageCount)

		sub, err := client.Subscribe(ctx, subject, func(ctx context.Context, msg Message) error {
			mu.Lock()
			receivedCount++
			mu.Unlock()
			wg.Done()
			return nil
		})
		require.NoError(t, err)
		defer sub.Unsubscribe()

		time.Sleep(100 * time.Millisecond)

		// 批量发布
		for i := 0; i < messageCount; i++ {
			err = client.Publish(ctx, subject, []byte(fmt.Sprintf("message-%d", i)))
			require.NoError(t, err)
		}

		wg.Wait()
		assert.Equal(t, messageCount, receivedCount)
	})
}

// TestSubscriptionUnsubscribe 测试取消订阅
func TestSubscriptionUnsubscribe(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	natsURL := getEnvOrDefault("NATS_URL", "nats://localhost:4222")

	t.Run("取消订阅后不再接收消息", func(t *testing.T) {
		natsConn, err := connector.NewNATS(&connector.NATSConfig{
			URL:  natsURL,
			Name: "test-nats-unsub",
		}, connector.WithLogger(getTestLogger()))
		require.NoError(t, err)

		ctx := context.Background()
		err = natsConn.Connect(ctx)
		if err != nil {
			t.Skip("NATS 服务不可用")
		}
		defer natsConn.Close()

		client, err := New(&Config{
			Driver: DriverNatsCore,
		}, WithNATSConnector(natsConn), WithLogger(getTestLogger()))
		require.NoError(t, err)
		defer client.Close()

		subject := testSubject("unsubscribe")

		receivedCount := 0
		var mu sync.Mutex

		sub, err := client.Subscribe(ctx, subject, func(ctx context.Context, msg Message) error {
			mu.Lock()
			receivedCount++
			mu.Unlock()
			return nil
		})
		require.NoError(t, err)

		time.Sleep(100 * time.Millisecond)

		// 发布第一条消息
		err = client.Publish(ctx, subject, []byte("message-1"))
		require.NoError(t, err)

		time.Sleep(100 * time.Millisecond)

		// 取消订阅
		err = sub.Unsubscribe()
		require.NoError(t, err)

		// 发布第二条消息（不应该被接收）
		err = client.Publish(ctx, subject, []byte("message-2"))
		require.NoError(t, err)

		time.Sleep(100 * time.Millisecond)

		assert.Equal(t, 1, receivedCount)
	})
}

// getEnvOrDefault 获取环境变量或返回默认值
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
