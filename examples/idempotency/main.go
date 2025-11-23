package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ceyewan/genesis/pkg/clog"
	clogtypes "github.com/ceyewan/genesis/pkg/clog/types"
	"github.com/ceyewan/genesis/pkg/connector"
	"github.com/ceyewan/genesis/pkg/container"
	"github.com/ceyewan/genesis/pkg/idempotency"
	"github.com/ceyewan/genesis/pkg/idempotency/adapter"
)

func main() {
	ctx := context.Background()

	// 1. 创建 Logger
	logger, err := clog.New(&clogtypes.Config{
		Level:  "info",
		Format: "console",
		Output: "stdout",
	}, &clogtypes.Option{
		NamespaceParts: []string{"example", "idempotency"},
	})
	if err != nil {
		log.Fatalf("failed to create logger: %v", err)
	}

	// 2. 使用 Container 初始化 Redis 连接器
	containerCfg := &container.Config{
		Redis: &connector.RedisConfig{
			Addr:        "127.0.0.1:6379",
			Password:    "",
			DialTimeout: 2 * time.Second,
		},
	}

	c, err := container.New(containerCfg, container.WithLogger(logger))
	if err != nil {
		log.Fatalf("failed to create container: %v", err)
	}
	defer c.Close()

	redisConn, err := c.GetRedisConnector(*containerCfg.Redis)
	if err != nil {
		log.Fatalf("failed to get redis connector: %v", err)
	}

	// 测试连接
	if err := redisConn.GetClient().Ping(ctx).Err(); err != nil {
		log.Fatalf("failed to ping redis: %v", err)
	}
	logger.Info("redis connected")

	// 3. 创建幂等组件
	idem, err := idempotency.New(redisConn, &idempotency.Config{
		Prefix:        "example:idem:",
		DefaultTTL:    1 * time.Hour,
		ProcessingTTL: 5 * time.Minute,
	}, idempotency.WithLogger(logger))
	if err != nil {
		log.Fatalf("failed to create idempotency: %v", err)
	}

	// 示例 1: 直接使用幂等组件
	fmt.Println("\n=== Example 1: Direct Usage ===")
	directUsageExample(ctx, idem, logger)

	// 示例 2: Gin 中间件使用
	fmt.Println("\n=== Example 2: Gin Middleware ===")
	ginMiddlewareExample(idem, logger)
}

// directUsageExample 直接使用幂等组件的示例
func directUsageExample(ctx context.Context, idem idempotency.Idempotent, logger clog.Logger) {
	// 模拟业务逻辑
	counter := 0
	businessLogic := func() (any, error) {
		counter++
		logger.Info("executing business logic", clog.Int("counter", counter))
		time.Sleep(100 * time.Millisecond) // 模拟耗时操作
		return map[string]any{
			"result": "success",
			"count":  counter,
			"time":   time.Now().Format(time.RFC3339),
		}, nil
	}

	// 第一次调用
	key := "order:12345"
	result1, err := idem.Do(ctx, key, businessLogic)
	if err != nil {
		logger.Error("first call failed", clog.Error(err))
		return
	}
	logger.Info("first call result", clog.Any("result", result1))

	// 第二次调用（应该返回缓存结果）
	result2, err := idem.Do(ctx, key, businessLogic)
	if err != nil {
		logger.Error("second call failed", clog.Error(err))
		return
	}
	logger.Info("second call result (cached)", clog.Any("result", result2))

	// 检查状态
	status, cachedResult, err := idem.Check(ctx, key)
	if err != nil {
		logger.Error("check failed", clog.Error(err))
		return
	}
	logger.Info("check status", clog.Any("status", status), clog.Any("result", cachedResult))

	// 删除幂等记录
	time.Sleep(1 * time.Second)
	if err := idem.Delete(ctx, key); err != nil {
		logger.Error("delete failed", clog.Error(err))
		return
	}
	logger.Info("idempotency record deleted")

	// 第三次调用（应该重新执行）
	result3, err := idem.Do(ctx, key, businessLogic)
	if err != nil {
		logger.Error("third call failed", clog.Error(err))
		return
	}
	logger.Info("third call result (re-executed)", clog.Any("result", result3))
}

// ginMiddlewareExample Gin 中间件使用示例
func ginMiddlewareExample(idem idempotency.Idempotent, logger clog.Logger) {
	r := gin.New()
	r.Use(gin.Recovery())

	// 使用幂等中间件
	r.Use(adapter.GinMiddleware(idem, nil, idempotency.WithTTL(30*time.Minute)))

	// 计数器用于演示
	orderCounter := 0

	// 创建订单接口
	r.POST("/orders", func(c *gin.Context) {
		// 模拟订单创建
		orderCounter++
		orderID := fmt.Sprintf("ORDER-%d", orderCounter)

		// 模拟耗时操作
		time.Sleep(100 * time.Millisecond)

		c.JSON(http.StatusOK, gin.H{
			"order_id": orderID,
			"status":   "created",
			"time":     time.Now().Format(time.RFC3339),
			"counter":  orderCounter,
		})
	})

	// 带状态码过滤的中间件示例
	r.POST("/payments", adapter.GinMiddlewareWithStatus(
		idem,
		nil,
		[]int{http.StatusOK, http.StatusCreated}, // 只缓存成功状态
		idempotency.WithTTL(1*time.Hour),
	), func(c *gin.Context) {
		// 模拟支付逻辑
		time.Sleep(100 * time.Millisecond)

		c.JSON(http.StatusOK, gin.H{
			"payment_id": "PAY-123",
			"status":     "success",
			"time":       time.Now().Format(time.RFC3339),
		})
	})

	logger.Info("starting gin server on :8080")
	fmt.Println("\nGin server is running on :8080")
	fmt.Println("\nTry these commands:")
	fmt.Println("  # First request (should create order)")
	fmt.Println(`  curl -X POST http://localhost:8080/orders -H "X-Idempotency-Key: req-001"`)
	fmt.Println("\n  # Second request with same key (should return cached result)")
	fmt.Println(`  curl -X POST http://localhost:8080/orders -H "X-Idempotency-Key: req-001"`)
	fmt.Println("\n  # Third request with different key (should create new order)")
	fmt.Println(`  curl -X POST http://localhost:8080/orders -H "X-Idempotency-Key: req-002"`)
	fmt.Println("\n  # Payment request")
	fmt.Println(`  curl -X POST http://localhost:8080/payments -H "X-Idempotency-Key: pay-001"`)
	fmt.Println()

	if err := r.Run(":8080"); err != nil {
		logger.Error("failed to start server", clog.Error(err))
	}
}
