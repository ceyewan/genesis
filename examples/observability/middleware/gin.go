// Package middleware 提供 Gin/gRPC 的可观测性中间件示例。
//
// 这些是最佳实践演示，不是 Genesis 核心组件的一部分。
// 用户可以根据需要复制到自己的项目中使用或修改。
package middleware

import (
	"strconv"
	"time"

	"github.com/ceyewan/genesis/metrics"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

// Observability 返回一个集成了 Trace/Metrics/Logging 的 Gin 中间件
//
// 使用示例：
//
//	r := gin.New()
//	r.Use(middleware.Observability(
//	    middleware.WithServiceName("api-gateway"),
//	    middleware.WithHistogram(histogram),
//	)...)
func Observability(opts ...MiddlewareOption) []gin.HandlerFunc {
	cfg := &middlewareConfig{
		serviceName: "api",
	}
	for _, opt := range opts {
		opt(cfg)
	}

	middlewares := []gin.HandlerFunc{}

	// 1. Panic Recover
	middlewares = append(middlewares, gin.Recovery())

	// 2. Tracing
	if cfg.serviceName != "" {
		middlewares = append(middlewares, otelgin.Middleware(cfg.serviceName))
	}

	// 3. Metrics
	if cfg.histogram != nil {
		middlewares = append(middlewares, func(c *gin.Context) {
			start := time.Now()
			c.Next()

			cfg.histogram.Record(c.Request.Context(), time.Since(start).Seconds(),
				metrics.L("method", c.Request.Method),
				metrics.L("path", c.FullPath()),
				metrics.L("status", strconv.Itoa(c.Writer.Status())),
			)
		})
	}

	return middlewares
}

// middlewareConfig 中间件配置
type middlewareConfig struct {
	serviceName string
	histogram   metrics.Histogram
}

// MiddlewareOption 中间件选项
type MiddlewareOption func(*middlewareConfig)

// WithServiceName 设置服务名（用于 Trace）
func WithServiceName(name string) MiddlewareOption {
	return func(cfg *middlewareConfig) {
		cfg.serviceName = name
	}
}

// WithHistogram 设置自定义 Histogram 指标
func WithHistogram(h metrics.Histogram) MiddlewareOption {
	return func(cfg *middlewareConfig) {
		cfg.histogram = h
	}
}
