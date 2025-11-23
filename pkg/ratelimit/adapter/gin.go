package adapter

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ceyewan/genesis/pkg/ratelimit/types"
)

// GinMiddleware 创建 Gin 限流中间件
//
// 参数:
//   - limiter: 限流器实例
//   - keyFunc: 从请求中提取限流键的函数，如果为 nil，默认使用客户端 IP
//   - limitFunc: 获取限流规则的函数
//
// 使用示例:
//
//	r := gin.New()
//	r.Use(adapter.GinMiddleware(limiter,
//	    nil, // 使用默认的 IP 作为 key
//	    func(c *gin.Context) types.Limit {
//	        return types.Limit{Rate: 10, Burst: 20} // 10 QPS
//	    },
//	))
func GinMiddleware(
	limiter types.Limiter,
	keyFunc func(*gin.Context) string,
	limitFunc func(*gin.Context) types.Limit,
) gin.HandlerFunc {
	if keyFunc == nil {
		// 默认使用客户端 IP 作为限流键
		keyFunc = func(c *gin.Context) string {
			return c.ClientIP()
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

		// 检查是否允许请求
		allowed, err := limiter.Allow(c.Request.Context(), key, limit)
		if err != nil {
			// 降级策略：限流器出错时放行，避免影响业务
			// 实际生产中可能需要根据具体情况决定是否放行
			c.Next()
			return
		}

		if !allowed {
			// 被限流，返回 429
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "rate limit exceeded",
			})
			return
		}

		c.Next()
	}
}

// GinMiddlewareWithHeaders 创建带响应头的 Gin 限流中间件
// 会在响应中添加限流相关的 Header
//
// 使用示例:
//
//	r.Use(adapter.GinMiddlewareWithHeaders(limiter, nil, limitFunc))
func GinMiddlewareWithHeaders(
	limiter types.Limiter,
	keyFunc func(*gin.Context) string,
	limitFunc func(*gin.Context) types.Limit,
) gin.HandlerFunc {
	if keyFunc == nil {
		keyFunc = func(c *gin.Context) string {
			return c.ClientIP()
		}
	}

	return func(c *gin.Context) {
		key := keyFunc(c)
		if key == "" {
			c.Next()
			return
		}

		limit := limitFunc(c)
		if limit.Rate <= 0 || limit.Burst <= 0 {
			c.Next()
			return
		}

		// 设置限流相关的响应头
		c.Header("X-RateLimit-Limit", formatLimit(limit))

		allowed, err := limiter.Allow(c.Request.Context(), key, limit)
		if err != nil {
			c.Next()
			return
		}

		if !allowed {
			c.Header("X-RateLimit-Remaining", "0")
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "rate limit exceeded",
			})
			return
		}

		c.Next()
	}
}

// GinMiddlewarePerUser 创建基于用户 ID 的限流中间件
// 需要用户在上下文中设置 userID
//
// 使用示例:
//
//	r.Use(authMiddleware) // 设置 userID 到 context
//	r.Use(adapter.GinMiddlewarePerUser(limiter, limitFunc))
func GinMiddlewarePerUser(
	limiter types.Limiter,
	limitFunc func(*gin.Context) types.Limit,
) gin.HandlerFunc {
	return GinMiddleware(limiter, func(c *gin.Context) string {
		// 从 context 获取用户 ID
		if userID, exists := c.Get("userID"); exists {
			if uid, ok := userID.(string); ok {
				return "user:" + uid
			}
		}
		// 如果没有用户 ID，使用 IP 作为后备
		return "ip:" + c.ClientIP()
	}, limitFunc)
}

// GinMiddlewarePerPath 创建基于路径的限流中间件
// 不同路径使用不同的限流规则
//
// 使用示例:
//
//	rules := map[string]types.Limit{
//	    "/api/login": {Rate: 5, Burst: 10},    // 登录接口限流更严格
//	    "/api/data":  {Rate: 100, Burst: 200}, // 数据接口限流较宽松
//	}
//	r.Use(adapter.GinMiddlewarePerPath(limiter, rules, defaultLimit))
func GinMiddlewarePerPath(
	limiter types.Limiter,
	pathLimits map[string]types.Limit,
	defaultLimit types.Limit,
) gin.HandlerFunc {
	return GinMiddleware(
		limiter,
		func(c *gin.Context) string {
			// 组合 IP 和路径作为限流键
			return c.ClientIP() + ":" + c.Request.URL.Path
		},
		func(c *gin.Context) types.Limit {
			// 根据路径返回对应的限流规则
			if limit, ok := pathLimits[c.Request.URL.Path]; ok {
				return limit
			}
			return defaultLimit
		},
	)
}

// formatLimit 格式化限流规则为字符串
func formatLimit(limit types.Limit) string {
	return fmt.Sprintf("rate=%.2f, burst=%d", limit.Rate, limit.Burst)
}
