package adapter

import (
	"bytes"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ceyewan/genesis/idempotency/types"
)

// IdempotencyKeyHeader 幂等键的 HTTP Header 名称
const IdempotencyKeyHeader = "X-Idempotency-Key"

// responseWriter 包装 gin.ResponseWriter 以捕获响应内容
type responseWriter struct {
	gin.ResponseWriter
	body       *bytes.Buffer
	statusCode int
}

func (w *responseWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func (w *responseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

// GinMiddleware 创建 Gin 幂等中间件
//
// 参数:
//   - idem: 幂等组件实例
//   - keyFunc: 从请求中提取幂等键的函数，如果为 nil，默认从 Header 中获取
//   - opts: 幂等选项 (如 TTL)
//
// 使用示例:
//
//	r := gin.New()
//	r.Use(adapter.GinMiddleware(idem, nil))
//	// 或自定义 key 提取函数
//	r.Use(adapter.GinMiddleware(idem, func(c *gin.Context) string {
//	    return c.GetHeader("X-Request-ID")
//	}))
func GinMiddleware(idem types.Idempotent, keyFunc func(*gin.Context) string, opts ...types.DoOption) gin.HandlerFunc {
	if keyFunc == nil {
		// 默认从 Header 中获取幂等键
		keyFunc = func(c *gin.Context) string {
			return c.GetHeader(IdempotencyKeyHeader)
		}
	}

	return func(c *gin.Context) {
		// 提取幂等键
		key := keyFunc(c)
		if key == "" {
			// 没有幂等键，直接放行
			c.Next()
			return
		}

		// 包装 ResponseWriter 以捕获响应
		writer := &responseWriter{
			ResponseWriter: c.Writer,
			body:           bytes.NewBuffer(nil),
			statusCode:     http.StatusOK,
		}
		c.Writer = writer

		// 执行幂等逻辑
		_, err := idem.Do(c.Request.Context(), key, func() (any, error) {
			// 执行后续处理器
			c.Next()

			// 检查是否有错误
			if len(c.Errors) > 0 {
				// 如果有错误，返回错误以便不缓存结果
				return nil, c.Errors[0]
			}

			// 返回响应内容作为结果
			return map[string]any{
				"status_code": writer.statusCode,
				"body":        writer.body.Bytes(),
				"headers":     c.Writer.Header(),
			}, nil
		}, opts...)

		if err != nil {
			// 处理幂等错误
			if err == types.ErrProcessing {
				// 请求正在处理中，返回 429
				c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
					"error": "request is being processed",
				})
				return
			}

			if err == types.ErrKeyEmpty {
				// 键为空（不应该发生，因为上面已经检查过）
				c.Next()
				return
			}

			// 其他错误（如 Redis 连接失败）
			// 降级策略：记录日志并继续执行（避免幂等组件故障影响业务）
			c.Next()
			return
		}

		// 如果是从缓存返回的结果，需要检查并返回
		// 但由于 Do 方法已经处理了缓存，这里不需要额外处理
		// c.Next() 已经在 Do 的 fn 中调用过了
	}
}

// GinMiddlewareWithStatus 创建带状态码过滤的 Gin 幂等中间件
// 只有当响应状态码在 successCodes 范围内时才缓存结果
//
// 参数:
//   - idem: 幂等组件实例
//   - keyFunc: 从请求中提取幂等键的函数
//   - successCodes: 成功状态码列表，只有这些状态码才会缓存结果
//   - opts: 幂等选项 (如 TTL)
//
// 使用示例:
//
//	r.Use(adapter.GinMiddlewareWithStatus(idem, nil, []int{200, 201}, idempotency.WithTTL(1*time.Hour)))
func GinMiddlewareWithStatus(
	idem types.Idempotent,
	keyFunc func(*gin.Context) string,
	successCodes []int,
	opts ...types.DoOption,
) gin.HandlerFunc {
	if keyFunc == nil {
		keyFunc = func(c *gin.Context) string {
			return c.GetHeader(IdempotencyKeyHeader)
		}
	}

	// 创建状态码映射以快速查找
	successMap := make(map[int]bool)
	for _, code := range successCodes {
		successMap[code] = true
	}

	return func(c *gin.Context) {
		key := keyFunc(c)
		if key == "" {
			c.Next()
			return
		}

		writer := &responseWriter{
			ResponseWriter: c.Writer,
			body:           bytes.NewBuffer(nil),
			statusCode:     http.StatusOK,
		}
		c.Writer = writer

		_, err := idem.Do(c.Request.Context(), key, func() (any, error) {
			c.Next()

			// 检查状态码是否在成功范围内
			if !successMap[writer.statusCode] {
				// 状态码不在成功范围内，返回错误以避免缓存
				return nil, http.ErrAbortHandler
			}

			if len(c.Errors) > 0 {
				return nil, c.Errors[0]
			}

			return map[string]any{
				"status_code": writer.statusCode,
				"body":        writer.body.Bytes(),
				"headers":     c.Writer.Header(),
			}, nil
		}, opts...)

		if err != nil {
			if err == types.ErrProcessing {
				c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
					"error": "request is being processed",
				})
				return
			}

			if err == types.ErrKeyEmpty {
				c.Next()
				return
			}

			// 降级：继续执行
			c.Next()
			return
		}
	}
}
