package idem

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ceyewan/genesis/testkit"
)

func TestGinMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	redisConn := testkit.NewRedisContainerConnector(t)

	prefix := "test:idem:middleware:" + testkit.NewID() + ":"
	idemComp, err := New(&Config{
		Driver:     DriverRedis,
		Prefix:     prefix,
		DefaultTTL: 1 * time.Hour,
		LockTTL:    5 * time.Second,
	}, WithRedisConnector(redisConn))
	if err != nil {
		t.Fatalf("failed to create idem: %v", err)
	}

	r := gin.New()
	// 注意：这里需要类型断言，因为 GinMiddleware 返回的是 interface{}
	// 且返回的是匿名函数 func(*gin.Context)，需要转换为 gin.HandlerFunc
	r.Use(gin.HandlerFunc(idemComp.GinMiddleware().(func(*gin.Context))))

	var handlerExecCount int32
	r.POST("/test", func(c *gin.Context) {
		atomic.AddInt32(&handlerExecCount, 1)
		c.Header("X-Custom-Header", "foo")
		c.JSON(200, gin.H{"status": "ok", "count": handlerExecCount})
	})

	r.POST("/error", func(c *gin.Context) {
		atomic.AddInt32(&handlerExecCount, 1)
		c.JSON(500, gin.H{"error": "internal error"})
	})

	// 1. 测试正常请求
	t.Run("Normal Request", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/test", nil)
		req.Header.Set("X-Idempotency-Key", "req1")

		r.ServeHTTP(w, req)

		if w.Code != 200 {
			t.Errorf("expected status 200, got %d", w.Code)
		}
		if atomic.LoadInt32(&handlerExecCount) != 1 {
			t.Errorf("expected exec count 1, got %d", handlerExecCount)
		}
		if w.Header().Get("X-Custom-Header") != "foo" {
			t.Errorf("expected custom header foo, got %s", w.Header().Get("X-Custom-Header"))
		}
	})

	// 2. 测试重复请求（缓存命中）
	t.Run("Duplicate Request", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/test", nil)
		req.Header.Set("X-Idempotency-Key", "req1") // 相同的 Key

		r.ServeHTTP(w, req)

		if w.Code != 200 {
			t.Errorf("expected status 200, got %d", w.Code)
		}
		// handlerExecCount 应该仍然是 1，没有增加
		if atomic.LoadInt32(&handlerExecCount) != 1 {
			t.Errorf("expected exec count 1, got %d", handlerExecCount)
		}
		// Header 也应该被缓存
		if w.Header().Get("X-Custom-Header") != "foo" {
			t.Errorf("expected custom header foo, got %s", w.Header().Get("X-Custom-Header"))
		}
	})

	// 3. 测试无 Key 请求（不幂等）
	t.Run("No Key Request", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/test", nil)
		// 不设置 X-Idempotency-Key

		r.ServeHTTP(w, req)

		if w.Code != 200 {
			t.Errorf("expected status 200, got %d", w.Code)
		}
		// handlerExecCount 应该增加到 2
		if atomic.LoadInt32(&handlerExecCount) != 2 {
			t.Errorf("expected exec count 2, got %d", handlerExecCount)
		}
	})

	// 4. 测试失败请求（不缓存）
	t.Run("Error Request", func(t *testing.T) {
		key := "req-error"

		// 第一次请求，返回 500
		w1 := httptest.NewRecorder()
		req1, _ := http.NewRequest("POST", "/error", nil)
		req1.Header.Set("X-Idempotency-Key", key)
		r.ServeHTTP(w1, req1)

		if w1.Code != 500 {
			t.Errorf("expected status 500, got %d", w1.Code)
		}

		currentCount := atomic.LoadInt32(&handlerExecCount)

		// 第二次请求，应该再次执行 Handler (因为 500 不缓存)
		w2 := httptest.NewRecorder()
		req2, _ := http.NewRequest("POST", "/error", nil)
		req2.Header.Set("X-Idempotency-Key", key)
		r.ServeHTTP(w2, req2)

		if w2.Code != 500 {
			t.Errorf("expected status 500, got %d", w2.Code)
		}

		if atomic.LoadInt32(&handlerExecCount) != currentCount+1 {
			t.Errorf("expected exec count to increment, but it didn't")
		}
	})
}
