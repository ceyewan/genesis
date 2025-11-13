package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	clog "github.com/ceyewan/genesis/pkg/log"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func main() {
	fmt.Println("=== clog 高级功能演示 ===")
	fmt.Println("本指南演示 clog 在实际应用场景中的高级用法")

	// 初始化全局 logger
	initGlobalLogger()

	// 演示1: HTTP 服务集成
	fmt.Println("\n🚀 示例1: HTTP 服务与中间件集成")
	demoHTTPServiceIntegration()

	// 演示2: 复杂业务流程
	fmt.Println("\n🏭 示例2: 复杂业务流程日志追踪")
	demoComplexBusinessProcess()

	// 演示3: 错误处理与监控
	fmt.Println("\n🔍 示例3: 错误处理与监控")
	demoErrorHandlingAndMonitoring()

	// 演示4: 性能优化技巧
	fmt.Println("\n⚡ 示例4: 性能优化技巧")
	demoPerformanceOptimization()

	fmt.Println("\n✅ 高级功能演示完成！")
	fmt.Println("💡 提示: 查看 examples/rotation/main.go 了解日志轮转功能")
}

// initGlobalLogger 初始化全局 logger
func initGlobalLogger() {
	config := clog.GetDefaultConfig("production")

	if err := clog.Init(context.Background(), config, clog.WithNamespace("advanced-demo")); err != nil {
		fmt.Printf("❌ 初始化 logger 失败: %v\n", err)
		return
	}

	clog.Info("高级演示服务启动成功")
}

// demoHTTPServiceIntegration 演示 HTTP 服务集成
func demoHTTPServiceIntegration() {
	// 创建 HTTP 服务
	r := setupHTTPServer()

	// 模拟几个请求
	makeTestRequests(r)

	fmt.Println("✅ HTTP 服务集成演示完成")
}

// setupHTTPServer 设置 HTTP 服务器
func setupHTTPServer() *gin.Engine {
	r := gin.New()

	// 添加中间件
	r.Use(TraceMiddleware())
	r.Use(LoggingMiddleware())
	r.Use(RecoveryMiddleware())

	// 设置路由
	r.POST("/api/users", createUserHandler)
	r.GET("/api/users/:id", getUserHandler)
	r.POST("/api/orders", createOrderHandler)

	return r
}

// TraceMiddleware 链路追踪中间件
func TraceMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		startTime := time.Now()

		// 获取或生成 trace ID
		traceID := c.GetHeader("X-Trace-ID")
		if traceID == "" {
			traceID = uuid.New().String()
		}

		// 注入到 context
		ctx := clog.WithTraceID(c.Request.Context(), traceID)
		c.Request = c.Request.WithContext(ctx)

		// 设置响应头
		c.Header("X-Trace-ID", traceID)
		c.Header("X-Process-Time", time.Since(startTime).String())

		c.Next()
	}
}

// LoggingMiddleware 日志记录中间件
func LoggingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		method := c.Request.Method

		c.Next()

		// 记录请求完成日志
		latency := time.Since(start)
		status := c.Writer.Status()

		logger := clog.WithContext(c.Request.Context()).Namespace("http")

		logger.Info("HTTP 请求完成",
			clog.String("method", method),
			clog.String("path", path),
			clog.Int("status", status),
			clog.Duration("latency", latency),
			clog.String("client_ip", c.ClientIP()),
		)

		// 记录错误请求
		if status >= 400 {
			logger.Error("HTTP 请求异常",
				clog.Int("status", status),
				clog.String("error", c.Errors.String()),
			)
		}
	}
}

// RecoveryMiddleware 异常恢复中间件
func RecoveryMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				// 记录 panic 信息
				logger := clog.WithContext(c.Request.Context()).Namespace("recovery")
				logger.Error("HTTP 请求发生 panic",
					clog.Any("panic", err),
					clog.String("path", c.Request.URL.Path),
				)

				// 返回错误响应
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":    "内部服务器错误",
					"trace_id": c.GetHeader("X-Trace-ID"),
				})

				c.Abort()
			}
		}()

		c.Next()
	}
}

