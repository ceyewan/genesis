package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/ceyewan/genesis/pkg/clog"
	clogtypes "github.com/ceyewan/genesis/pkg/clog/types"
	"github.com/ceyewan/genesis/pkg/telemetry"
	"github.com/ceyewan/genesis/pkg/telemetry/types"
	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

var (
	// 日志器（将在 main 函数中初始化）
	mainLogger    clog.Logger
	serviceLogger clog.Logger
	grpcLogger    clog.Logger
	httpLogger    clog.Logger

	// 自定义业务指标
	requestCounter   types.Counter
	errorCounter     types.Counter
	responseTimeHist types.Histogram
	activeUsersGauge types.Gauge
	messageSizeHist  types.Histogram
)

// OrderService 模拟订单服务
type OrderService struct {
	orders sync.Map // 模拟订单存储
}

// OrderRequest 订单请求
type OrderRequest struct {
	UserID  int64   `json:"user_id"`
	Product string  `json:"product"`
	Amount  float64 `json:"amount"`
}

// OrderResponse 订单响应
type OrderResponse struct {
	OrderID   string
	Status    string
	Timestamp int64
}

func main() {
	// 初始化日志器
	logger, err := clog.New(&clogtypes.Config{
		Level:  "info",
		Format: "console",
		Output: "stdout",
	}, &clogtypes.Option{
		NamespaceParts: []string{"example", "telemetry"},
	})
	if err != nil {
		log.Fatalf("初始化日志记录器失败: %v", err)
	}

	// 创建子日志器
	mainLogger = logger
	serviceLogger = logger
	grpcLogger = logger
	httpLogger = logger

	mainLogger.Info("=== Genesis Telemetry Examples ===")

	// 示例 1: 基础遥测配置
	basicTelemetryExample()

	// 示例 2: 高级遥测配置（带自定义指标）
	advancedTelemetryExample()

	// 示例 3: 完整服务示例（gRPC + HTTP + 指标 + 追踪）
	fullServiceExample()
}

// 示例 1: 基础遥测配置
func basicTelemetryExample() {
	fmt.Println("--- Example 1: Basic Telemetry Configuration ---")

	// 创建基础配置
	cfg := &telemetry.Config{
		ServiceName:          "basic-telemetry-demo",
		ExporterType:         "stdout",    // 输出到控制台便于演示
		PrometheusListenAddr: ":9091",     // Prometheus 指标端口
		SamplerType:          "always_on", // 全采样便于演示
	}

	// 初始化遥测
	tel, err := telemetry.New(cfg)
	if err != nil {
		log.Printf("Failed to create telemetry: %v\n", err)
		return
	}
	defer tel.Shutdown(context.Background())

	fmt.Printf("✓ Telemetry initialized with service: %s\n", cfg.ServiceName)
	fmt.Printf("✓ Metrics available at: http://localhost%s/metrics\n", cfg.PrometheusListenAddr)
	fmt.Printf("✓ Traces output to: stdout\n")
	fmt.Println()
}

