package main

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ceyewan/genesis/metrics"
	"github.com/gin-gonic/gin"
)

func main() {
	ctx := context.Background()

	// 1. 创建 Metrics 配置
	cfg := &metrics.Config{
		Enabled:     true,
		ServiceName: "gin-demo",
		Version:     "v1.0.0",
		Port:        9090,
		Path:        "/metrics",
	}

	// 2. 初始化 Metrics
	meter, err := metrics.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create metrics: %v", err)
	}
	defer func() {
		if err := meter.Shutdown(ctx); err != nil {
			log.Printf("Failed to shutdown metrics: %v", err)
		}
	}()

	// 3. 创建自定义指标
	requestCounter, err := meter.Counter(
		"http_requests_total",
		"Total HTTP requests",
	)
	if err != nil {
		log.Fatalf("Failed to create counter: %v", err)
	}

	requestDuration, err := meter.Histogram(
		"http_request_duration_seconds",
		"HTTP request duration in seconds",
	)
	if err != nil {
		log.Fatalf("Failed to create histogram: %v", err)
	}

	activeRequests, err := meter.Gauge(
		"http_requests_active",
		"Number of active HTTP requests",
	)
	if err != nil {
		log.Fatalf("Failed to create gauge: %v", err)
	}

	// 4. 创建 Gin 路由器
	router := gin.Default()

	// 5. 添加 Metrics 中间件
	router.Use(metricsMiddleware(
		requestCounter,
		requestDuration,
		activeRequests,
	))

	// 6. 定义业务路由
	router.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "Hello from Gin Demo",
		})
	})

	// 模拟慢请求，用于演示活跃连接数
	router.GET("/slow", func(c *gin.Context) {
		time.Sleep(2 * time.Second) // 模拟 2 秒处理时间
		c.JSON(http.StatusOK, gin.H{
			"message": "Slow request completed",
		})
	})

	router.POST("/orders", func(c *gin.Context) {
		// 模拟订单创建
		var order struct {
			Name  string  `json:"name" binding:"required"`
			Price float64 `json:"price" binding:"required,gt=0"`
		}

		if err := c.ShouldBindJSON(&order); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// 模拟业务处理
		time.Sleep(100 * time.Millisecond)

		c.JSON(http.StatusCreated, gin.H{
			"id":    12345,
			"name":  order.Name,
			"price": order.Price,
		})
	})

	router.GET("/users/:id", func(c *gin.Context) {
		id := c.Param("id")
		c.JSON(http.StatusOK, gin.H{
			"id":   id,
			"name": "User " + id,
		})
	})

	router.GET("/error", func(c *gin.Context) {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Something went wrong",
		})
	})

	// 7. 启动 Gin HTTP 服务（在协程中）
	go func() {
		log.Printf("Starting Gin server on :8080")
		if err := router.Run(":8080"); err != nil {
			log.Printf("Gin server error: %v", err)
		}
	}()

	// 8. 模拟客户端请求生成指标
	log.Printf("Starting client simulator...")
	go simulateClient()

	log.Printf("Prometheus metrics available at http://localhost:9090/metrics")

	// 9. 等待退出信号
	select {}
}

// simulateClient 模拟客户端发送请求以生成指标
func simulateClient() {
	time.Sleep(2 * time.Second) // 等待服务器启动

	client := &http.Client{Timeout: 5 * time.Second}

	// 定义要模拟的请求
	requests := []struct {
		method  string
		url     string
		payload string
	}{
		{"GET", "http://localhost:8080/", ""},
		{"POST", "http://localhost:8080/orders", `{"name":"MacBook Pro","price":2499.99}`},
		{"GET", "http://localhost:8080/users/123", ""},
		{"GET", "http://localhost:8080/users/456", ""},
		{"GET", "http://localhost:8080/error", ""},
		{"GET", "http://localhost:8080/slow", ""}, // 慢请求，用于演示活跃连接
		{"GET", "http://localhost:8080/slow", ""}, // 多发几个慢请求
		{"GET", "http://localhost:8080/slow", ""},
	}

	// 循环发送请求
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		for _, req := range requests {
			go func(method, url, payload string) {
				var httpReq *http.Request
				var err error

				if method == "POST" {
					httpReq, err = http.NewRequest(method, url, strings.NewReader(payload))
					if err == nil {
						httpReq.Header.Set("Content-Type", "application/json")
					}
				} else {
					httpReq, err = http.NewRequest(method, url, nil)
				}

				if err != nil {
					log.Printf("Failed to create request: %v", err)
					return
				}

				resp, err := client.Do(httpReq)
				if err != nil {
					log.Printf("Request failed: %v", err)
					return
				}
				resp.Body.Close()
			}(req.method, req.url, req.payload)

			time.Sleep(100 * time.Millisecond)
		}
	}
}

// metricsMiddleware 记录 HTTP 请求的指标
func metricsMiddleware(counter metrics.Counter, duration metrics.Histogram, active metrics.Gauge) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		// 增加活跃请求数
		active.Inc(ctx, metrics.L("method", c.Request.Method))

		// 记录请求耗时
		start := time.Now()
		defer func() {
			elapsed := time.Since(start).Seconds()

			// 记录计数器
			counter.Inc(ctx,
				metrics.L("method", c.Request.Method),
				metrics.L("path", c.Request.URL.Path),
				metrics.L("status", strconv.Itoa(c.Writer.Status())),
			)

			// 记录直方图
			duration.Record(ctx, elapsed,
				metrics.L("method", c.Request.Method),
				metrics.L("path", c.Request.URL.Path),
			)

			// 减少活跃请求数
			active.Dec(ctx, metrics.L("method", c.Request.Method))
		}()

		// 继续处理请求
		c.Next()
	}
}
