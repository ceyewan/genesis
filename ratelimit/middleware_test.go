package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ceyewan/genesis/clog"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================
// 辅助函数
// ============================================================

func setupTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	return gin.New()
}

func newTestLimiter(t *testing.T) Limiter {
	t.Helper()

	logger, _ := clog.New(&clog.Config{Level: "error"})
	limiter, err := New(&Config{Driver: DriverStandalone, Standalone: &StandaloneConfig{}}, WithLogger(logger))
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = limiter.Close()
	})

	return limiter
}

// ============================================================
// GinMiddleware 基础测试
// ============================================================

func TestGinMiddleware_Basic(t *testing.T) {
	t.Run("正常请求应该通过", func(t *testing.T) {
		limiter := newTestLimiter(t)
		router := setupTestRouter()

		router.Use(GinMiddleware(limiter, &GinMiddlewareOptions{
			KeyFunc: func(c *gin.Context) string {
				return "test-client"
			},
			LimitFunc: func(c *gin.Context) Limit {
				return Limit{Rate: 10, Burst: 10}
			},
		}))

		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "ok", w.Body.String())
	})

	t.Run("被限流的请求应该返回 429", func(t *testing.T) {
		limiter := newTestLimiter(t)
		router := setupTestRouter()

		router.Use(GinMiddleware(limiter, &GinMiddlewareOptions{
			KeyFunc: func(c *gin.Context) string {
				return "limited-client"
			},
			LimitFunc: func(c *gin.Context) Limit {
				return Limit{Rate: 1, Burst: 1}
			},
		}))

		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		// 第一次请求成功
		req1 := httptest.NewRequest("GET", "/test", nil)
		w1 := httptest.NewRecorder()
		router.ServeHTTP(w1, req1)
		assert.Equal(t, http.StatusOK, w1.Code)

		// 第二次请求应该被限流
		req2 := httptest.NewRequest("GET", "/test", nil)
		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, req2)
		assert.Equal(t, http.StatusTooManyRequests, w2.Code)
		assert.Contains(t, w2.Body.String(), "rate limit exceeded")
	})

	t.Run("不同客户端应该独立限流", func(t *testing.T) {
		limiter := newTestLimiter(t)
		router := setupTestRouter()

		router.Use(GinMiddleware(limiter, &GinMiddlewareOptions{
			KeyFunc: func(c *gin.Context) string {
				return c.ClientIP()
			},
			LimitFunc: func(c *gin.Context) Limit {
				return Limit{Rate: 1, Burst: 1}
			},
		}))

		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		// 客户端 1
		req1 := httptest.NewRequest("GET", "/test", nil)
		req1.RemoteAddr = "192.168.1.1:1234"
		w1 := httptest.NewRecorder()
		router.ServeHTTP(w1, req1)
		assert.Equal(t, http.StatusOK, w1.Code)

		// 客户端 2（不同 IP，应该独立限流）
		req2 := httptest.NewRequest("GET", "/test", nil)
		req2.RemoteAddr = "192.168.1.2:5678"
		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, req2)
		assert.Equal(t, http.StatusOK, w2.Code, "不同 IP 应该独立限流")
	})
}

// ============================================================
// GinMiddleware 边界条件测试
// ============================================================