// 示例 2: 高级遥测配置（带自定义指标）
func advancedTelemetryExample() {
	fmt.Println("--- Example 2: Advanced Telemetry with Custom Metrics ---")

	// 创建高级配置
	cfg := &telemetry.Config{
		ServiceName:          "advanced-telemetry-demo",
		ExporterType:         "stdout",
		ExporterEndpoint:     "http://localhost:14268/api/traces", // Jaeger 端点
		PrometheusListenAddr: ":9092",
		SamplerType:          "trace_id_ratio",
		SamplerRatio:         0.1, // 10% 采样率
	}

	// 初始化遥测
	tel, err := telemetry.New(cfg)
	if err != nil {
		log.Printf("Failed to create telemetry: %v\n", err)
		return
	}
	defer tel.Shutdown(context.Background())

	// 创建自定义指标
	ctx := context.Background()

	// 请求计数器
	requestCounter, err = tel.Meter().Counter(
		"custom_requests_total",
		"Total number of custom requests by type and status",
	)
	if err != nil {
		log.Printf("Failed to create counter: %v\n", err)
		return
	}

	// 响应时间直方图
	responseTimeHist, err = tel.Meter().Histogram(
		"custom_response_duration_seconds",
		"Custom operation response time in seconds",
		types.WithUnit("s"),
	)
	if err != nil {
		log.Printf("Failed to create histogram: %v\n", err)
		return
	}

	// 活跃用户数仪表盘
	activeUsersGauge, err = tel.Meter().Gauge(
		"active_users_total",
		"Number of active users",
	)
	if err != nil {
		log.Printf("Failed to create gauge: %v\n", err)
		return
	}

	// 记录一些示例数据
	requestCounter.Inc(ctx, types.Label{Key: "type", Value: "api"}, types.Label{Key: "status", Value: "success"})
	requestCounter.Inc(ctx, types.Label{Key: "type", Value: "api"}, types.Label{Key: "status", Value: "error"})

	responseTimeHist.Record(ctx, 0.125, types.Label{Key: "operation", Value: "query"})
	responseTimeHist.Record(ctx, 0.245, types.Label{Key: "operation", Value: "update"})

	activeUsersGauge.Set(ctx, 42, types.Label{Key: "service", Value: "user-service"})

	// 创建追踪示例
	tracer := tel.Tracer()
	_, span := tracer.Start(ctx, "advanced-operation", types.WithSpanKind(types.SpanKindInternal))

	// 模拟一些工作
	time.Sleep(50 * time.Millisecond)

	// 设置属性和状态
	span.SetAttributes(
		types.Attribute{Key: "user.id", Value: "12345"},
		types.Attribute{Key: "operation.type", Value: "data-processing"},
		types.Attribute{Key: "data.size", Value: 1024},
	)
	span.SetStatus(types.StatusCodeOk, "Operation completed successfully")

	span.End()

	fmt.Printf("✓ Custom metrics created and recorded\n")
	fmt.Printf("✓ Trace span created with attributes\n")
	fmt.Printf("✓ Metrics available at: http://localhost%s/metrics\n", cfg.PrometheusListenAddr)
	fmt.Println()
}

// 示例 3: 完整服务示例（gRPC + HTTP + 指标 + 追踪）
func fullServiceExample() {
	fmt.Println("--- Example 3: Full Service with gRPC + HTTP + Metrics + Tracing ---")

	// 创建服务配置
	cfg := &telemetry.Config{
		ServiceName:          "order-service",
		ExporterType:         "stdout",
		PrometheusListenAddr: ":9093",
		SamplerType:          "always_on",
	}

	// 初始化遥测
	tel, err := telemetry.New(cfg)
	if err != nil {
		log.Printf("Failed to create telemetry: %v\n", err)
		return
	}

	// 初始化自定义指标
	if err := initServiceMetrics(tel); err != nil {
		log.Printf("Failed to init metrics: %v\n", err)
		return
	}

	// 启动服务
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup

	// 启动 gRPC 服务器
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := startGRPCServer(ctx, tel); err != nil {
			grpcLogger.Error("gRPC server failed", clog.Error(err))
		}
	}()

	// 启动 HTTP 服务器
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := startHTTPServer(ctx, tel); err != nil {
			httpLogger.Error("HTTP server failed", clog.Error(err))
		}
	}()

	// 启动业务指标模拟器
	wg.Add(1)
	go func() {
		defer wg.Done()
		simulateBusinessMetrics(ctx, tel)
	}()

	// 等待退出信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	fmt.Printf("✓ Order service started\n")
	fmt.Printf("✓ gRPC server: localhost:8081\n")
	fmt.Printf("✓ HTTP server: localhost:8080\n")
	fmt.Printf("✓ Metrics: http://localhost%s/metrics\n", cfg.PrometheusListenAddr)
	fmt.Println("✓ Press Ctrl+C to stop")
	fmt.Println()

	<-quit
	mainLogger.Info("收到退出信号，开始优雅关闭")
	cancel()

	// 优雅关闭
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	mainLogger.Info("关闭遥测系统")
	if err := tel.Shutdown(shutdownCtx); err != nil {
		mainLogger.Error("failed to shutdown telemetry", clog.Error(err))
	}

	// 等待所有 goroutine 结束
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		mainLogger.Info("所有服务已优雅关闭")
	case <-shutdownCtx.Done():
		mainLogger.Warn("关闭超时，强制退出")
	}
}

