package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/ratelimit"
)

func main() {
	ctx := context.Background()

	// 1. 创建 Logger
	logger, err := clog.New(&clog.Config{
		Level:  "info",
		Format: "console",
		Output: "stdout",
	})
	if err != nil {
		log.Fatalf("failed to create logger: %v", err)
	}

	fmt.Println("\n=== Genesis RateLimit 组件示例 ===")
	fmt.Println("本示例演示限流组件的两种使用模式:")
	fmt.Println("  1. 单机模式 (Standalone): 基于内存的限流")
	fmt.Println("  2. 分布式模式 (Distributed): 基于 Redis 的限流")
	fmt.Println()

	// 示例 1: 单机模式
	fmt.Println("=== 示例 1: 单机模式 ===")
	standaloneExample(ctx, logger)

	// 示例 2: 分布式模式（需要 Redis）
	fmt.Println("\n=== 示例 2: 分布式模式 ===")
	distributedExample(ctx, logger)

	// 示例 3: Gin 中间件（使用单机模式）
	fmt.Println("\n=== 示例 3: Gin 中间件 ===")
	ginExample(logger)
}

// standaloneExample 单机模式示例
func standaloneExample(ctx context.Context, logger clog.Logger) {
	// 创建单机限流器
	limiter, err := ratelimit.NewStandalone(&ratelimit.StandaloneConfig{
		CleanupInterval: 1 * time.Minute,
		IdleTimeout:     5 * time.Minute,
	}, ratelimit.WithLogger(logger))
	if err != nil {
		logger.Error("failed to create standalone limiter", clog.Error(err))
		return
	}
	// Note: 单机模式的 limiter 会在后台 goroutine 清理，不需要显式关闭

	// 定义限流规则: 5 QPS, 突发 10
	limit := ratelimit.Limit{
		Rate:  5,  // 每秒 5 个请求
		Burst: 10, // 突发允许 10 个
	}

	// 模拟请求
	key := "user:123"
	successCount := 0
	failedCount := 0

	fmt.Printf("测试限流规则: Rate=%.0f QPS, Burst=%d\n", limit.Rate, limit.Burst)
	fmt.Println("快速发送 20 个请求...")

	for i := 0; i < 20; i++ {
		allowed, err := limiter.Allow(ctx, key, limit)
		if err != nil {
			logger.Error("allow failed", clog.Error(err))
			continue
		}

		if allowed {
			successCount++
			fmt.Printf("  请求 %2d: ✓ 通过\n", i+1)
		} else {
			failedCount++
			fmt.Printf("  请求 %2d: ✗ 被限流\n", i+1)
		}

		// 稍微延迟以便观察
		time.Sleep(10 * time.Millisecond)
	}

	fmt.Printf("\n结果: 成功=%d, 失败=%d\n", successCount, failedCount)

	// 等待一段时间后重试
	fmt.Println("\n等待 1 秒后重试...")
	time.Sleep(1 * time.Second)

	allowed, _ := limiter.Allow(ctx, key, limit)
	if allowed {
		fmt.Println("✓ 等待后请求通过（令牌已恢复）")
	} else {
		fmt.Println("✗ 等待后仍被限流")
	}
}

// distributedExample 分布式模式示例
func distributedExample(ctx context.Context, logger clog.Logger) {
	// 创建 Redis 连接器
	redisConn, err := connector.NewRedis(&connector.RedisConfig{
		Addr:        "127.0.0.1:6379",
		Password:    "genesis_redis_password",
		DialTimeout: 2 * time.Second,
	}, connector.WithLogger(logger))
	if err != nil {
		logger.Warn("跳过分布式模式示例: Redis 连接器创建失败", clog.Error(err))
		return
	}
	defer redisConn.Close()

	// 测试连接
	if err := redisConn.GetClient().Ping(ctx).Err(); err != nil {
		logger.Warn("跳过分布式模式示例: Redis 不可用", clog.Error(err))
		return
	}

	// 创建分布式限流器
	limiter, err := ratelimit.NewDistributed(redisConn, &ratelimit.DistributedConfig{
		Prefix: "example:ratelimit:",
	}, ratelimit.WithLogger(logger))
	if err != nil {
		logger.Error("failed to create distributed limiter", clog.Error(err))
		return
	}

	// 定义限流规则：更严格的限流，便于观察效果
	limit := ratelimit.Limit{
		Rate:  10, // 每秒 10 个请求
		Burst: 15, // 突发允许 15 个
	}

	// 测试
	key := "api:/users"
	fmt.Printf("测试分布式限流: Rate=%.0f QPS, Burst=%d\n", limit.Rate, limit.Burst)
	fmt.Println("快速发送 25 个请求...")

	successCount := 0
	failedCount := 0
	for i := 0; i < 25; i++ {
		allowed, err := limiter.Allow(ctx, key, limit)
		if err != nil {
			logger.Error("allow failed", clog.Error(err))
			continue
		}

		if allowed {
			successCount++
			fmt.Printf("  请求 %2d: ✓ 通过\n", i+1)
		} else {
			failedCount++
			fmt.Printf("  请求 %2d: ✗ 被限流\n", i+1)
		}

		// 极短延迟，模拟高并发
		time.Sleep(5 * time.Millisecond)
	}

	fmt.Printf("\n结果: 成功=%d, 失败=%d\n", successCount, failedCount)

	// 等待一段时间后重试
	fmt.Println("\n等待 1 秒后重试...")
	time.Sleep(1 * time.Second)

	allowed, _ := limiter.Allow(ctx, key, limit)
	if allowed {
		fmt.Println("✓ 等待后请求通过（令牌已恢复）")
	} else {
		fmt.Println("✗ 等待后仍被限流")
	}
}

