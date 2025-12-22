package idempotency

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ceyewan/genesis/clog"
)

// GinMiddleware 创建 Gin 幂等性中间件
//
// 参数:
//   - opts: 中间件选项（如自定义 HTTP 头名称）
//
// 使用示例:
//
//	r := gin.Default()
//	r.POST("/orders", idem.GinMiddleware(), func(c *gin.Context) {
//	    c.JSON(200, gin.H{"order_id": "123"})
//	})
func (i *idempotency) GinMiddleware(opts ...MiddlewareOption) any {
	// 应用选项
	opt := middlewareOptions{
		headerKey: "X-Idempotency-Key",
	}
	for _, o := range opts {
		o(&opt)
	}

	return func(c *gin.Context) {
		// 从请求头获取幂等键
		key := c.GetHeader(opt.headerKey)
		if key == "" {
			// 没有幂等键，直接放行
			c.Next()
			return
		}

		// 尝试获取缓存的响应
		cachedResp, err := i.store.GetResult(c.Request.Context(), key)
		if err == nil {
			// 缓存命中，返回缓存的响应
			if i.logger != nil {
				i.logger.Debug("idempotency cache hit for HTTP request", clog.String("key", key))
			}

			// 解析缓存的响应
			var resp map[string]any
			if err := json.Unmarshal(cachedResp, &resp); err == nil {
				if statusCode, ok := resp["status"].(float64); ok {
					if body, ok := resp["body"].(string); ok {
						c.Data(int(statusCode), "application/json", []byte(body))
						return
					}
				}
			}
		}

		// 缓存未命中或解析失败，继续处理请求
		// 使用 ResponseWriter 包装器捕获响应
		writer := &responseWriter{
			ResponseWriter: c.Writer,
			body:           bytes.NewBuffer(nil),
		}
		c.Writer = writer

		// 继续处理请求
		c.Next()

		// 如果请求成功，缓存响应
		if c.Writer.Status() >= 200 && c.Writer.Status() < 300 {
			// 构建响应对象
			resp := map[string]any{
				"status": c.Writer.Status(),
				"body":   writer.body.String(),
			}

			// 序列化并缓存
			if respBytes, err := json.Marshal(resp); err == nil {
				if err := i.store.SetResult(c.Request.Context(), key, respBytes, i.cfg.DefaultTTL); err != nil {
					if i.logger != nil {
						i.logger.Error("failed to cache HTTP response", clog.Error(err), clog.String("key", key))
					}
				}
			}
		}
	}
}

// responseWriter 响应写入器包装器，用于捕获响应体
type responseWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

// Write 写入响应体
func (w *responseWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

// WriteString 写入字符串响应体
func (w *responseWriter) WriteString(s string) (int, error) {
	w.body.WriteString(s)
	return w.ResponseWriter.WriteString(s)
}

// WriteHeader 写入响应头
func (w *responseWriter) WriteHeader(statusCode int) {
	w.ResponseWriter.WriteHeader(statusCode)
}

// Flush 刷新响应
func (w *responseWriter) Flush() {
	w.ResponseWriter.Flush()
}

// CloseNotify 返回关闭通知通道
func (w *responseWriter) CloseNotify() <-chan bool {
	return w.ResponseWriter.CloseNotify()
}

// Hijack 劫持连接
func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.ResponseWriter.Hijack()
}

// Pusher 返回推送器
func (w *responseWriter) Pusher() http.Pusher {
	return w.ResponseWriter.Pusher()
}
