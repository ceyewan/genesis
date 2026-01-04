// examples/mq/main.go
package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/mq"
	"github.com/ceyewan/genesis/testkit"
	"github.com/joho/godotenv"
)

func main() {
	// 尝试加载 .env 文件
	_ = godotenv.Load()

	ctx := context.Background()
	logger := initLogger()

	drivers := []struct {
		name string
		init func(context.Context, clog.Logger) (mq.Client, func(), error)
	}{
		{name: "nats-core", init: initNATS},
		{name: "nats-jetstream", init: initNATSJetStream},
		{name: "redis-stream", init: initRedis},
	}

	for _, item := range drivers {
		logger.Info("=== Genesis MQ Component Example ===", clog.String("driver", item.name))

		mqClient, cleanup, err := item.init(ctx, logger)
		if err != nil {
			logger.Error("初始化 MQ 失败", clog.String("driver", item.name), clog.Error(err))
			continue
		}

		runDemo(ctx, mqClient, logger)
		cleanup()

		logger.Info("=== 示例演示完成 ===", clog.String("driver", item.name))
	}
}

// initLogger 初始化日志组件
func initLogger() clog.Logger {
	logger, err := clog.New(clog.NewDevDefaultConfig("genesis"))
	if err != nil {
		return clog.Discard()
	}
	return logger.WithNamespace("mq-example")
}

// -----------------------------------------------------------
// 初始化函数 (特定驱动)
// -----------------------------------------------------------

func initNATS(ctx context.Context, logger clog.Logger) (mq.Client, func(), error) {
	conn, err := connector.NewNATS(&connector.NATSConfig{
		URL:  getEnvOrDefault("NATS_URL", "nats://127.0.0.1:4222"),
		Name: "genesis-mq-nats-example",
	}, connector.WithLogger(logger))
	if err != nil {
		return nil, nil, err
	}
	if err := conn.Connect(ctx); err != nil {
		return nil, nil, err
	}
	client, err := mq.New(&mq.Config{
		Driver: mq.DriverNatsCore,
	}, mq.WithNATSConnector(conn), mq.WithLogger(logger))
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	return client, func() {
		_ = client.Close()
		_ = conn.Close()
	}, nil
}

func initNATSJetStream(ctx context.Context, logger clog.Logger) (mq.Client, func(), error) {
	conn, err := connector.NewNATS(&connector.NATSConfig{
		URL:  getEnvOrDefault("NATS_URL", "nats://127.0.0.1:4222"),
		Name: "genesis-mq-nats-js-example",
	}, connector.WithLogger(logger))
	if err != nil {
		return nil, nil, err
	}
	if err := conn.Connect(ctx); err != nil {
		return nil, nil, err
	}
	client, err := mq.New(&mq.Config{
		Driver: mq.DriverNatsJetStream,
		JetStream: &mq.JetStreamConfig{
			AutoCreateStream: true,
		},
	}, mq.WithNATSConnector(conn), mq.WithLogger(logger))
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	return client, func() {
		_ = client.Close()
		_ = conn.Close()
	}, nil
}

func initRedis(ctx context.Context, logger clog.Logger) (mq.Client, func(), error) {
	conn, err := connector.NewRedis(&connector.RedisConfig{
		Addr: getEnvOrDefault("REDIS_ADDR", "localhost:6379"),
		DB:   1,
	}, connector.WithLogger(logger))
	if err != nil {
		return nil, nil, err
	}
	if err := conn.Connect(ctx); err != nil {
		return nil, nil, err
	}
	client, err := mq.New(&mq.Config{
		Driver: mq.DriverRedis,
	}, mq.WithRedisConnector(conn), mq.WithLogger(logger))
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	return client, func() {
		_ = client.Close()
		_ = conn.Close()
	}, nil
}

// -----------------------------------------------------------
// 统一的业务演示逻辑
// -----------------------------------------------------------

func runDemo(ctx context.Context, mqClient mq.Client, logger clog.Logger) {
	id := testkit.NewID()
	topic1 := fmt.Sprintf("topic1-%s", id)
	topic2 := fmt.Sprintf("topic2-%s", id)
	group := fmt.Sprintf("group-%s", id)

	logger.Info("场景一: 广播/发布订阅", clog.String("topic", topic1))

	var wg1 sync.WaitGroup
	wg1.Add(20) // 2个消费者，各收到10条

	handler := func(name string) mq.Handler {
		return func(ctx context.Context, msg mq.Message) error {
			logger.Info("收到消息", clog.String("consumer", name), clog.String("data", string(msg.Data())))
			wg1.Done()
			return nil
		}
	}

	_, _ = mqClient.Subscribe(ctx, topic1, handler("Consumer-A"))
	_, _ = mqClient.Subscribe(ctx, topic1, handler("Consumer-B"))

	// 给订阅者一点点准备时间
	time.Sleep(time.Second)

	logger.Info("发送广播消息", clog.Int("count", 10))
	for i := 1; i <= 10; i++ {
		data := []byte(fmt.Sprintf("MSG-%d", i))
		_ = mqClient.Publish(ctx, topic1, data)
	}

	wg1.Wait()
	logger.Info("场景一完成: 所有订阅者均收到全部消息")

	logger.Info("场景二: 消费者组/负载均衡", clog.String("topic", topic2), clog.String("group", group))

	var wg2 sync.WaitGroup
	wg2.Add(10) // 组内平摊10条

	workerHandler := func(name string) mq.Handler {
		return func(ctx context.Context, msg mq.Message) error {
			logger.Info("处理任务", clog.String("worker", name), clog.String("data", string(msg.Data())))
			wg2.Done()
			_ = msg.Ack()
			return nil
		}
	}

	_, _ = mqClient.Subscribe(ctx, topic2, workerHandler("Worker-1"), mq.WithQueueGroup(group), mq.WithManualAck())
	_, _ = mqClient.Subscribe(ctx, topic2, workerHandler("Worker-2"), mq.WithQueueGroup(group), mq.WithManualAck())

	logger.Info("等待消费者组准备就绪")
	time.Sleep(500 * time.Millisecond)

	logger.Info("发送队列任务", clog.Int("count", 10))
	for i := 1; i <= 10; i++ {
		data := []byte(fmt.Sprintf("TASK-%d", i))
		_ = mqClient.Publish(ctx, topic2, data)
		time.Sleep(50 * time.Millisecond)
	}

	done := make(chan struct{})
	go func() {
		wg2.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.Info("场景二完成: 消费者组平摊处理了所有任务")
	case <-time.After(15 * time.Second):
		logger.Warn("场景二等待超时")
	}
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