// ginExample Gin 中间件示例
func ginExample(logger clog.Logger) {
	// 创建单机限流器
	limiter, err := ratelimit.NewStandalone(&ratelimit.StandaloneConfig{
		CleanupInterval: 1 * time.Minute,
		IdleTimeout:     5 * time.Minute,
	}, ratelimit.WithLogger(logger))
	if err != nil {
		logger.Error("failed to create limiter", clog.Error(err))
		return
	}

	r := gin.New()
	r.Use(gin.Recovery())

	// 定义不同路径的限流规则
	pathLimits := map[string]ratelimit.Limit{
		"/api/test":   {Rate: 10, Burst: 15},   // 测试接口：10 QPS，突发 15
		"/api/login":  {Rate: 5, Burst: 8},     // 登录接口限流更严格
		"/api/data":   {Rate: 100, Burst: 200}, // 数据接口限流较宽松
		"/api/upload": {Rate: 2, Burst: 3},     // 上传接口限流最严格
	}

	// 使用路径限流中间件（注意：key 是 IP，所以同一个 IP 访问同一个路径会被限流）
	apiGroup := r.Group("/api")
	apiGroup.Use(ratelimit.GinMiddleware(limiter,
		func(c *gin.Context) string {
			// 只使用路径作为 key，这样所有请求到同一路径都会共享限流器
			return c.Request.URL.Path
		},
		func(c *gin.Context) ratelimit.Limit {
			// 根据路径返回对应的限流规则
			if limit, ok := pathLimits[c.Request.URL.Path]; ok {
				return limit
			}
			return ratelimit.Limit{Rate: 50, Burst: 100}
		}))

	// 测试接口
	apiGroup.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "success",
			"time":    time.Now().Format(time.RFC3339),
		})
	})

	apiGroup.POST("/login", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "login success",
		})
	})

	apiGroup.GET("/data", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"data": []int{1, 2, 3, 4, 5},
		})
	})

	apiGroup.POST("/upload", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "upload success",
		})
	})

	logger.Info("starting gin server on :8080")
	fmt.Println("\nGin server is running on :8080")
	fmt.Println("\n测试命令（注意：限流器已修改为按路径限流，所以访问同一路径会触发限流）:")
	fmt.Println()
	fmt.Println("  # 测试 /api/test 限流 (Rate=10 QPS, Burst=15)")
	fmt.Println("  # 快速发送 20 个请求，前 15 个应该通过（Burst），后 5 个被限流")
	fmt.Println("  for i in {1..20}; do curl http://localhost:8080/api/test && echo; sleep 0.01; done")
	fmt.Println()
	fmt.Println("  # 测试登录接口限流 (Rate=5 QPS, Burst=8)")
	fmt.Println("  # 快速发送 12 个请求，前 8 个应该通过，后 4 个被限流")
	fmt.Println("  for i in {1..12}; do curl -X POST http://localhost:8080/api/login && echo; sleep 0.01; done")
	fmt.Println()
	fmt.Println("  # 测试上传接口限流 (Rate=2 QPS, Burst=3)")
	fmt.Println("  # 快速发送 6 个请求，前 3 个应该通过，后 3 个被限流")
	fmt.Println("  for i in {1..6}; do curl -X POST http://localhost:8080/api/upload && echo; sleep 0.01; done")
	fmt.Println()

	if err := r.Run(":8080"); err != nil {
		logger.Error("failed to start server", clog.Error(err))
	}
}