func TestGinMiddleware_EdgeCases(t *testing.T) {
	t.Run("nil limiter 应该使用 Discard", func(t *testing.T) {
		router := setupTestRouter()

		router.Use(GinMiddleware(nil, &GinMiddlewareOptions{
			KeyFunc: func(c *gin.Context) string {
				return "test"
			},
			LimitFunc: func(c *gin.Context) Limit {
				return Limit{Rate: 1, Burst: 1}
			},
		}))

		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		// 所有请求都应该成功
		for i := 0; i < 10; i++ {
			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
		}
	})

	t.Run("nil options 应该使用默认值", func(t *testing.T) {
		limiter := newTestLimiter(t)
		router := setupTestRouter()

		// 不传 options
		router.Use(GinMiddleware(limiter, nil))

		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// 应该使用默认行为（ClientIP 作为 key，无效限流规则放行）
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("空 key 应该放行", func(t *testing.T) {
		limiter := newTestLimiter(t)
		router := setupTestRouter()

		router.Use(GinMiddleware(limiter, &GinMiddlewareOptions{
			KeyFunc: func(c *gin.Context) string {
				return "" // 空 key
			},
			LimitFunc: func(c *gin.Context) Limit {
				return Limit{Rate: 1, Burst: 1}
			},
		}))

		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "空 key 应该放行")
	})

	t.Run("无效限流规则应该放行", func(t *testing.T) {
		limiter := newTestLimiter(t)
		router := setupTestRouter()

		router.Use(GinMiddleware(limiter, &GinMiddlewareOptions{
			KeyFunc: func(c *gin.Context) string {
				return "test"
			},
			LimitFunc: func(c *gin.Context) Limit {
				return Limit{Rate: 0, Burst: 0} // 无效
			},
		}))

		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		// 所有请求都应该成功
		for i := 0; i < 10; i++ {
			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
		}
	})

	t.Run("nil KeyFunc 应该使用默认 ClientIP", func(t *testing.T) {
		limiter := newTestLimiter(t)
		router := setupTestRouter()

		router.Use(GinMiddleware(limiter, &GinMiddlewareOptions{
			LimitFunc: func(c *gin.Context) Limit {
				return Limit{Rate: 10, Burst: 10}
			},
		}))

		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("nil LimitFunc 应该返回无效限流规则（放行）", func(t *testing.T) {
		limiter := newTestLimiter(t)
		router := setupTestRouter()

		router.Use(GinMiddleware(limiter, &GinMiddlewareOptions{
			KeyFunc: func(c *gin.Context) string {
				return "test"
			},
			// LimitFunc 为 nil
		}))

		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

// ============================================================
// GinMiddleware WithHeaders 测试
// ============================================================

func TestGinMiddleware_WithHeaders(t *testing.T) {
	t.Run("启用响应头", func(t *testing.T) {
		limiter := newTestLimiter(t)
		router := setupTestRouter()

		router.Use(GinMiddleware(limiter, &GinMiddlewareOptions{
			WithHeaders: true,
			KeyFunc: func(c *gin.Context) string {
				return "test-client"
			},
			LimitFunc: func(c *gin.Context) Limit {
				return Limit{Rate: 10, Burst: 20}
			},
		}))

		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Header().Get("X-RateLimit-Limit"), "rate=10.00")
		assert.Contains(t, w.Header().Get("X-RateLimit-Limit"), "burst=20")
	})

	t.Run("被限流时设置剩余数为 0", func(t *testing.T) {
		limiter := newTestLimiter(t)
		router := setupTestRouter()

		router.Use(GinMiddleware(limiter, &GinMiddlewareOptions{
			WithHeaders: true,
			KeyFunc: func(c *gin.Context) string {
				return "limited-client"
			},
			LimitFunc: func(c *gin.Context) Limit {
				return Limit{Rate: 1, Burst: 1}
			},
		}))

		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		// 第一次请求
		req1 := httptest.NewRequest("GET", "/test", nil)
		w1 := httptest.NewRecorder()
		router.ServeHTTP(w1, req1)
		assert.Equal(t, http.StatusOK, w1.Code)

		// 第二次请求被限流
		req2 := httptest.NewRequest("GET", "/test", nil)
		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, req2)
		assert.Equal(t, http.StatusTooManyRequests, w2.Code)
		assert.Equal(t, "0", w2.Header().Get("X-RateLimit-Remaining"))
	})

	t.Run("不启用响应头时不设置头", func(t *testing.T) {
		limiter := newTestLimiter(t)
		router := setupTestRouter()

		router.Use(GinMiddleware(limiter, &GinMiddlewareOptions{
			WithHeaders: false,
			KeyFunc: func(c *gin.Context) string {
				return "test-client"
			},
			LimitFunc: func(c *gin.Context) Limit {
				return Limit{Rate: 10, Burst: 20}
			},
		}))

		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Empty(t, w.Header().Get("X-RateLimit-Limit"))
	})
}

// ============================================================
// GinMiddleware 自定义 KeyFunc 测试
// ============================================================

func TestGinMiddleware_CustomKeyFunc(t *testing.T) {
	t.Run("使用 Header 中的 User ID 作为 key", func(t *testing.T) {
		limiter := newTestLimiter(t)
		router := setupTestRouter()

		router.Use(GinMiddleware(limiter, &GinMiddlewareOptions{
			KeyFunc: func(c *gin.Context) string {
				userID := c.GetHeader("X-User-ID")
				if userID == "" {
					return "anonymous"
				}
				return "user:" + userID
			},
			LimitFunc: func(c *gin.Context) Limit {
				return Limit{Rate: 1, Burst: 1}
			},
		}))

		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		// 用户 1 的第一次请求
		req1 := httptest.NewRequest("GET", "/test", nil)
		req1.Header.Set("X-User-ID", "user1")
		w1 := httptest.NewRecorder()
		router.ServeHTTP(w1, req1)
		assert.Equal(t, http.StatusOK, w1.Code)

		// 用户 1 的第二次请求应该被限流
		req2 := httptest.NewRequest("GET", "/test", nil)
		req2.Header.Set("X-User-ID", "user1")
		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, req2)
		assert.Equal(t, http.StatusTooManyRequests, w2.Code)

		// 用户 2 的第一次请求应该成功
		req3 := httptest.NewRequest("GET", "/test", nil)
		req3.Header.Set("X-User-ID", "user2")
		w3 := httptest.NewRecorder()
		router.ServeHTTP(w3, req3)
		assert.Equal(t, http.StatusOK, w3.Code)
	})

	t.Run("使用路径作为 key", func(t *testing.T) {
		limiter := newTestLimiter(t)
		router := setupTestRouter()

		router.Use(GinMiddleware(limiter, &GinMiddlewareOptions{
			KeyFunc: func(c *gin.Context) string {
				return "path:" + c.Request.URL.Path
			},
			LimitFunc: func(c *gin.Context) Limit {
				return Limit{Rate: 1, Burst: 1}
			},
		}))

		router.GET("/api1", func(c *gin.Context) {
			c.String(http.StatusOK, "api1")
		})
		router.GET("/api2", func(c *gin.Context) {
			c.String(http.StatusOK, "api2")
		})

		// /api1 的第一次请求
		req1 := httptest.NewRequest("GET", "/api1", nil)
		w1 := httptest.NewRecorder()
		router.ServeHTTP(w1, req1)
		assert.Equal(t, http.StatusOK, w1.Code)

		// /api1 的第二次请求被限流
		req2 := httptest.NewRequest("GET", "/api1", nil)
		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, req2)
		assert.Equal(t, http.StatusTooManyRequests, w2.Code)

		// /api2 的第一次请求应该成功
		req3 := httptest.NewRequest("GET", "/api2", nil)
		w3 := httptest.NewRecorder()
		router.ServeHTTP(w3, req3)
		assert.Equal(t, http.StatusOK, w3.Code)
	})
}

// ============================================================
// GinMiddleware 自定义 LimitFunc 测试
// ============================================================

func TestGinMiddleware_CustomLimitFunc(t *testing.T) {
	t.Run("根据路径返回不同限流规则", func(t *testing.T) {
		limiter := newTestLimiter(t)
		router := setupTestRouter()

		router.Use(GinMiddleware(limiter, &GinMiddlewareOptions{
			KeyFunc: func(c *gin.Context) string {
				return "client"
			},
			LimitFunc: func(c *gin.Context) Limit {
				switch c.Request.URL.Path {
				case "/api/public":
					return Limit{Rate: 100, Burst: 100} // 公开 API 限制宽松
				case "/api/admin":
					return Limit{Rate: 1, Burst: 1} // 管理 API 限制严格
				default:
					return Limit{Rate: 50, Burst: 50}
				}
			},
		}))

		router.GET("/api/public", func(c *gin.Context) {
			c.String(http.StatusOK, "public")
		})
		router.GET("/api/admin", func(c *gin.Context) {
			c.String(http.StatusOK, "admin")
		})

		// 公开 API 可以发送更多请求
		for i := 0; i < 50; i++ {
			req := httptest.NewRequest("GET", "/api/public", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
		}

		// 管理 API 应该更容易被限流 (Rate=1, Burst=1)
		req1 := httptest.NewRequest("GET", "/api/admin", nil)
		w1 := httptest.NewRecorder()
		router.ServeHTTP(w1, req1)
		assert.Equal(t, http.StatusOK, w1.Code)

		req2 := httptest.NewRequest("GET", "/api/admin", nil)
		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, req2)
		assert.Equal(t, http.StatusTooManyRequests, w2.Code)
	})

	t.Run("根据 Header 返回不同限流规则", func(t *testing.T) {
		limiter := newTestLimiter(t)
		router := setupTestRouter()

		router.Use(GinMiddleware(limiter, &GinMiddlewareOptions{
			KeyFunc: func(c *gin.Context) string {
				return "client"
			},
			LimitFunc: func(c *gin.Context) Limit {
				tier := c.GetHeader("X-Tier")
				switch tier {
				case "premium":
					return Limit{Rate: 1000, Burst: 1000}
				case "free":
					return Limit{Rate: 1, Burst: 1} // Free 用户严格限流
				default:
					return Limit{Rate: 100, Burst: 100}
				}
			},
		}))

		router.GET("/api", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		// Free 用户很快被限流
		req1 := httptest.NewRequest("GET", "/api", nil)
		req1.Header.Set("X-Tier", "free")
		w1 := httptest.NewRecorder()
		router.ServeHTTP(w1, req1)
		assert.Equal(t, http.StatusOK, w1.Code)

		req2 := httptest.NewRequest("GET", "/api", nil)
		req2.Header.Set("X-Tier", "free")
		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, req2)
		assert.Equal(t, http.StatusTooManyRequests, w2.Code)

		// Premium 用户不容易被限流
		for i := 0; i < 100; i++ {
			req := httptest.NewRequest("GET", "/api", nil)
			req.Header.Set("X-Tier", "premium")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
		}
	})
}

// ============================================================
// formatLimit 测试
// ============================================================

func TestFormatLimit(t *testing.T) {
	tests := []struct {
		name  string
		limit Limit
		want  string
	}{
		{
			name:  "整数 Rate",
			limit: Limit{Rate: 10, Burst: 20},
			want:  "rate=10.00, burst=20",
		},
		{
			name:  "浮点数 Rate",
			limit: Limit{Rate: 0.5, Burst: 5},
			want:  "rate=0.50, burst=5",
		},
		{
			name:  "大数值 Rate",
			limit: Limit{Rate: 1000.123, Burst: 5000},
			want:  "rate=1000.12, burst=5000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatLimit(tt.limit)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ============================================================
// GinMiddlewareOptions 结构体测试
// ============================================================

func TestGinMiddlewareOptions(t *testing.T) {
	t.Run("创建选项", func(t *testing.T) {
		opts := &GinMiddlewareOptions{
			WithHeaders: true,
			KeyFunc: func(c *gin.Context) string {
				return "test"
			},
			LimitFunc: func(c *gin.Context) Limit {
				return Limit{Rate: 10, Burst: 20}
			},
		}

		assert.True(t, opts.WithHeaders)
		assert.NotNil(t, opts.KeyFunc)
		assert.NotNil(t, opts.LimitFunc)
	})
}

// ============================================================
// 链式中间件测试
// ============================================================

func TestGinMiddleware_Chaining(t *testing.T) {
	t.Run("多个中间件协同工作", func(t *testing.T) {
		limiter1 := newTestLimiter(t)
		limiter2 := newTestLimiter(t)
		router := setupTestRouter()

		// 第一个限流器：按 IP 限流
		router.Use(GinMiddleware(limiter1, &GinMiddlewareOptions{
			KeyFunc: func(c *gin.Context) string {
				return "ip:" + c.ClientIP()
			},
			LimitFunc: func(c *gin.Context) Limit {
				return Limit{Rate: 100, Burst: 100}
			},
		}))

		// 第二个限流器：按路径限流
		router.Use(GinMiddleware(limiter2, &GinMiddlewareOptions{
			KeyFunc: func(c *gin.Context) string {
				return "path:" + c.Request.URL.Path
			},
			LimitFunc: func(c *gin.Context) Limit {
				return Limit{Rate: 1, Burst: 1}
			},
		}))

		router.GET("/test", func(c *gin.Context) {
			c.String(http.StatusOK, "ok")
		})

		// 第一次请求成功
		req1 := httptest.NewRequest("GET", "/test", nil)
		w1 := httptest.NewRecorder()
		router.ServeHTTP(w1, req1)
		assert.Equal(t, http.StatusOK, w1.Code)

		// 第二次请求应该被第二个限流器限制
		req2 := httptest.NewRequest("GET", "/test", nil)
		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, req2)
		assert.Equal(t, http.StatusTooManyRequests, w2.Code)
	})
}
