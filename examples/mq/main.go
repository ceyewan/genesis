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

	logger.Info("=== Genesis MQ Component Example (NATS JetStream) ===")

	mqClient, cleanup, err := initJetStream(ctx, logger)
	if err != nil {
		logger.Error("初始化 MQ 失败", clog.Error(err))
		return
	}
	defer cleanup()

	runDemo(ctx, mqClient, logger)

	logger.Info("=== 示例演示完成 ===")
}

// initLogger 初始化日志组件
func initLogger() clog.Logger {
	logger, err := clog.New(clog.NewDevDefaultConfig("genesis"))
	if err != nil {
		return clog.Discard()
	}
	return logger.WithNamespace("mq-example")
}

// initJetStream 初始化 NATS JetStream
func initJetStream(ctx context.Context, logger clog.Logger) (mq.MQ, func(), error) {
	conn, err := connector.NewNATS(&connector.NATSConfig{
		URL:  getEnvOrDefault("NATS_URL", "nats://127.0.0.1:4222"),
		Name: "genesis-mq-jetstream-example",
	}, connector.WithLogger(logger))
	if err != nil {
		return nil, nil, err
	}
	if err := conn.Connect(ctx); err != nil {
		return nil, nil, err
	}
	client, err := mq.New(&mq.Config{
		Driver: mq.DriverNATSJetStream,
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

// runDemo 运行演示场景
func runDemo(ctx context.Context, mqClient mq.MQ, logger clog.Logger) {
	id := testkit.NewID()

	// 场景一: 基础发布订阅
	demoBasicPubSub(ctx, mqClient, logger, id)

	// 场景二: 队列组/负载均衡
	demoQueueGroup(ctx, mqClient, logger, id)

	// 场景三: 中间件链
	demoMiddleware(ctx, mqClient, logger, id)

	// 场景四: 手动确认
	demoManualAck(ctx, mqClient, logger, id)

	// 场景五: 消息头
	demoHeaders(ctx, mqClient, logger, id)
}

// demoBasicPubSub 基础发布订阅演示
func demoBasicPubSub(ctx context.Context, mqClient mq.MQ, logger clog.Logger, id string) {
	topic := fmt.Sprintf("demo.basic-%s", id)

	logger.Info("场景一: 基础发布订阅", clog.String("topic", topic))

	var wg sync.WaitGroup
	wg.Add(10) // 10条消息

	handler := func(msg mq.Message) error {
		logger.Info("收到消息",
			clog.String("topic", msg.Topic()),
			clog.String("data", string(msg.Data())),
			clog.String("msg_id", msg.ID()),
		)
		wg.Done()
		return nil
	}

	sub, err := mqClient.Subscribe(ctx, topic, handler)
	if err != nil {
		logger.Error("订阅失败", clog.Error(err))
		return
	}
	defer sub.Unsubscribe()

	time.Sleep(500 * time.Millisecond)

	logger.Info("发送消息", clog.Int("count", 10))
	for i := 1; i <= 10; i++ {
		data := []byte(fmt.Sprintf("MSG-%d", i))
		if err := mqClient.Publish(ctx, topic, data); err != nil {
			logger.Error("发布失败", clog.Error(err))
		}
	}

	wg.Wait()
	logger.Info("场景一完成")
	time.Sleep(time.Second)
}

// demoQueueGroup 队列组负载均衡演示
func demoQueueGroup(ctx context.Context, mqClient mq.MQ, logger clog.Logger, id string) {
	topic := fmt.Sprintf("demo.queue-%s", id)
	group := fmt.Sprintf("worker-group-%s", id)

	logger.Info("场景二: 队列组负载均衡", clog.String("topic", topic), clog.String("group", group))

	var wg sync.WaitGroup
	wg.Add(10)

	workerHandler := func(name string) mq.Handler {
		return func(msg mq.Message) error {
			logger.Info("处理任务",
				clog.String("worker", name),
				clog.String("data", string(msg.Data())),
				clog.String("msg_id", msg.ID()),
			)
			wg.Done()
			return nil
		}
	}

	sub1, err := mqClient.Subscribe(ctx, topic, workerHandler("Worker-1"), mq.WithQueueGroup(group))
	if err != nil {
		logger.Error("订阅失败", clog.Error(err))
		return
	}
	defer sub1.Unsubscribe()

	sub2, err := mqClient.Subscribe(ctx, topic, workerHandler("Worker-2"), mq.WithQueueGroup(group))
	if err != nil {
		logger.Error("订阅失败", clog.Error(err))
		return
	}
	defer sub2.Unsubscribe()

	time.Sleep(500 * time.Millisecond)

	logger.Info("发送任务", clog.Int("count", 10))
	for i := 1; i <= 10; i++ {
		data := []byte(fmt.Sprintf("TASK-%d", i))
		_ = mqClient.Publish(ctx, topic, data)
	}

	wg.Wait()
	logger.Info("场景二完成: 消费者组平摊处理了所有任务")
	time.Sleep(time.Second)
}

// demoMiddleware 中间件演示
func demoMiddleware(ctx context.Context, mqClient mq.MQ, logger clog.Logger, id string) {
	topic := fmt.Sprintf("demo.middleware-%s", id)

	logger.Info("场景三: 中间件链", clog.String("topic", topic))

	var wg sync.WaitGroup
	var mu sync.Mutex
	successCount := 0

	// 使用 WithManualAck 避免 JetStream 自动重投导致循环
	handler := func(msg mq.Message) error {
		data := string(msg.Data())

		// 最外层包装：确保 wg.Done() 只调用一次
		defer func() {
			mu.Lock()
			successCount++
			wg.Done()
			mu.Unlock()
		}()

		// 业务处理函数（会重试）
		businessHandler := func(msg mq.Message) error {
			// 只对 TASK-3 模拟一次失败
			if data == "TASK-3" {
				return fmt.Errorf("simulated error for %s", data)
			}
			logger.Info("业务处理成功", clog.String("data", data))
			return nil
		}

		// 组合中间件: Recover -> Logging -> Retry
		chain := mq.Chain(
			mq.WithRecover(logger),
			mq.WithLogging(logger),
			mq.WithRetry(mq.RetryConfig{
				MaxRetries:     2,
				InitialBackoff: 50 * time.Millisecond,
				MaxBackoff:     200 * time.Millisecond,
				Multiplier:     2.0,
			}, logger),
		)

		err := chain(businessHandler)(msg)

		// 手动确认消息（无论成功失败都 Ack，避免重投）
		if err != nil {
			logger.Warn("任务最终失败（已重试），确认消息", clog.String("data", data))
		}
		_ = msg.Ack()
		return nil
	}

	sub, err := mqClient.Subscribe(ctx, topic, handler, mq.WithManualAck())
	if err != nil {
		logger.Error("订阅失败", clog.Error(err))
		return
	}
	defer sub.Unsubscribe()

	time.Sleep(500 * time.Millisecond)

	wg.Add(5)
	logger.Info("发送任务（包含一条会失败的任务）")
	for i := 1; i <= 5; i++ {
		data := []byte(fmt.Sprintf("TASK-%d", i))
		_ = mqClient.Publish(ctx, topic, data)
	}

	wg.Wait()
	logger.Info("场景三完成", clog.Int("success_count", successCount))
	time.Sleep(time.Second)
}

// demoManualAck 手动确认演示
func demoManualAck(ctx context.Context, mqClient mq.MQ, logger clog.Logger, id string) {
	topic := fmt.Sprintf("demo.ack-%s", id)

	logger.Info("场景四: 手动确认", clog.String("topic", topic))

	var wg sync.WaitGroup
	var doneMap sync.Map // 跟踪已处理的消息，避免 Nak 重投时重复 Done
	wg.Add(5)

	handler := func(msg mq.Message) error {
		data := string(msg.Data())
		msgID := msg.ID()

		// 检查是否已经处理过这条消息（避免 Nak 重投时重复处理）
		if _, loaded := doneMap.LoadOrStore(msgID, true); loaded {
			// 已经处理过，直接确认并返回
			_ = msg.Ack()
			return nil
		}

		if data == "TASK-3" {
			// 拒绝消息（会重投）
			logger.Warn("拒绝消息", clog.String("data", data))
			_ = msg.Nak()
		} else {
			// 确认消息
			logger.Info("确认消息", clog.String("data", data))
			_ = msg.Ack()
		}
		wg.Done()
		return nil
	}

	sub, err := mqClient.Subscribe(ctx, topic, handler, mq.WithManualAck())
	if err != nil {
		logger.Error("订阅失败", clog.Error(err))
		return
	}
	defer sub.Unsubscribe()

	time.Sleep(500 * time.Millisecond)

	logger.Info("发送任务")
	for i := 1; i <= 5; i++ {
		data := []byte(fmt.Sprintf("TASK-%d", i))
		_ = mqClient.Publish(ctx, topic, data)
	}

	wg.Wait()
	logger.Info("场景四完成")
	time.Sleep(time.Second)
}

// demoHeaders 消息头演示
func demoHeaders(ctx context.Context, mqClient mq.MQ, logger clog.Logger, id string) {
	topic := fmt.Sprintf("demo.headers-%s", id)

	logger.Info("场景五: 消息头", clog.String("topic", topic))

	var wg sync.WaitGroup
	wg.Add(3)

	handler := func(msg mq.Message) error {
		headers := msg.Headers()
		logger.Info("收到带头部的消息",
			clog.String("data", string(msg.Data())),
			clog.String("trace-id", headers.Get("trace-id")),
			clog.String("user-id", headers.Get("user-id")),
			clog.String("priority", headers.Get("priority")),
		)
		wg.Done()
		return nil
	}

	sub, err := mqClient.Subscribe(ctx, topic, handler)
	if err != nil {
		logger.Error("订阅失败", clog.Error(err))
		return
	}
	defer sub.Unsubscribe()

	time.Sleep(500 * time.Millisecond)

	// 使用多种方式设置消息头
	logger.Info("发送带头部的消息")

	// 方式1: WithHeaders
	_ = mqClient.Publish(ctx, topic, []byte("MSG-1"),
		mq.WithHeaders(mq.Headers{
			"trace-id": "trace-123",
			"user-id":  "user-456",
			"priority": "high",
		}))

	// 方式2: WithHeader 单个设置
	_ = mqClient.Publish(ctx, topic, []byte("MSG-2"),
		mq.WithHeader("trace-id", "trace-789"),
		mq.WithHeader("user-id", "user-101"),
		mq.WithHeader("priority", "low"),
	)

	// 方式3: 混合
	_ = mqClient.Publish(ctx, topic, []byte("MSG-3"),
		mq.WithHeaders(mq.Headers{"trace-id": "trace-222"}),
		mq.WithHeader("priority", "medium"),
	)

	wg.Wait()
	logger.Info("场景五完成")
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
