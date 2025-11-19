package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/ceyewan/genesis/pkg/connector"
	"github.com/ceyewan/genesis/pkg/container"
	"github.com/ceyewan/genesis/pkg/mq"
)

// Order 订单事件模型
type Order struct {
	ID     string  `json:"id"`
	Amount float64 `json:"amount"`
	Status string  `json:"status"`
}

func main() {
	fmt.Println("=== Genesis MQ Component Example ===")

	// 1. 初始化容器 (使用 JetStream 模式)
	app := initContainer()
	defer app.Close()

	// 2. 演示：广播订阅 (Subscribe)
	demoSubscribe(app)

	// 3. 演示：队列订阅 (QueueSubscribe)
	demoQueueSubscribe(app)

	// 4. 演示：发布消息
	demoPublish(app)

	// 等待消息处理完成
	time.Sleep(2 * time.Second)
}

func initContainer() *container.Container {
	fmt.Println("\n--- 1. Initializing Container ---")

	cfg := &container.Config{
		// NATS 连接配置
		NATS: &connector.NATSConfig{
			URL:           "nats://127.0.0.1:4222",
			Name:          "genesis-mq-example",
			ReconnectWait: 2 * time.Second,
			MaxReconnects: 5,
		},
		// MQ 组件配置
		MQ: &mq.Config{
			Driver: mq.DriverNatsJetStream, // 使用 JetStream 模式
			JetStream: &mq.JetStreamConfig{
				AutoCreateStream: true,
			},
		},
	}

	app, err := container.New(cfg)
	if err != nil {
		panic(fmt.Sprintf("failed to initialize container: %v", err))
	}
	fmt.Println("Container initialized successfully")
	return app
}

func demoSubscribe(app *container.Container) {
	fmt.Println("\n--- 2. Demo: Subscribe (Broadcast) ---")
	ctx := context.Background()

	// 订阅 "orders.created" 主题
	// 所有订阅者都会收到消息
	_, err := app.MQ.Subscribe(ctx, "orders.created", func(ctx context.Context, msg mq.Message) error {
		fmt.Printf("[Broadcast] Received order: %s\n", string(msg.Data()))
		return nil // 自动 Ack
	})

	if err != nil {
		fmt.Printf("Subscribe failed: %v\n", err)
	} else {
		fmt.Println("Subscribed to 'orders.created' (Broadcast)")
	}
}

func demoQueueSubscribe(app *container.Container) {
	fmt.Println("\n--- 3. Demo: QueueSubscribe (Load Balanced) ---")
	ctx := context.Background()

	// 模拟两个 Worker 订阅同一个队列组 "order_processors"
	// 消息只会被其中一个 Worker 处理
	var wg sync.WaitGroup
	wg.Add(2)

	for i := 1; i <= 2; i++ {
		workerID := i
		_, err := app.MQ.QueueSubscribe(ctx, "orders.created", "order_processors", func(ctx context.Context, msg mq.Message) error {
			var order Order
			if err := json.Unmarshal(msg.Data(), &order); err != nil {
				return err
			}
			fmt.Printf("[Worker %d] Processing order: %s, Amount: %.2f\n", workerID, order.ID, order.Amount)
			return nil // 自动 Ack
		})
		if err != nil {
			fmt.Printf("Worker %d subscribe failed: %v\n", workerID, err)
		} else {
			fmt.Printf("Worker %d subscribed to 'orders.created' (Queue: order_processors)\n", workerID)
		}
	}
}

func demoPublish(app *container.Container) {
	fmt.Println("\n--- 4. Demo: Publish ---")
	ctx := context.Background()

	// 发送 5 个订单消息
	for i := 1; i <= 5; i++ {
		order := Order{
			ID:     fmt.Sprintf("ORD-%d", i),
			Amount: float64(i * 100),
			Status: "created",
		}
		data, _ := json.Marshal(order)

		if err := app.MQ.Publish(ctx, "orders.created", data); err != nil {
			fmt.Printf("Publish failed: %v\n", err)
		} else {
			fmt.Printf("Published order: %s\n", order.ID)
		}
	}
}
