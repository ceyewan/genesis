package idem

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
		shouldCache: func(status int) bool {
			return status >= 200 && status < 300
		},
	}
	for _, o := range opts {
		o(&opt)
	}

	return func(c *gin.Context) {
		// 从请求头获取幂等键
		rawKey := c.GetHeader(opt.headerKey)
		if rawKey == "" {
			// 没有幂等键，直接放行
			c.Next()
			return
		}
		scope := c.FullPath()
		if scope == "" {
			scope = c.Request.URL.Path
		}
		key := scopedIdempotencyKey("http", c.Request.Method+" "+scope, rawKey)
		fingerprint, err := fingerprintHTTPRequest(c.Request)
		if err != nil {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}

		decode := func(cached []byte, logger clog.Logger, key string) (any, error) {
			payload, err := decodeIdemEnvelope(cached, fingerprint)
			if err != nil {
				return nil, err
			}
			return decodeCachedHTTPResponse(payload, logger, key)
		}
		cachedResp, token, locked, err := i.loadResultOrAcquireLock(c.Request.Context(), key, decode)
		if err != nil {
			if i.logger != nil {
				i.logger.Error("failed to wait for HTTP idem result", clog.Error(err), clog.String("key", key))
			}
			if errors.Is(err, ErrKeyConflict) {
				c.AbortWithStatus(http.StatusConflict)
			} else {
				c.AbortWithStatus(http.StatusInternalServerError)
			}
			return
		}
		if !locked {
			if i.logger != nil {
				i.logger.Debug("idem cache hit for HTTP request", clog.String("key", key))
			}
			writeCachedHTTPResponse(c, cachedResp.(cachedHTTPResponse))
			c.Abort()
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
		execCtx, cancel := context.WithCancel(c.Request.Context())
		defer cancel()
		c.Request = c.Request.WithContext(execCtx)

		stopRefresh, refreshErrCh := i.startLockRefresh(key, token, cancel)
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
		if refreshErr := collectRefreshError(refreshErrCh); refreshErr != nil {
			if i.logger != nil {
				i.logger.Error("lock refresh failed during HTTP execution", clog.Error(refreshErr), clog.String("key", key))
			}
			return
		}

		if opt.shouldCache(c.Writer.Status()) {
			resp := cachedHTTPResponse{
				Status: c.Writer.Status(),
				Header: cloneHeader(c.Writer.Header()),
				Body:   append([]byte(nil), writer.body.Bytes()...),
			}
			resp.Header.Del("Content-Length")

			if respBytes, err := json.Marshal(resp); err == nil {
				envelope, envelopeErr := encodeIdemEnvelope(fingerprint, respBytes)
				if envelopeErr != nil {
					return
				}
				if err := i.store.SetResult(c.Request.Context(), key, envelope, i.cfg.DefaultTTL, token); err != nil {
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

type idemEnvelope struct {
	Fingerprint string `json:"fingerprint"`
	Payload     []byte `json:"payload"`
}

func encodeIdemEnvelope(fingerprint string, payload []byte) ([]byte, error) {
	return json.Marshal(idemEnvelope{Fingerprint: fingerprint, Payload: payload})
}

func decodeIdemEnvelope(data []byte, fingerprint string) ([]byte, error) {
	var envelope idemEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	if envelope.Fingerprint != fingerprint {
		return nil, ErrKeyConflict
	}
	return envelope.Payload, nil
}

func scopedIdempotencyKey(kind, endpoint, key string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(kind+"\x00"+endpoint+"\x00"+key)))
}

func fingerprintHTTPRequest(req *http.Request) (string, error) {
	var body []byte
	if req.Body != nil {
		var err error
		body, err = io.ReadAll(req.Body)
		if err != nil {
			return "", err
		}
		if err := req.Body.Close(); err != nil {
			return "", err
		}
		req.Body = io.NopCloser(bytes.NewReader(body))
	}
	hash := sha256.New()
	_, _ = io.WriteString(hash, req.Method+"\x00"+req.URL.EscapedPath()+"\x00"+req.URL.RawQuery+"\x00")
	_, _ = hash.Write(body)
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

type cachedHTTPResponse struct {
	Status int         `json:"status"`
	Header http.Header `json:"header"`
	Body   []byte      `json:"body"`
}

func decodeCachedHTTPResponse(cachedResp []byte, logger clog.Logger, key string) (any, error) {
	var resp cachedHTTPResponse
	if err := json.Unmarshal(cachedResp, &resp); err != nil {
		if logger != nil {
			logger.Error("failed to unmarshal cached HTTP response", clog.Error(err), clog.String("key", key))
		}
		return nil, err
	}
	return resp, nil
}

func writeCachedHTTPResponse(c *gin.Context, resp cachedHTTPResponse) {
	for name, values := range resp.Header {
		for _, v := range values {
			c.Writer.Header().Add(name, v)
		}
	}
	c.Status(resp.Status)
	_, _ = c.Writer.Write(resp.Body)
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