// createUserHandler 创建用户处理器
func createUserHandler(c *gin.Context) {
	logger := clog.WithContext(c.Request.Context()).Namespace("user")

	logger.Info("开始创建用户请求")

	// 模拟业务处理
	time.Sleep(50 * time.Millisecond)

	// 模拟用户创建逻辑
	userID := uuid.New().String()

	logger.Info("用户创建成功",
		clog.String("user_id", userID),
		clog.String("email", "newuser@example.com"),
	)

	c.JSON(http.StatusCreated, gin.H{
		"user_id":    userID,
		"email":      "newuser@example.com",
		"created_at": time.Now().Format(time.RFC3339),
	})
}

// getUserHandler 获取用户处理器
func getUserHandler(c *gin.Context) {
	logger := clog.WithContext(c.Request.Context()).Namespace("user")

	userID := c.Param("id")
	logger.Info("查询用户信息", clog.String("user_id", userID))

	// 模拟数据库查询
	time.Sleep(30 * time.Millisecond)

	logger.Info("用户信息查询成功", clog.String("user_id", userID))

	c.JSON(http.StatusOK, gin.H{
		"user_id": userID,
		"name":    "示例用户",
		"email":   "user@example.com",
	})
}

// createOrderHandler 创建订单处理器
func createOrderHandler(c *gin.Context) {
	logger := clog.WithContext(c.Request.Context()).Namespace("order")

	logger.Info("开始创建订单")

	// 模拟复杂订单处理流程
	orderID := processOrderCreation(c.Request.Context())

	logger.Info("订单创建成功",
		clog.String("order_id", orderID),
		clog.Float64("amount", 99.99),
	)

	c.JSON(http.StatusCreated, gin.H{
		"order_id": orderID,
		"amount":   99.99,
		"status":   "pending",
	})
}

// makeTestRequests 发送测试请求
func makeTestRequests(r *gin.Engine) {
	// 这里只是演示，实际应该启动 HTTP 服务器
	fmt.Println("   📡 模拟 HTTP 请求处理...")

	// 模拟请求日志
	logger := clog.Namespace("simulation")

	logger.Info("模拟创建用户请求",
		clog.String("method", "POST"),
		clog.String("path", "/api/users"),
		clog.String("trace_id", "sim-trace-001"),
	)

	logger.Info("模拟查询用户请求",
		clog.String("method", "GET"),
		clog.String("path", "/api/users/123"),
		clog.String("trace_id", "sim-trace-002"),
	)

	logger.Info("模拟创建订单请求",
		clog.String("method", "POST"),
		clog.String("path", "/api/orders"),
		clog.String("trace_id", "sim-trace-003"),
	)
}

// demoComplexBusinessProcess 演示复杂业务流程
func demoComplexBusinessProcess() {
	// 模拟电商订单处理流程
	traceID := uuid.New().String()
	ctx := clog.WithTraceID(context.Background(), traceID)

	logger := clog.WithContext(ctx)
	logger.Info("开始处理电商订单流程",
		clog.String("order_id", "ORDER-123456"),
		clog.Float64("amount", 299.99),
	)

	// 处理订单的各个阶段
	if err := processOrderWorkflow(ctx, "ORDER-123456"); err != nil {
		logger.Error("订单处理失败",
			clog.Err(err),
			clog.String("order_id", "ORDER-123456"),
		)
		return
	}

	logger.Info("订单处理流程完成",
		clog.String("order_id", "ORDER-123456"),
		clog.String("status", "completed"),
	)
}

