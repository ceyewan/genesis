package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/examples/observability/middleware"
	"github.com/ceyewan/genesis/examples/observability/proto"
	"github.com/ceyewan/genesis/metrics"
	genesistrace "github.com/ceyewan/genesis/trace"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	oteltrace "go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// 全局组件
var (
	logger clog.Logger
	meter  metrics.Meter

	// 自定义指标
	httpRequestDuration  metrics.Histogram
	mqProcessingDuration metrics.Histogram
)

// ============================================================================
// 1. MQ 模拟 (Context Propagation 难点演示)
// ============================================================================

type MQMessage struct {
	Payload string
	Headers map[string]string // 用于承载 TraceContext
}

var mqChannel = make(chan MQMessage, 100)

func startMQConsumer() {
	go func() {
		for msg := range mqChannel {
			// A. 提取 Context (关键!)
			// 将 MQ Header 中的 Trace 信息还原到 Context 中
			// 这样后续的 Log/Trace 才能关联上之前的链路
			ctx := genesistrace.Extract(context.Background(), msg.Headers)

			// B. 开始一个新的 Span (作为消费者 Span)
			tracer := otel.Tracer("mq-consumer")
			ctx, span := tracer.Start(ctx, "process_order_notification")
			defer span.End()

			// 开始计时 (Metrics)
			start := time.Now()

			logger.InfoContext(ctx, "MQ Consumer received message",
				clog.String("payload", msg.Payload),
			)

			// 模拟处理耗时
			time.Sleep(20 * time.Millisecond)
			logger.InfoContext(ctx, "Notification sent", clog.String("status", "success"))

			// 记录 MQ 处理耗时
			mqProcessingDuration.Record(ctx, time.Since(start).Seconds(), metrics.L("status", "success"))
		}
	}()
}

// ============================================================================
// 2. gRPC Service (Logic Layer)
// ============================================================================

type OrderServiceImpl struct {
	proto.UnimplementedOrderServiceServer
}

func (s *OrderServiceImpl) CreateOrder(ctx context.Context, req *proto.CreateOrderRequest) (*proto.CreateOrderResponse, error) {
	// otelgrpc 已经自动创建了 Span，并注入到 ctx 中
	// 我们可以直接添加属性
	span := oteltrace.SpanFromContext(ctx) // 获取当前 Span
	span.SetAttributes(attribute.String("order.user_id", req.UserId))

	// 或者如果需要创建子 Span:
	// tracer := otel.Tracer("order-service")
	// ctx, span = tracer.Start(ctx, "business-logic")
	// defer span.End()

	logger.InfoContext(ctx, "Processing order",
		clog.String("user_id", req.UserId),
		clog.String("product_id", req.ProductId),
	)

	// 模拟 DB 操作
	time.Sleep(10 * time.Millisecond)

	// 发送 MQ 消息 (Context Propagation 注入)
	headers := make(map[string]string)
	genesistrace.Inject(ctx, headers)

	msg := MQMessage{
		Payload: fmt.Sprintf("Order created for %s", req.UserId),
		Headers: headers,
	}
	mqChannel <- msg

	return &proto.CreateOrderResponse{
		OrderId: "ORD-" + req.UserId,
		Status:  "CREATED",
	}, nil
}

func startGRPCServer() error {
	lis, err := net.Listen("tcp", ":9090")
	if err != nil {
		return err
	}

	// 注册 otelgrpc 拦截器
	s := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
	)
	proto.RegisterOrderServiceServer(s, &OrderServiceImpl{})

	go func() {
		logger.Info("gRPC server listening on :9090")
		if err := s.Serve(lis); err != nil {
			logger.Error("gRPC serve failed", clog.Error(err))
		}
	}()
	return nil
}

// ============================================================================
// 3. API Gateway (HTTP Layer)
// ============================================================================

func startGateway() {
	// 初始化 gRPC 客户端 (带 otel 拦截器)
	// otelgrpc 既负责 Trace 也负责 Metrics (自动上报 rpc.client.duration 等)
	conn, err := grpc.NewClient("localhost:9090",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil {
		logger.Fatal("Failed to connect to gRPC server", clog.Error(err))
	}
	grpcClient := proto.NewOrderServiceClient(conn)

	// 初始化 Gin
	r := gin.New()
	// 使用统一的可观测性中间件 (Panic Recover + Tracing + Metrics)
	r.Use(middleware.Observability(
		middleware.WithServiceName("api-gateway"),
		middleware.WithHistogram(httpRequestDuration),
	))

	r.POST("/orders", func(c *gin.Context) {
		ctx := c.Request.Context()
		var req struct {
			UserID    string `json:"user_id"`
			ProductID string `json:"product_id"`
		}
		if err := c.BindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		logger.InfoContext(ctx, "Received order request", clog.String("user_id", req.UserID))

		// 调用 gRPC
		resp, err := grpcClient.CreateOrder(ctx, &proto.CreateOrderRequest{
			UserId:    req.UserID,
			ProductId: req.ProductID,
		})
		if err != nil {
			logger.ErrorContext(ctx, "gRPC call failed", clog.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal Server Error"})
			return
		}

		c.JSON(http.StatusOK, resp)
	})

	logger.Info("Gateway listening on :8080")
	if err := r.Run(":8080"); err != nil {
		logger.Fatal("Gateway run failed", clog.Error(err))
	}
}

// ============================================================================
// Main Orchestrator
// ============================================================================

func main() {
	ctx := context.Background()

	// 1. 初始化 Trace (Tempo/Jaeger)
	// 支持 OTLP_ENDPOINT 环境变量覆盖（用于 Docker 环境）
	otlpEndpoint := os.Getenv("OTLP_ENDPOINT")
	if otlpEndpoint == "" {
		otlpEndpoint = "localhost:4317" // 本地开发默认值
	}
	traceShutdown, err := genesistrace.Init(&genesistrace.Config{
		ServiceName: "observability-demo",
		Endpoint:    otlpEndpoint,
		Sampler:     1.0,
		Insecure:    true,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer traceShutdown(ctx)

	// 2. 初始化 Logger (带 Trace 关联)
	logger, _ = clog.New(
		&clog.Config{Level: "info", Format: "json"},
		clog.WithTraceContext(),
	)

	// 3. 初始化 Metrics
	metricsCfg := metrics.NewDevDefaultConfig("observability-demo")
	metricsCfg.Port = 9100 // 避免与 gRPC 端口冲突
	metricsCfg.EnableRuntime = true // 开启 Runtime 监控 (Goroutine, GC)
	meter, err = metrics.New(metricsCfg)
	if err != nil {
		log.Fatal(err)
	}
	defer meter.Shutdown(ctx)

	// 初始化自定义指标
	httpRequestDuration, _ = meter.Histogram("http_request_duration_seconds", "HTTP request duration",
		metrics.WithBuckets([]float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}))

	mqProcessingDuration, _ = meter.Histogram("mq_processing_duration_seconds", "MQ processing duration",
		metrics.WithBuckets([]float64{0.005, 0.01, 0.05, 0.1, 0.5, 1}))

	// 4. 启动组件
	logger.Info("Starting Observability Demo...")

	startMQConsumer()                         // 启动 MQ 消费者
	if err := startGRPCServer(); err != nil { // 启动 gRPC 服务
		log.Fatal(err)
	}

	// 稍微等待 gRPC 启动
	time.Sleep(time.Second)

	// 启动 Gateway (阻塞主线程)
	startGateway()
}
