// examples/mq/main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
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

	// 命令行参数
	var (
		mode = flag.String("driver", "nats", "MQ driver: nats, nats-js, redis, kafka")
	)
	flag.Parse()

	fmt.Printf("=== Genesis MQ Component Example (Driver: %s) ===\n\n", *mode)

	ctx := context.Background()
	logger := initLogger()

	var mqClient mq.Client
	var err error

	// 1. 初始化连接器和 MQ 客户端
	switch *mode {
	case "nats":
		mqClient, err = initNATS(ctx, logger)
	case "nats-js":
		mqClient, err = initNATSJetStream(ctx, logger)
	case "redis":
		mqClient, err = initRedis(ctx, logger)
	case "kafka":
		mqClient, err = initKafka(ctx, logger)
	default:
		log.Fatalf("未知的驱动类型: %s (支持: nats, nats-js, redis, kafka)", *mode)
	}

	if err != nil {
		log.Fatalf("初始化 MQ 失败: %v", err)
	}
	defer mqClient.Close()

	// 2. 运行统一的业务演示逻辑
	runDemo(ctx, mqClient)

	fmt.Println("\n=== 示例演示完成 ===")
}

// initLogger 初始化日志组件
func initLogger() clog.Logger {
	return clog.Discard()
}

// -----------------------------------------------------------
// 初始化函数 (特定驱动)
// -----------------------------------------------------------

func initNATS(ctx context.Context, logger clog.Logger) (mq.Client, error) {
	conn, err := connector.NewNATS(&connector.NATSConfig{
		URL:  getEnvOrDefault("NATS_URL", "nats://127.0.0.1:4222"),
		Name: "genesis-mq-nats-example",
	}, connector.WithLogger(logger))
	if err != nil {
		return nil, err
	}
	if err := conn.Connect(ctx); err != nil {
		return nil, err
	}
	driver := mq.NewNatsCoreDriver(conn, logger)
	return mq.New(driver, mq.WithLogger(logger))
}

func initNATSJetStream(ctx context.Context, logger clog.Logger) (mq.Client, error) {
	conn, err := connector.NewNATS(&connector.NATSConfig{
		URL:  getEnvOrDefault("NATS_URL", "nats://127.0.0.1:4222"),
		Name: "genesis-mq-nats-js-example",
	}, connector.WithLogger(logger))
	if err != nil {
		return nil, err
	}
	if err := conn.Connect(ctx); err != nil {
		return nil, err
	}
	driver, err := mq.NewNatsJetStreamDriver(conn, &mq.JetStreamConfig{
		AutoCreateStream: true,
	}, logger)
	if err != nil {
		return nil, err
	}
	return mq.New(driver, mq.WithLogger(logger))
}

func initRedis(ctx context.Context, logger clog.Logger) (mq.Client, error) {
	conn, err := connector.NewRedis(&connector.RedisConfig{
		Addr: getEnvOrDefault("REDIS_ADDR", "localhost:6379"),
		DB:   1,
	}, connector.WithLogger(logger))
	if err != nil {
		return nil, err
	}
	if err := conn.Connect(ctx); err != nil {
		return nil, err
	}
	driver := mq.NewRedisDriver(conn, logger)
	return mq.New(driver, mq.WithLogger(logger))
}

func initKafka(ctx context.Context, logger clog.Logger) (mq.Client, error) {
	conn, err := connector.NewKafka(&connector.KafkaConfig{
		Name: "genesis-mq-kafka-example",
		Seed: []string{getEnvOrDefault("KAFKA_BROKERS", "localhost:9092")},
	}, connector.WithLogger(logger))
	if err != nil {
		return nil, err
	}
	if err := conn.Connect(ctx); err != nil {
		return nil, err
	}
	driver := mq.NewKafkaDriver(conn, logger)
	return mq.New(driver, mq.WithLogger(logger))
}

// -----------------------------------------------------------
// 统一的业务演示逻辑
// -----------------------------------------------------------

func runDemo(ctx context.Context, mqClient mq.Client) {
	id := testkit.NewID()
	topic1 := fmt.Sprintf("topic1-%s", id)
	topic2 := fmt.Sprintf("topic2-%s", id)
	group := fmt.Sprintf("group-%s", id)

	fmt.Printf("--- 场景一: 广播/发布订阅 (Fan-out) ---\n")
	fmt.Printf("Topic: %s\n\n", topic1)

	var wg1 sync.WaitGroup
	wg1.Add(20) // 2个消费者，各收到10条

	handler := func(name string) mq.Handler {
		return func(ctx context.Context, msg mq.Message) error {
			fmt.Printf("    [%s] 收到消息: %s\n", name, string(msg.Data()))
			wg1.Done()
			return nil
		}
	}

	_, _ = mqClient.Subscribe(ctx, topic1, handler("Consumer-A"))
	_, _ = mqClient.Subscribe(ctx, topic1, handler("Consumer-B"))

	// 给订阅者一点点准备时间 (尤其是 Kafka)
	time.Sleep(time.Second)

	fmt.Println("    发送 10 条广播消息...")
	for i := 1; i <= 10; i++ {
		data := []byte(fmt.Sprintf("MSG-%d", i))
		_ = mqClient.Publish(ctx, topic1, data)
	}

	wg1.Wait()
	fmt.Println("    ✓ 场景一完成: 所有订阅者均收到全部消息")

	fmt.Printf("\n--- 场景二: 消费者组/负载均衡 (Load Balance) ---\n")
	fmt.Printf("Topic: %s, Group: %s\n\n", topic2, group)

	var wg2 sync.WaitGroup
	wg2.Add(10) // 组内平摊10条

	workerHandler := func(name string) mq.Handler {
		return func(ctx context.Context, msg mq.Message) error {
			fmt.Printf("    [%s] 处理任务: %s\n", name, string(msg.Data()))
			wg2.Done()
			_ = msg.Ack()
			return nil
		}
	}

	_, _ = mqClient.Subscribe(ctx, topic2, workerHandler("Worker-1"), mq.WithQueueGroup(group))
	_, _ = mqClient.Subscribe(ctx, topic2, workerHandler("Worker-2"), mq.WithQueueGroup(group))

	// Kafka 需要较多时间来完成 JoinGroup 和 SyncGroup
	fmt.Println("    等待消费者组准备就绪...")
	time.Sleep(5 * time.Second)

	fmt.Println("    发送 10 条队列任务...")
	for i := 1; i <= 10; i++ {
		data := []byte(fmt.Sprintf("TASK-%d", i))
		// 使用 WithKey 确保消息分布到不同分区 (Kafka 专用优化)
		key := fmt.Sprintf("key-%d", i)
		_ = mqClient.Publish(ctx, topic2, data, mq.WithKey(key))
		time.Sleep(50 * time.Millisecond)
	}

	// 负载均衡场景下，Kafka 可能需要较长时间 Rebalance
	done := make(chan struct{})
	go func() {
		wg2.Wait()
		close(done)
	}()

	select {
	case <-done:
		fmt.Println("    ✓ 场景二完成: 消费者组平摊处理了所有任务")
	case <-time.After(15 * time.Second):
		fmt.Println("    ⏳ 场景二等待超时 (部分驱动可能处理较慢)")
	}
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