// processOrderWorkflow 处理订单工作流
func processOrderWorkflow(ctx context.Context, orderID string) error {
	logger := clog.WithContext(ctx)

	// 阶段1: 库存检查
	inventoryLogger := logger.Namespace("inventory")
	if err := checkInventory(ctx, orderID); err != nil {
		inventoryLogger.Error("库存检查失败", clog.Err(err))
		return err
	}

	// 阶段2: 支付处理
	paymentLogger := logger.Namespace("payment")
	if err := processPayment(ctx, orderID, 299.99); err != nil {
		paymentLogger.Error("支付处理失败", clog.Err(err))
		return err
	}

	// 阶段3: 订单创建
	orderLogger := logger.Namespace("order")
	if err := createOrderRecord(ctx, orderID); err != nil {
		orderLogger.Error("订单创建失败", clog.Err(err))
		return err
	}

	// 阶段4: 发送通知
	notificationLogger := logger.Namespace("notification")
	if err := sendOrderNotification(ctx, orderID); err != nil {
		notificationLogger.Warn("通知发送失败，但订单处理成功", clog.Err(err))
		// 不返回错误，因为订单处理已经完成
	}

	return nil
}

// checkInventory 检查库存
func checkInventory(ctx context.Context, orderID string) error {
	logger := clog.WithContext(ctx).Namespace("inventory")

	logger.Info("开始检查库存", clog.String("order_id", orderID))
	time.Sleep(20 * time.Millisecond)

	// 模拟库存充足的情况
	logger.Info("库存检查通过", clog.String("order_id", orderID))
	return nil
}

// processPayment 处理支付
func processPayment(ctx context.Context, orderID string, amount float64) error {
	logger := clog.WithContext(ctx).Namespace("payment")

	logger.Info("开始处理支付",
		clog.String("order_id", orderID),
		clog.Float64("amount", amount),
	)

	time.Sleep(100 * time.Millisecond)

	// 模拟支付成功
	logger.Info("支付处理成功",
		clog.String("order_id", orderID),
		clog.String("payment_id", "PAY-"+uuid.New().String()),
	)

	return nil
}

// createOrderRecord 创建订单记录
func createOrderRecord(ctx context.Context, orderID string) error {
	logger := clog.WithContext(ctx).Namespace("database")

	logger.Info("创建订单记录", clog.String("order_id", orderID))
	time.Sleep(30 * time.Millisecond)

	logger.Info("订单记录创建成功", clog.String("order_id", orderID))
	return nil
}

// sendOrderNotification 发送订单通知
func sendOrderNotification(ctx context.Context, orderID string) error {
	logger := clog.WithContext(ctx).Namespace("notification")

	logger.Info("发送订单通知", clog.String("order_id", orderID))
	time.Sleep(50 * time.Millisecond)

	// 模拟偶尔失败
	if time.Now().Unix()%3 == 0 {
		return fmt.Errorf("邮件服务暂时不可用")
	}

	logger.Info("订单通知发送成功", clog.String("order_id", orderID))
	return nil
}

// demoErrorHandlingAndMonitoring 演示错误处理与监控
func demoErrorHandlingAndMonitoring() {
	traceID := uuid.New().String()
	ctx := clog.WithTraceID(context.Background(), traceID)

	logger := clog.WithContext(ctx).Namespace("monitoring")

	// 演示不同类型的错误处理
	demoDatabaseErrors(ctx)
	demoAPICallErrors(ctx)
	demoBusinessLogicErrors(ctx)

	// 演示监控指标记录
	logger.Info("系统健康检查",
		clog.String("status", "healthy"),
		clog.Int("active_connections", 150),
		clog.Float64("cpu_usage", 45.5),
		clog.Float64("memory_usage", 67.2),
	)
}

// demoDatabaseErrors 演示数据库错误处理
func demoDatabaseErrors(ctx context.Context) {
	logger := clog.WithContext(ctx).Namespace("database")

	// 模拟连接错误
	logger.Error("数据库连接失败",
		clog.String("error_type", "connection_timeout"),
		clog.String("host", "db-primary"),
		clog.Int("port", 5432),
		clog.Duration("timeout", 5*time.Second),
	)

	// 模拟查询错误
	logger.Error("数据库查询失败",
		clog.String("error_type", "query_syntax"),
		clog.String("table", "users"),
		clog.String("query", "SELECT * FROM users WHERE email = 'test@'"),
	)
}

