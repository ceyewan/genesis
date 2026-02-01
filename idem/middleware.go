package idem

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
func (i *idem) GinMiddleware(opts ...MiddlewareOption) any {
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

		cachedResp, token, locked, err := i.waitForResultOrLock(c.Request.Context(), key)
		if err != nil {
			if i.logger != nil {
				i.logger.Error("failed to wait for HTTP idem result", clog.Error(err), clog.String("key", key))
			}
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		if !locked {
			if i.logger != nil {
				i.logger.Debug("idem cache hit for HTTP request", clog.String("key", key))
			}
			if ok := writeCachedHTTPResponse(c, cachedResp, i.logger, key); ok {
				c.Abort()
				return
			}
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}

		lockReleased := false
		defer func() {
			if lockReleased {
				return
			}
			if err := i.store.Unlock(c.Request.Context(), key, token); err != nil && i.logger != nil {
				i.logger.Error("failed to unlock after HTTP execution failure", clog.Error(err), clog.String("key", key))
			}
		}()
		stopRefresh := i.startLockRefresh(key, token)
		defer stopRefresh()

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
			resp := cachedHTTPResponse{
				Status: c.Writer.Status(),
				Header: cloneHeader(c.Writer.Header()),
				Body:   append([]byte(nil), writer.body.Bytes()...),
			}
			resp.Header.Del("Content-Length")

			if respBytes, err := json.Marshal(resp); err == nil {
				if err := i.store.SetResult(c.Request.Context(), key, respBytes, i.cfg.DefaultTTL, token); err != nil {
					if i.logger != nil {
						i.logger.Error("failed to cache HTTP response", clog.Error(err), clog.String("key", key))
					}
				} else {
					lockReleased = true
				}
			}
		}
	}
}

type cachedHTTPResponse struct {
	Status int         `json:"status"`
	Header http.Header `json:"header"`
	Body   []byte      `json:"body"`
}

func writeCachedHTTPResponse(c *gin.Context, cachedResp []byte, logger clog.Logger, key string) bool {
	var resp cachedHTTPResponse
	if err := json.Unmarshal(cachedResp, &resp); err != nil {
		if logger != nil {
			logger.Error("failed to unmarshal cached HTTP response", clog.Error(err), clog.String("key", key))
		}
		return false
	}
	for name, values := range resp.Header {
		for _, v := range values {
			c.Writer.Header().Add(name, v)
		}
	}
	c.Status(resp.Status)
	_, _ = c.Writer.Write(resp.Body)
	return true
}

func cloneHeader(header http.Header) http.Header {
	dup := make(http.Header, len(header))
	for k, v := range header {
		dup[k] = append([]string(nil), v...)
	}
	return dup
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