// 初始化服务指标
func initServiceMetrics(tel telemetry.Telemetry) error {
	var err error

	// 请求计数器
	requestCounter, err = tel.Meter().Counter(
		"order_requests_total",
		"Total number of order requests by type and status",
	)
	if err != nil {
		return fmt.Errorf("failed to create request counter: %w", err)
	}

	// 错误计数器
	errorCounter, err = tel.Meter().Counter(
		"order_errors_total",
		"Total number of order errors by type",
	)
	if err != nil {
		return fmt.Errorf("failed to create error counter: %w", err)
	}

	// 响应时间直方图
	responseTimeHist, err = tel.Meter().Histogram(
		"order_response_duration_seconds",
		"Order operation response time in seconds",
		types.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("failed to create response time histogram: %w", err)
	}

	// 消息大小直方图
	messageSizeHist, err = tel.Meter().Histogram(
		"order_message_size_bytes",
		"Size of order messages in bytes",
		types.WithUnit("bytes"),
	)
	if err != nil {
		return fmt.Errorf("failed to create message size histogram: %w", err)
	}

	serviceLogger.Info("服务指标初始化完成")
	return nil
}

// 启动 gRPC 服务器
func startGRPCServer(ctx context.Context, tel telemetry.Telemetry) error {
	lis, err := net.Listen("tcp", ":8081")
	if err != nil {
		return fmt.Errorf("failed to listen on :8081: %w", err)
	}

	// 创建 gRPC 服务器，集成遥测拦截器
	server := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			tel.GRPCServerInterceptor(),
			loggingInterceptor(),
		),
	)

	// 注册服务（这里只是示例，实际应用中会注册真实的服务）
	reflection.Register(server)

	grpcLogger.Info("gRPC 服务器启动", clog.String("address", ":8081"))

	// 启动服务器
	go func() {
		if err := server.Serve(lis); err != nil {
			grpcLogger.Error("gRPC server serve failed", clog.Error(err))
		}
	}()

	// 等待上下文取消
	<-ctx.Done()
	grpcLogger.Info("正在关闭 gRPC 服务器")
	server.GracefulStop()
	grpcLogger.Info("gRPC 服务器已关闭")

	return nil
}

// 启动 HTTP 服务器
func startHTTPServer(ctx context.Context, tel telemetry.Telemetry) error {
	// 设置 Gin 模式
	gin.SetMode(gin.ReleaseMode)

	engine := gin.New()

	// 添加遥测中间件和自定义中间件
	engine.Use(
		tel.HTTPMiddleware(),
		corsMiddleware(),
		recoveryMiddleware(),
	)

	// 注册路由
	setupRoutes(engine, tel)

	server := &http.Server{
		Addr:           ":8080",
		Handler:        engine,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 1 << 20, // 1MB
	}

	httpLogger.Info("HTTP 服务器启动", clog.String("address", ":8080"))

	// 启动服务器
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			httpLogger.Error("HTTP server serve failed", clog.Error(err))
		}
	}()

	// 等待上下文取消
	<-ctx.Done()
	httpLogger.Info("正在关闭 HTTP 服务器")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		httpLogger.Error("HTTP server shutdown failed", clog.Error(err))
		return err
	}

	httpLogger.Info("HTTP 服务器已关闭")
	return nil
}

// 设置 HTTP 路由
func setupRoutes(engine *gin.Engine, tel telemetry.Telemetry) {
	orderService := &OrderService{}

	// API v1 路由组
	v1 := engine.Group("/api/v1")
	{
		// 订单相关路由
		orders := v1.Group("/orders")
		{
			orders.POST("/create", func(c *gin.Context) {
				handleCreateOrder(c, tel, orderService)
			})
			orders.GET("/:id/status", func(c *gin.Context) {
				handleGetOrderStatus(c, tel, orderService)
			})
			orders.PUT("/:id/cancel", func(c *gin.Context) {
				handleCancelOrder(c, tel, orderService)
			})
		}

		// 健康检查
		v1.GET("/health", handleHealthCheck)

		// 指标端点
		v1.GET("/metrics/info", handleMetricsInfo)
	}

	// 根路径
	engine.GET("/", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"service": "order-service",
			"version": "1.0.0",
			"status":  "running",
			"telemetry": gin.H{
				"metrics": "/api/v1/metrics/info",
				"health":  "/api/v1/health",
			},
		})
	})
}

