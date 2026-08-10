package idem

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

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
func (i *idem) GinMiddleware(opts ...MiddlewareOption) gin.HandlerFunc {
	// 应用选项
	opt := middlewareOptions{
		headerKey:       "X-Idempotency-Key",
		maxRequestBytes: defaultHTTPMaxBodyBytes,
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
		if err := i.validateRawKey(rawKey); err != nil {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}
		identityScope, err := resolveHTTPIdentityScope(c, opt.identityScopeFunc)
		if err != nil {
			if i.logger != nil {
				i.logger.Warn("failed to resolve HTTP idem identity scope", clog.Error(err))
			}
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		scope := c.FullPath()
		if scope == "" {
			scope = c.Request.URL.Path
		}
		keyMaterial := bindIdempotencyIdentity("http-key", rawKey, identityScope)
		key := scopedIdempotencyKey("http", c.Request.Method+" "+scope, keyMaterial)
		fingerprint, err := fingerprintHTTPRequest(c.Request, opt.maxRequestBytes)
		if err != nil {
			if errors.Is(err, errHTTPRequestTooLarge) {
				c.AbortWithStatus(http.StatusRequestEntityTooLarge)
			} else {
				c.AbortWithStatus(http.StatusBadRequest)
			}
			return
		}
		fingerprint = bindIdempotencyIdentity("http-fingerprint", fingerprint, identityScope)

		decode := func(cached []byte, logger clog.Logger, key string) (any, error) {
			payload, err := decodeIdemEnvelope(cached, fingerprint)
			if err != nil {
				return nil, err
			}
			return decodeCachedHTTPResponse(payload, logger, key)
		}
		requestCtx := c.Request.Context()
		cachedResp, token, locked, err := i.loadResultOrAcquireLock(requestCtx, key, decode)
		if err != nil {
			if i.logger != nil {
				i.logger.Error("failed to wait for HTTP idem result", clog.Error(err), clog.String("key", key))
			}
			if errors.Is(err, ErrKeyConflict) {
				c.AbortWithStatus(http.StatusConflict)
			} else if errors.Is(err, ErrStoreCapacity) {
				c.AbortWithStatus(http.StatusServiceUnavailable)
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
			if err := i.unlockForCleanup(requestCtx, key, token); err != nil && i.logger != nil {
				i.logger.Error("failed to unlock after HTTP execution failure", clog.Error(err), clog.String("key", key))
			}
		}()
		execCtx, cancel := context.WithCancel(requestCtx)
		defer cancel()
		c.Request = c.Request.WithContext(execCtx)

		stopRefresh, refreshErrCh := i.startLockRefresh(key, token, cancel)
		defer stopRefresh()

		// 使用 ResponseWriter 包装器捕获响应
		writer := &responseWriter{
			ResponseWriter: c.Writer,
			body:           newLimitedCapture(i.maxResultBytes),
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
			if writer.body.Exceeded() {
				if i.logger != nil {
					i.logger.Warn("skip caching oversized HTTP response",
						clog.String("key", key),
						clog.Int("max_result_bytes", i.maxResultBytes))
				}
				return
			}
			resp := cachedHTTPResponse{
				Status: c.Writer.Status(),
				Header: cloneHeader(c.Writer.Header()),
				Body:   writer.body.Bytes(),
			}
			resp.Header.Del("Content-Length")

			if respBytes, err := json.Marshal(resp); err == nil {
				envelope, envelopeErr := encodeIdemEnvelope(fingerprint, respBytes)
				if envelopeErr != nil {
					return
				}
				if len(envelope) > i.maxResultBytes {
					if i.logger != nil {
						i.logger.Warn("skip caching oversized encoded HTTP response",
							clog.String("key", key),
							clog.Int("result_bytes", len(envelope)),
							clog.Int("max_result_bytes", i.maxResultBytes))
					}
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

func resolveHTTPIdentityScope(c *gin.Context, fn HTTPIdentityScopeFunc) (string, error) {
	if fn == nil {
		return "", nil
	}
	scope, err := fn(c)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(scope) == "" {
		return "", errors.New("idem: HTTP identity scope is empty")
	}
	return scope, nil
}

func bindIdempotencyIdentity(kind, value, identityScope string) string {
	if identityScope == "" {
		return value
	}
	hash := sha256.New()
	var size [8]byte
	for _, part := range []string{kind, identityScope, value} {
		binary.BigEndian.PutUint64(size[:], uint64(len(part)))
		_, _ = hash.Write(size[:])
		_, _ = hash.Write([]byte(part))
	}
	return fmt.Sprintf("%x", hash.Sum(nil))
}

var errHTTPRequestTooLarge = errors.New("idem: HTTP request body exceeds the configured byte limit")

func fingerprintHTTPRequest(req *http.Request, maxBytes int64) (string, error) {
	var body []byte
	if req.Body != nil {
		if req.ContentLength > maxBytes {
			_ = req.Body.Close()
			return "", errHTTPRequestTooLarge
		}
		readLimit := max(maxBytes+1, maxBytes)
		var err error
		body, err = io.ReadAll(io.LimitReader(req.Body, readLimit))
		if err != nil {
			_ = req.Body.Close()
			return "", err
		}
		if err := req.Body.Close(); err != nil {
			return "", err
		}
		if int64(len(body)) > maxBytes {
			return "", errHTTPRequestTooLarge
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
	body *limitedCapture
}

// Write 写入响应体
func (w *responseWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.body.Write(b[:n])
	return n, err
}

// WriteString 写入字符串响应体
func (w *responseWriter) WriteString(s string) (int, error) {
	n, err := w.ResponseWriter.WriteString(s)
	w.body.WriteString(s[:n])
	return n, err
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

// limitedCapture grows lazily in bounded chunks. Response bytes continue to
// stream to the caller after the capture is full; Exceeded then disables
// caching. The sum of capture chunk capacities never exceeds limit.
type limitedCapture struct {
	chunks   [][]byte
	n        int
	limit    int
	exceeded bool
}

const (
	minCaptureChunkBytes = 1 << 10
	maxCaptureChunkBytes = 32 << 10
)

func newLimitedCapture(limit int) *limitedCapture {
	return &limitedCapture{limit: limit}
}

func (b *limitedCapture) Write(p []byte) {
	if len(p) == 0 {
		return
	}
	if b.n >= b.limit {
		b.exceeded = true
		return
	}
	written := b.writeBytes(p)
	if written < len(p) {
		b.exceeded = true
	}
}

func (b *limitedCapture) WriteString(s string) {
	if len(s) == 0 {
		return
	}
	if b.n >= b.limit {
		b.exceeded = true
		return
	}
	written := b.writeString(s)
	if written < len(s) {
		b.exceeded = true
	}
}

func (b *limitedCapture) writeBytes(p []byte) int {
	total := 0
	for len(p) > 0 && b.n < b.limit {
		chunk := b.writableChunk(len(p))
		n := copy(chunk[len(chunk):cap(chunk)], p)
		last := len(b.chunks) - 1
		b.chunks[last] = chunk[:len(chunk)+n]
		b.n += n
		total += n
		p = p[n:]
	}
	return total
}

func (b *limitedCapture) writeString(s string) int {
	total := 0
	for len(s) > 0 && b.n < b.limit {
		chunk := b.writableChunk(len(s))
		n := copy(chunk[len(chunk):cap(chunk)], s)
		last := len(b.chunks) - 1
		b.chunks[last] = chunk[:len(chunk)+n]
		b.n += n
		total += n
		s = s[n:]
	}
	return total
}

func (b *limitedCapture) writableChunk(nextWriteBytes int) []byte {
	if len(b.chunks) > 0 {
		last := b.chunks[len(b.chunks)-1]
		if len(last) < cap(last) {
			return last
		}
	}

	remaining := b.limit - b.n
	capacity := min(max(nextWriteBytes, minCaptureChunkBytes), maxCaptureChunkBytes, remaining)
	b.chunks = append(b.chunks, make([]byte, 0, capacity))
	return b.chunks[len(b.chunks)-1]
}

func (b *limitedCapture) Capacity() int {
	total := 0
	for _, chunk := range b.chunks {
		total += cap(chunk)
	}
	return total
}

func (b *limitedCapture) Bytes() []byte {
	if b.n == 0 {
		return nil
	}
	if len(b.chunks) == 1 {
		return b.chunks[0]
	}
	joined := make([]byte, 0, b.n)
	for _, chunk := range b.chunks {
		joined = append(joined, chunk...)
	}
	return joined
}

func (b *limitedCapture) Exceeded() bool {
	return b.exceeded
}
