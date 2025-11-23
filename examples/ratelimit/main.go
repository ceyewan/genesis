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
	"github.com/ceyewan/genesis/pkg/ratelimit"
	"github.com/ceyewan/genesis/pkg/ratelimit/adapter"
	"github.com/ceyewan/genesis/pkg/ratelimit/types"
)

func main() {
	ctx := context.Background()

	// 1. 创建 Logger
	logger, err := clog.New(&clogtypes.Config{
		Level:  "info",
		Format: "console",
		Output: "stdout",
	}, &clogtypes.Option{
		NamespaceParts: []string{"example", "ratelimit"},
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
	limiter, err := ratelimit.New(&types.Config{
		Mode: types.ModeStandalone,
		Standalone: types.StandaloneConfig{
			CleanupInterval: 1 * time.Minute,
			IdleTimeout:     5 * time.Minute,
		},
	}, nil, ratelimit.WithLogger(logger))
	if err != nil {
		logger.Error("failed to create standalone limiter", clog.Error(err))
		return
	}
	// Note: 单机模式的 limiter 会在后台 goroutine 清理，不需要显式关闭

	// 定义限流规则: 5 QPS, 突发 10
	limit := types.Limit{
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
	// 使用 Container 初始化 Redis 连接器
	containerCfg := &container.Config{
		Redis: &connector.RedisConfig{
			Addr:        "127.0.0.1:6379",
			Password:    "",
			DialTimeout: 2 * time.Second,
		},
	}

	c, err := container.New(containerCfg, container.WithLogger(logger))
	if err != nil {
		logger.Warn("跳过分布式模式示例: 容器初始化失败", clog.Error(err))
		return
	}
	defer c.Close()

	redisConn, err := c.GetRedisConnector(*containerCfg.Redis)
	if err != nil {
		logger.Warn("跳过分布式模式示例: 获取连接器失败", clog.Error(err))
		return
	}

	// 测试连接
	if err := redisConn.GetClient().Ping(ctx).Err(); err != nil {
		logger.Warn("跳过分布式模式示例: Redis 不可用", clog.Error(err))
		return
	}

	// 创建分布式限流器
	limiter, err := ratelimit.New(&types.Config{
		Mode: types.ModeDistributed,
		Distributed: types.DistributedConfig{
			Prefix: "example:ratelimit:",
		},
	}, redisConn, ratelimit.WithLogger(logger))
	if err != nil {
		logger.Error("failed to create distributed limiter", clog.Error(err))
		return
	}

	// 定义限流规则
	limit := types.Limit{
		Rate:  10, // 每秒 10 个请求
		Burst: 20, // 突发允许 20 个
	}

	// 测试
	key := "api:/users"
	fmt.Printf("测试分布式限流: Rate=%.0f QPS, Burst=%d\n", limit.Rate, limit.Burst)

	successCount := 0
	for i := 0; i < 15; i++ {
		allowed, err := limiter.Allow(ctx, key, limit)
		if err != nil {
			logger.Error("allow failed", clog.Error(err))
			continue
		}

		if allowed {
			successCount++
			fmt.Printf("  请求 %2d: ✓\n", i+1)
		} else {
			fmt.Printf("  请求 %2d: ✗\n", i+1)
		}
	}

	fmt.Printf("\n结果: %d/15 请求通过\n", successCount)
}

// ginExample Gin 中间件示例
func ginExample(logger clog.Logger) {
	// 创建单机限流器
	limiter, err := ratelimit.New(&types.Config{
		Mode: types.ModeStandalone,
	}, nil, ratelimit.WithLogger(logger))
	if err != nil {
		logger.Error("failed to create limiter", clog.Error(err))
		return
	}

	r := gin.New()
	r.Use(gin.Recovery())

	// 全局限流中间件: 10 QPS
	r.Use(adapter.GinMiddleware(limiter, nil, func(c *gin.Context) types.Limit {
		return types.Limit{Rate: 10, Burst: 20}
	}))

	// 定义不同路径的限流规则
	pathLimits := map[string]types.Limit{
		"/api/login":  {Rate: 5, Burst: 10},    // 登录接口限流更严格
		"/api/data":   {Rate: 100, Burst: 200}, // 数据接口限流较宽松
		"/api/upload": {Rate: 2, Burst: 5},     // 上传接口限流最严格
	}

	// 使用路径限流中间件
	apiGroup := r.Group("/api")
	apiGroup.Use(adapter.GinMiddlewarePerPath(limiter, pathLimits, types.Limit{Rate: 50, Burst: 100}))

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
	fmt.Println("\n测试命令:")
	fmt.Println("  # 测试全局限流 (10 QPS)")
	fmt.Println("  for i in {1..15}; do curl http://localhost:8080/api/test; echo; done")
	fmt.Println()
	fmt.Println("  # 测试登录接口限流 (5 QPS)")
	fmt.Println("  for i in {1..10}; do curl -X POST http://localhost:8080/api/login; echo; done")
	fmt.Println()
	fmt.Println("  # 测试数据接口限流 (100 QPS)")
	fmt.Println("  curl http://localhost:8080/api/data")
	fmt.Println()
	fmt.Println("  # 测试上传接口限流 (2 QPS)")
	fmt.Println("  for i in {1..5}; do curl -X POST http://localhost:8080/api/upload; echo; done")
	fmt.Println()

	if err := r.Run(":8080"); err != nil {
		logger.Error("failed to start server", clog.Error(err))
	}
}