// demoAPICallErrors 演示 API 调用错误处理
func demoAPICallErrors(ctx context.Context) {
	logger := clog.WithContext(ctx).Namespace("api")

	// 模拟超时错误
	logger.Error("外部 API 调用超时",
		clog.String("service", "payment-service"),
		clog.String("endpoint", "/api/v1/payments"),
		clog.Duration("timeout", 3*time.Second),
		clog.Int("http_code", 0),
	)

	// 模拟业务错误
	logger.Error("支付服务返回业务错误",
		clog.String("service", "payment-service"),
		clog.String("error_code", "INSUFFICIENT_BALANCE"),
		clog.String("error_message", "账户余额不足"),
		clog.Int("http_code", 400),
	)
}

// demoBusinessLogicErrors 演示业务逻辑错误处理
func demoBusinessLogicErrors(ctx context.Context) {
	logger := clog.WithContext(ctx).Namespace("business")

	// 模拟验证错误
	logger.Error("用户数据验证失败",
		clog.String("error_type", "validation"),
		clog.String("field", "email"),
		clog.String("value", "invalid-email"),
		clog.String("reason", "邮箱格式不正确"),
	)

	// 模拟权限错误
	logger.Error("用户权限不足",
		clog.String("error_type", "authorization"),
		clog.String("user_id", "user-123"),
		clog.String("required_role", "admin"),
		clog.String("current_role", "user"),
	)
}

// demoPerformanceOptimization 演示性能优化技巧
func demoPerformanceOptimization() {
	logger := clog.Namespace("performance")

	// 演示批量操作
	demoBatchOperations()

	// 演示异步日志记录
	demoAsyncLogging()

	// 演示条件日志记录
	demoConditionalLogging()

	logger.Info("性能优化演示完成")
}

// demoBatchOperations 演示批量操作
func demoBatchOperations() {
	logger := clog.Namespace("batch")

	startTime := time.Now()

	// 模拟批量用户导入
	userIDs := []string{"user-1", "user-2", "user-3", "user-4", "user-5"}

	logger.Info("开始批量导入用户",
		clog.Int("batch_size", len(userIDs)),
	)

	for _, userID := range userIDs {
		logger.Info("处理用户",
			clog.String("user_id", userID),
			clog.String("operation", "import"),
		)
		time.Sleep(10 * time.Millisecond) // 模拟处理时间
	}

	logger.Info("批量导入完成",
		clog.Int("processed_count", len(userIDs)),
		clog.Duration("total_time", time.Since(startTime)),
	)
}

// demoAsyncLogging 演示异步日志记录
func demoAsyncLogging() {
	logger := clog.Namespace("async")

	// 在高并发场景下，可以使用缓冲通道进行异步日志记录
	// 这里只是演示概念，实际实现需要更复杂的架构

	logger.Info("异步日志记录演示",
		clog.String("technique", "buffered_logging"),
		clog.Int("buffer_size", 1000),
		clog.Int("worker_count", 3),
	)
}

// demoConditionalLogging 演示条件日志记录
func demoConditionalLogging() {
	logger := clog.Namespace("conditional")

	// 只在特定条件下记录详细日志
	shouldLogDetails := true

	if shouldLogDetails {
		logger.Debug("详细调试信息",
			clog.String("condition", "debug_mode_enabled"),
			clog.String("component", "data_processor"),
		)
	}

	// 基于采样率的日志记录
	sampleRate := 0.1 // 10% 的采样率
	if time.Now().UnixNano()%100 < int64(sampleRate*100) {
		logger.Info("采样日志记录",
			clog.Float64("sample_rate", sampleRate),
			clog.String("purpose", "performance_monitoring"),
		)
	}
}

// processOrderCreation 处理订单创建（用于 HTTP 演示）
func processOrderCreation(ctx context.Context) string {
	logger := clog.WithContext(ctx).Namespace("order")

	logger.Info("开始处理订单创建")
	time.Sleep(80 * time.Millisecond)

	orderID := "ORDER-" + uuid.New().String()
	logger.Info("订单创建完成", clog.String("order_id", orderID))

	return orderID
}
