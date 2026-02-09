package metrics

import (
	"time"

	"github.com/gin-gonic/gin"
)

// GinHTTPMiddleware 返回一个可重用的 Gin 中间件，用于记录 HTTP RED 指标
func GinHTTPMiddleware(httpMetrics *HTTPServerMetrics) gin.HandlerFunc {
	return func(c *gin.Context) {
		if httpMetrics == nil {
			c.Next()
			return
		}

		start := time.Now()
		c.Next()

		route := c.FullPath()
		if route == "" {
			// 未命中路由时统一收敛，避免将原始 URL Path 作为标签导致高基数
			route = UnknownRoute
		}

		httpMetrics.Observe(c.Request.Context(), c.Request.Method, route, c.Writer.Status(), time.Since(start))
	}
}