// 订单处理函数
func handleCreateOrder(c *gin.Context, tel telemetry.Telemetry, service *OrderService) {
	start := time.Now()
	ctx := c.Request.Context()

	var req OrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		requestCounter.Inc(ctx, types.Label{Key: "operation", Value: "create_order"}, types.Label{Key: "status", Value: "invalid_request"})
		errorCounter.Inc(ctx, types.Label{Key: "type", Value: "validation_error"})
		c.JSON(400, gin.H{"error": "Invalid request"})
		return
	}

	// 创建追踪 span
	spanCtx, span := tel.Tracer().Start(ctx, "create_order", types.WithSpanKind(types.SpanKindServer))
	defer span.End()

	span.SetAttributes(
		types.Attribute{Key: "user.id", Value: req.UserID},
		types.Attribute{Key: "order.product", Value: req.Product},
		types.Attribute{Key: "order.amount", Value: req.Amount},
	)

	serviceLogger.Info("创建订单",
		clog.Int64("user_id", req.UserID),
		clog.String("product", req.Product),
		clog.Float64("amount", req.Amount))

	// 模拟业务逻辑
	time.Sleep(100 * time.Millisecond)

	// 模拟随机错误（10% 概率）
	if time.Now().UnixNano()%10 == 0 {
		span.SetStatus(types.StatusCodeError, "Insufficient inventory")
		span.RecordError(fmt.Errorf("insufficient inventory for product: %s", req.Product))

		requestCounter.Inc(spanCtx, types.Label{Key: "operation", Value: "create_order"}, types.Label{Key: "status", Value: "failed"})
		errorCounter.Inc(spanCtx, types.Label{Key: "type", Value: "inventory_error"})

		c.JSON(409, gin.H{"error": "Insufficient inventory"})
		return
	}

	// 创建订单
	orderID := fmt.Sprintf("ORDER-%d-%d", req.UserID, time.Now().UnixNano())
	response := OrderResponse{
		OrderID:   orderID,
		Status:    "created",
		Timestamp: time.Now().Unix(),
	}

	// 存储订单（模拟）
	service.orders.Store(orderID, response)

	// 记录消息大小
	requestBody, _ := c.GetRawData()
	messageSizeHist.Record(spanCtx, float64(len(requestBody)))

	// 记录指标
	requestCounter.Inc(spanCtx, types.Label{Key: "operation", Value: "create_order"}, types.Label{Key: "status", Value: "success"})
	responseTimeHist.Record(spanCtx, time.Since(start).Seconds(), types.Label{Key: "operation", Value: "create_order"})

	span.SetStatus(types.StatusCodeOk, "Order created successfully")

	c.JSON(200, response)
}

func handleGetOrderStatus(c *gin.Context, tel telemetry.Telemetry, service *OrderService) {
	start := time.Now()
	ctx := c.Request.Context()
	orderID := c.Param("id")

	// 创建追踪 span
	spanCtx, span := tel.Tracer().Start(ctx, "get_order_status", types.WithSpanKind(types.SpanKindServer))
	defer span.End()

	span.SetAttributes(types.Attribute{Key: "order.id", Value: orderID})

	// 查询订单
	if value, ok := service.orders.Load(orderID); ok {
		order := value.(OrderResponse)

		requestCounter.Inc(spanCtx, types.Label{Key: "operation", Value: "get_order_status"}, types.Label{Key: "status", Value: "success"})
		responseTimeHist.Record(spanCtx, time.Since(start).Seconds(), types.Label{Key: "operation", Value: "get_order_status"})

		span.SetStatus(types.StatusCodeOk, "Order found")
		c.JSON(200, order)
	} else {
		requestCounter.Inc(spanCtx, types.Label{Key: "operation", Value: "get_order_status"}, types.Label{Key: "status", Value: "not_found"})
		errorCounter.Inc(spanCtx, types.Label{Key: "type", Value: "order_not_found"})

		span.SetStatus(types.StatusCodeError, "Order not found")
		c.JSON(404, gin.H{"error": "Order not found"})
	}
}

