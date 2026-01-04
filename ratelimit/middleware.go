package ratelimit

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// GinMiddlewareOptions Gin 限流中间件配置
type GinMiddlewareOptions struct {
	WithHeaders bool
	KeyFunc     func(*gin.Context) string
	LimitFunc   func(*gin.Context) Limit
}

// GinMiddleware 创建 Gin 限流中间件
//
// 参数:
//   - limiter: 限流器实例，为 nil 时自动使用 Discard()（始终放行）
//   - opts: 中间件配置（可为空）
//
// 使用示例:
//
//	r := gin.New()
//	r.Use(ratelimit.GinMiddleware(limiter, &ratelimit.GinMiddlewareOptions{
//	    KeyFunc: func(c *gin.Context) string {
//	        return c.ClientIP()
//	    },
//	    LimitFunc: func(c *gin.Context) ratelimit.Limit {
//	        return ratelimit.Limit{Rate: 10, Burst: 20}
//	    },
//	}))
func GinMiddleware(limiter Limiter, opts *GinMiddlewareOptions) gin.HandlerFunc {
	// 如果 limiter 为 nil，使用 Discard() 实例
	if limiter == nil {
		limiter = Discard()
	}

	var keyFunc func(*gin.Context) string
	var limitFunc func(*gin.Context) Limit
	withHeaders := false
	if opts != nil {
		keyFunc = opts.KeyFunc
		limitFunc = opts.LimitFunc
		withHeaders = opts.WithHeaders
	}

	if keyFunc == nil {
		// 默认使用客户端 IP 作为限流键
		keyFunc = func(c *gin.Context) string {
			return c.ClientIP()
		}
	}
	if limitFunc == nil {
		limitFunc = func(c *gin.Context) Limit {
			return Limit{}
		}
	}

	return func(c *gin.Context) {
		// 提取限流键
		key := keyFunc(c)
		if key == "" {
			// 如果无法提取键，记录日志并放行
			c.Next()
			return
		}

		// 获取限流规则
		limit := limitFunc(c)
		if limit.Rate <= 0 || limit.Burst <= 0 {
			// 无效的限流规则，放行
			c.Next()
			return
		}

		if withHeaders {
			// 设置限流相关的响应头
			c.Header("X-RateLimit-Limit", formatLimit(limit))
		}

		// 检查是否允许请求
		allowed, err := limiter.Allow(c.Request.Context(), key, limit)
		if err != nil {
			// 降级策略：限流器出错时放行，避免影响业务
			// 实际生产中可能需要根据具体情况决定是否放行
			c.Next()
			return
		}

		if !allowed {
			if withHeaders {
				c.Header("X-RateLimit-Remaining", "0")
			}
			// 被限流，返回 429
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "rate limit exceeded",
			})
			return
		}

		c.Next()
	}
}

// formatLimit 格式化限流规则为字符串
func formatLimit(limit Limit) string {
	return fmt.Sprintf("rate=%.2f, burst=%d", limit.Rate, limit.Burst)
}
