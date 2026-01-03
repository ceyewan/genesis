package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/trace"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

func main() {
	ctx := context.Background()

	// 1. 初始化 Trace 组件
	// 这里配置了上报到 localhost:4317 (Tempo/Jaeger 默认端口)
	// 如果你没有运行 Collector，控制台可能会有连接错误的日志，但不影响服务运行
	traceShutdown, err := trace.Init(&trace.Config{
		ServiceName: "trace-demo",
		Endpoint:    "localhost:4317",
		Sampler:     1.0,  // 开发环境全量采样
		Insecure:    true, // 本地演示使用非加密链接
	})
	if err != nil {
		log.Fatalf("Failed to init trace: %v", err)
	}
	defer func() {
		if err := traceShutdown(ctx); err != nil {
			log.Printf("Failed to shutdown trace: %v", err)
		}
	}()

	// 2. 初始化 Logger (带 Trace Context 关联)
	logger, _ := clog.New(
		&clog.Config{Level: "info", Format: "console"},
		clog.WithTraceContext(),
	)

	// 3. 创建 Gin 路由器
	r := gin.Default()

	// 4. 添加 Tracing 中间件
	// 它会自动生成 Root Span，并将 TraceID 注入 Context
	r.Use(otelgin.Middleware("trace-demo"))

	r.GET("/ping", func(c *gin.Context) {
		// 获取带有 TraceID 的 Context
		ctx := c.Request.Context()

		// 5. 演示手动创建子 Span (用于追踪特定业务逻辑)
		// 获取全局 Tracer
		tracer := otel.Tracer("business-logic")
		// 开始一个新的 Span
		ctx, span := tracer.Start(ctx, "heavy-calculation")
		defer span.End()

		// 模拟业务逻辑
		time.Sleep(50 * time.Millisecond)

		// 为 Span 添加自定义属性 (可以在 Trace 后端检索)
		span.SetAttributes(attribute.String("calculation.type", "matrix"))

		// 6. 记录日志 (自动包含 TraceID 和 SpanID)
		// 注意：这里的日志关联的是当前的子 Span (heavy-calculation)
		logger.InfoContext(ctx, "Calculation finished", clog.String("result", "success"))

		c.JSON(http.StatusOK, gin.H{"message": "pong", "trace_id": span.SpanContext().TraceID().String()})
	})

	log.Println("Server starting on :8081...")
	if err := r.Run(":8081"); err != nil {
		log.Fatal(err)
	}
}
