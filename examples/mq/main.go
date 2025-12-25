package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"sync"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/mq"
)

// Order 订单事件模型
type Order struct {
	ID     string  `json:"id"`
	Amount float64 `json:"amount"`
	Status string  `json:"status"`
}

func main() {
	// 命令行参数
	var (
		mode = flag.String("mode", "jetstream", "MQ mode: core or jetstream")
	)
	flag.Parse()

	fmt.Printf("=== Genesis MQ Component Example (Mode: %s) ===\n", *mode)

	// 1. 初始化连接器和 MQ 客户端
	mqClient, conn := initMQClient(*mode)
	defer conn.Close()

	// 2. 演示：广播订阅 (Subscribe)
	demoSubscribe(mqClient)

	// 3. 演示：队列订阅 (Subscribe with QueueGroup)
	demoQueueSubscribe(mqClient)

	// 4. 演示：Channel 模式订阅
	demoSubscribeChan(mqClient)

	// 5. 演示：发布消息
	demoPublish(mqClient)

	// 等待消息处理完成
	time.Sleep(2 * time.Second)
}

func initMQClient(mode string) (mq.Client, connector.NATSConnector) {
	fmt.Printf("\n--- 1. Initializing MQ Client (Mode: %s) ---\n", mode)

	// 创建 logger
	logger, err := clog.New(&clog.Config{
		Level:  "info",
		Format: "console",
		Output: "stdout",
	})
	if err != nil {
		panic(fmt.Sprintf("failed to create logger: %v", err))
	}

	// 创建 NATS 连接器
	natsConn, err := connector.NewNATS(&connector.NATSConfig{
		URL:           "nats://127.0.0.1:4222",
		Name:          "genesis-mq-example",
		ReconnectWait: 2 * time.Second,
		MaxReconnects: 5,
	}, connector.WithLogger(logger))
	if err != nil {
		panic(fmt.Sprintf("failed to create NATS connector: %v", err))
	}

	// 建立连接
	ctx := context.Background()
	if err := natsConn.Connect(ctx); err != nil {
		panic(fmt.Sprintf("failed to connect to NATS: %v", err))
	}

	// 根据 mode 选择驱动类型
	var driver mq.Driver

	if mode == "core" {
		driver = mq.NewNatsCoreDriver(natsConn, logger)
	} else {
		// JetStream 模式
		jetStreamConfig := &mq.JetStreamConfig{
			AutoCreateStream: true,
		}
		driver, err = mq.NewNatsJetStreamDriver(natsConn, jetStreamConfig, logger)
		if err != nil {
			panic(fmt.Sprintf("failed to create NATS JetStream driver: %v", err))
		}
	}

	// 创建 MQ 客户端
	// 注意：现在 New 接受 Driver 接口，而不是 Connector
	mqClient, err := mq.New(driver, mq.WithLogger(logger))
	if err != nil {
		panic(fmt.Sprintf("failed to create MQ client: %v", err))
	}

	fmt.Printf("MQ Client initialized successfully (Mode: %s)\n", mode)
	return mqClient, natsConn
}

func demoSubscribe(mqClient mq.Client) {
	fmt.Println("\n--- 2. Demo: Subscribe (Broadcast) ---")
	ctx := context.Background()

	// 订阅 "orders.created" 主题
	// 所有订阅者都会收到消息
	_, err := mqClient.Subscribe(ctx, "orders.created", func(ctx context.Context, msg mq.Message) error {
		fmt.Printf("[Broadcast] Received order: %s\n", string(msg.Data()))
		return nil // 自动 Ack
	})

	if err != nil {
		fmt.Printf("Subscribe failed: %v\n", err)
	} else {
		fmt.Println("Subscribed to 'orders.created' (Broadcast)")
	}
}

func demoQueueSubscribe(mqClient mq.Client) {
	fmt.Println("\n--- 3. Demo: Subscribe (Load Balanced with QueueGroup) ---")
	ctx := context.Background()

	// 模拟两个 Worker 订阅同一个队列组 "order_processors"
	// 消息只会被其中一个 Worker 处理
	var wg sync.WaitGroup
	wg.Add(2)

	for i := 1; i <= 2; i++ {
		workerID := i
		// 使用 WithQueueGroup 选项替代原来的 QueueSubscribe 方法
		_, err := mqClient.Subscribe(ctx, "orders.created", func(ctx context.Context, msg mq.Message) error {
			var order Order
			if err := json.Unmarshal(msg.Data(), &order); err != nil {
				return err
			}
			fmt.Printf("[Worker %d] Processing order: %s, Amount: %.2f\n", workerID, order.ID, order.Amount)
			return nil // 自动 Ack
		}, mq.WithQueueGroup("order_processors"))

		if err != nil {
			fmt.Printf("Worker %d subscribe failed: %v\n", workerID, err)
		} else {
			fmt.Printf("Worker %d subscribed to 'orders.created' (Queue: order_processors)\n", workerID)
		}
	}
}

func demoSubscribeChan(mqClient mq.Client) {
	fmt.Println("\n--- 4. Demo: SubscribeChan ---")
	ctx := context.Background()

	// 使用 Channel 模式订阅
	ch, sub, err := mqClient.SubscribeChan(ctx, "orders.created", mq.WithBufferSize(10))
	if err != nil {
		fmt.Printf("SubscribeChan failed: %v\n", err)
		return
	}

	// 启动一个 goroutine 来处理消息
	go func() {
		defer sub.Unsubscribe()
		fmt.Println("[ChanListener] Listening for messages...")

		// 简单起见，这里只处理 2 个消息然后退出
		count := 0
		for msg := range ch {
			fmt.Printf("[ChanListener] Received via channel: %s\n", string(msg.Data()))
			msg.Ack()

			count++
			if count >= 2 {
				break
			}
		}
		fmt.Println("[ChanListener] Stopped listening")
	}()
}

func demoPublish(mqClient mq.Client) {
	fmt.Println("\n--- 5. Demo: Publish ---")
	ctx := context.Background()

	// 发送 5 个订单消息
	for i := 1; i <= 5; i++ {
		order := Order{
			ID:     fmt.Sprintf("ORD-%d", i),
			Amount: float64(i * 100),
			Status: "created",
		}
		data, _ := json.Marshal(order)

		if err := mqClient.Publish(ctx, "orders.created", data); err != nil {
			fmt.Printf("Publish failed: %v\n", err)
		} else {
			fmt.Printf("Published order: %s\n", order.ID)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
