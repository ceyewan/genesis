package main

import (
	"context"
	"net/http"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

func main() {
	ctx := context.Background()

	// 0. 初始化 Logger (开启 Trace Context 关联)
	// 这样日志中会自动包含 trace_id 和 span_id，方便与 Metrics 联动排查
	logger, _ := clog.New(
		&clog.Config{Level: "info", Format: "console"},
		clog.WithTraceContext(),
	)

	// 1. 创建 Metrics 配置
	cfg := metrics.NewDevDefaultConfig("gin-demo")
	cfg.EnableRuntime = true

	// 2. 初始化 Metrics (开启 Runtime 监控)
	meter, err := metrics.New(cfg)
	if err != nil {
		logger.Fatal("Failed to create metrics", clog.Error(err))
	}
	defer func() {
		if err := meter.Shutdown(ctx); err != nil {
			logger.Error("Failed to shutdown metrics", clog.Error(err))
		}
	}()

	// 3. 创建业务自定义指标
	// 示例：在线用户数（Gauge）
	onlineUsers, err := meter.Gauge(
		"im_online_users",
		"Current online user count",
	)
	if err != nil {
		logger.Fatal("Failed to create gauge", clog.Error(err))
	}

	// 示例：消息处理延迟（Histogram），使用自定义 buckets
	messageLag, err := meter.Histogram(
		"im_message_lag_seconds",
		"Message processing lag",
		metrics.WithBuckets([]float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}),
	)
	if err != nil {
		logger.Fatal("Failed to create histogram", clog.Error(err))
	}

	// 4. 创建 Gin 路由器
	router := gin.Default()

	// 5. 添加 OpenTelemetry 标准中间件
	// 这一行代码替代了原来的 metricsMiddleware，自动记录 HTTP Method, Path, Status, Duration 等
	// 并且自动处理 Trace Context
	router.Use(otelgin.Middleware("gin-demo"))

	// 6. 定义业务路由
	router.GET("/", func(c *gin.Context) {
		ctx := c.Request.Context()

		// 模拟用户上线
		onlineUsers.Inc(ctx, metrics.L("region", "shanghai"))

		// 记录一条带 TraceID 的日志
		logger.InfoContext(ctx, "User online event received", clog.String("region", "shanghai"))

		c.JSON(http.StatusOK, gin.H{
			"message": "Hello from Cloud Native Metrics",
		})
	})

	router.POST("/message", func(c *gin.Context) {
		ctx := c.Request.Context()

		// 模拟消息处理
		start := time.Now()
		logger.InfoContext(ctx, "Processing message", clog.String("type", "text"))

		time.Sleep(10 * time.Millisecond) // 模拟处理耗时
		lag := time.Since(start).Seconds()

		// 记录消息延迟 (支持自定义 Buckets)
		messageLag.Record(ctx, lag, metrics.L("type", "text"))

		c.JSON(http.StatusOK, gin.H{"status": "sent"})
	})

	// 7. 启动 Gin HTTP 服务
	go func() {
		logger.Info("Starting Gin server on :8080")
		if err := router.Run(":8080"); err != nil {
			logger.Error("Gin server error", clog.Error(err))
		}
	}()

	logger.Info("Prometheus metrics available at http://localhost:9090/metrics")
	logger.Info("Try accessing:")
	logger.Info("  curl http://localhost:8080/")
	logger.Info("  curl -X POST http://localhost:8080/message")

	// 8. 等待退出信号
	select {}
}