func handleCancelOrder(c *gin.Context, tel telemetry.Telemetry, service *OrderService) {
	start := time.Now()
	ctx := c.Request.Context()
	orderID := c.Param("id")

	// 创建追踪 span
	spanCtx, span := tel.Tracer().Start(ctx, "cancel_order", types.WithSpanKind(types.SpanKindServer))
	defer span.End()

	span.SetAttributes(types.Attribute{Key: "order.id", Value: orderID})

	// 删除订单（模拟取消）
	if _, ok := service.orders.Load(orderID); ok {
		service.orders.Delete(orderID)

		requestCounter.Inc(spanCtx, types.Label{Key: "operation", Value: "cancel_order"}, types.Label{Key: "status", Value: "success"})
		responseTimeHist.Record(spanCtx, time.Since(start).Seconds(), types.Label{Key: "operation", Value: "cancel_order"})

		span.SetStatus(types.StatusCodeOk, "Order cancelled successfully")
		c.JSON(200, gin.H{"message": "Order cancelled successfully"})
	} else {
		requestCounter.Inc(spanCtx, types.Label{Key: "operation", Value: "cancel_order"}, types.Label{Key: "status", Value: "not_found"})
		errorCounter.Inc(spanCtx, types.Label{Key: "type", Value: "order_not_found"})

		span.SetStatus(types.StatusCodeError, "Order not found")
		c.JSON(404, gin.H{"error": "Order not found"})
	}
}

// 辅助函数
func handleHealthCheck(c *gin.Context) {
	c.JSON(200, gin.H{
		"status":    "healthy",
		"timestamp": time.Now().Unix(),
		"service":   "order-service",
	})
}

func handleMetricsInfo(c *gin.Context) {
	c.JSON(200, gin.H{
		"message": "Metrics are available at /metrics endpoint",
		"custom_metrics": []string{
			"order_requests_total",
			"order_errors_total",
			"order_response_duration_seconds",
			"order_message_size_bytes",
		},
	})
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

func recoveryMiddleware() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered interface{}) {
		httpLogger.Error("HTTP request panic recovered",
			clog.String("method", c.Request.Method),
			clog.String("path", c.Request.URL.Path),
			clog.Any("panic", recovered))

		errorCounter.Inc(c.Request.Context(), types.Label{Key: "type", Value: "panic"})
		c.JSON(500, gin.H{"error": "Internal server error"})
	})
}

func loggingInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()

		grpcLogger.Debug("gRPC 请求开始", clog.String("method", info.FullMethod))

		resp, err := handler(ctx, req)

		duration := time.Since(start)
		code := codes.OK
		if err != nil {
			if st, ok := status.FromError(err); ok {
				code = st.Code()
			}
		}

		grpcLogger.Info("gRPC 请求完成",
			clog.String("method", info.FullMethod),
			clog.Duration("duration", duration),
			clog.String("status", code.String()))

		// 记录业务指标
		status := "success"
		if err != nil {
			status = "error"
		}

		requestCounter.Inc(ctx, types.Label{Key: "operation", Value: "grpc_request"}, types.Label{Key: "status", Value: status})
		responseTimeHist.Record(ctx, duration.Seconds(), types.Label{Key: "operation", Value: "grpc_request"})

		return resp, err
	}
}

// 模拟业务指标生成
func simulateBusinessMetrics(ctx context.Context, tel telemetry.Telemetry) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	serviceLogger.Info("开始模拟业务指标生成")

	for {
		select {
		case <-ctx.Done():
			serviceLogger.Info("停止业务指标生成")
			return
		case <-ticker.C:
			// 模拟后台任务指标
			requestCounter.Inc(ctx, types.Label{Key: "operation", Value: "background_task"}, types.Label{Key: "status", Value: "completed"})

			// 模拟活跃用户数变化
			activeUsers := 20 + time.Now().UnixNano()%50
			activeUsersGauge.Set(ctx, float64(activeUsers), types.Label{Key: "service", Value: "order-service"})

			serviceLogger.Debug("生成模拟业务指标",
				clog.Float64("active_users", float64(activeUsers)))
		}
	}
}
