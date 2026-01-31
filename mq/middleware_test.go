package mq

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/stretchr/testify/assert"
)

// ============================================================
// Chain 测试
// ============================================================

func TestChain(t *testing.T) {
	t.Run("空链返回原始 Handler", func(t *testing.T) {
		original := func(msg Message) error {
			return nil
		}

		chained := Chain()(original)
		err := chained(&mockMessage{})

		assert.NoError(t, err)
	})

	t.Run("单个中间件", func(t *testing.T) {
		called := false
		mw := func(next Handler) Handler {
			return func(msg Message) error {
				called = true
				return next(msg)
			}
		}

		original := func(msg Message) error {
			return nil
		}

		chained := Chain(mw)(original)
		err := chained(&mockMessage{})

		assert.NoError(t, err)
		assert.True(t, called, "中间件应该被调用")
	})

	t.Run("多个中间件按正确顺序执行", func(t *testing.T) {
		order := []string{}

		mw1 := func(next Handler) Handler {
			return func(msg Message) error {
				order = append(order, "mw1-before")
				err := next(msg)
				order = append(order, "mw1-after")
				return err
			}
		}

		mw2 := func(next Handler) Handler {
			return func(msg Message) error {
				order = append(order, "mw2-before")
				err := next(msg)
				order = append(order, "mw2-after")
				return err
			}
		}

		original := func(msg Message) error {
			order = append(order, "handler")
			return nil
		}

		// Chain(mw1, mw2) 的执行顺序是：mw1 -> mw2 -> handler
		chained := Chain(mw1, mw2)(original)
		_ = chained(&mockMessage{})

		// 预期：mw1-before, mw2-before, handler, mw2-after, mw1-after
		expected := []string{"mw1-before", "mw2-before", "handler", "mw2-after", "mw1-after"}
		assert.Equal(t, expected, order)
	})

	t.Run("中间件链中错误传递", func(t *testing.T) {
		expectedErr := errors.New("handler error")
		mw := func(next Handler) Handler {
			return func(msg Message) error {
				return next(msg)
			}
		}

		original := func(msg Message) error {
			return expectedErr
		}

		chained := Chain(mw)(original)
		err := chained(&mockMessage{})

		assert.Equal(t, expectedErr, err)
	})
}

// ============================================================
// WithRetry 测试
// ============================================================

func TestWithRetry(t *testing.T) {
	t.Run("Handler 成功时不重试", func(t *testing.T) {
		callCount := 0
		handler := func(msg Message) error {
			callCount++
			return nil
		}

		cfg := RetryConfig{
			MaxRetries:     3,
			InitialBackoff: 10 * time.Millisecond,
			MaxBackoff:     100 * time.Millisecond,
			Multiplier:     2.0,
		}

		retryHandler := WithRetry(cfg, clog.Discard())(handler)
		err := retryHandler(&mockMessage{})

		assert.NoError(t, err)
		assert.Equal(t, 1, callCount, "成功时不应该重试")
	})

	t.Run("Handler 失败时重试指定次数", func(t *testing.T) {
		callCount := 0
		handler := func(msg Message) error {
			callCount++
			return errors.New("handler failed")
		}

		cfg := RetryConfig{
			MaxRetries:     2,
			InitialBackoff: 5 * time.Millisecond,
			MaxBackoff:     50 * time.Millisecond,
			Multiplier:     2.0,
		}

		retryHandler := WithRetry(cfg, clog.Discard())(handler)
		err := retryHandler(&mockMessage{})

		assert.Error(t, err)
		assert.Equal(t, 3, callCount, "首次 + 2次重试 = 3次调用")
	})

	t.Run("重试后成功", func(t *testing.T) {
		callCount := 0
		handler := func(msg Message) error {
			callCount++
			if callCount < 3 {
				return errors.New("handler failed")
			}
			return nil
		}

		cfg := RetryConfig{
			MaxRetries:     3,
			InitialBackoff: 5 * time.Millisecond,
			MaxBackoff:     50 * time.Millisecond,
			Multiplier:     2.0,
		}

		retryHandler := WithRetry(cfg, clog.Discard())(handler)
		err := retryHandler(&mockMessage{})

		assert.NoError(t, err)
		assert.Equal(t, 3, callCount, "第3次尝试成功")
	})

	t.Run("Context 取消时停止重试", func(t *testing.T) {
		callCount := 0
		handler := func(msg Message) error {
			callCount++
			return errors.New("handler failed")
		}

		cfg := RetryConfig{
			MaxRetries:     10,
			InitialBackoff: 5 * time.Millisecond,
			MaxBackoff:     50 * time.Millisecond,
			Multiplier:     2.0,
		}

		ctx, cancel := context.WithCancel(context.Background())
		// 创建一个带取消功能的测试消息
		msg := &testMessageWithCancel{mockMessage: &mockMessage{}, ctx: ctx}

		// 启动一个 goroutine 在第一次重试前取消 context
		go func() {
			time.Sleep(2 * time.Millisecond)
			cancel()
		}()

		retryHandler := WithRetry(cfg, clog.Discard())(handler)
		err := retryHandler(msg)

		assert.Error(t, err)
		assert.Equal(t, context.Canceled, err)
		assert.LessOrEqual(t, callCount, 2, "Context 取消后应立即停止")
	})

	t.Run("Multiplier <= 1 时使用默认值 2.0", func(t *testing.T) {
		handler := func(msg Message) error {
			return errors.New("handler failed")
		}

		cfg := RetryConfig{
			MaxRetries:     1,
			InitialBackoff: 5 * time.Millisecond,
			MaxBackoff:     50 * time.Millisecond,
			Multiplier:     0.5, // 无效值
		}

		// 不应该 panic，应该使用默认值
		retryHandler := WithRetry(cfg, clog.Discard())(handler)
		err := retryHandler(&mockMessage{})

		assert.Error(t, err)
	})

	t.Run("MaxBackoff 限制退避时间", func(t *testing.T) {
		handler := func(msg Message) error {
			return errors.New("handler failed")
		}

		cfg := RetryConfig{
			MaxRetries:     5,
			InitialBackoff: 100 * time.Millisecond,
			MaxBackoff:     150 * time.Millisecond,
			Multiplier:     10.0, // 很大的倍数
		}

		start := time.Now()
		retryHandler := WithRetry(cfg, clog.Discard())(handler)
		_ = retryHandler(&mockMessage{})
		elapsed := time.Since(start)

		// 计算预期时间：100 + 150 + 150 + 150 + 150 + 150 = 850ms (受 MaxBackoff 限制)
		// 但实际时间受调度影响，只验证不超过某个上限
		assert.Less(t, elapsed, 2*time.Second, "受 MaxBackoff 限制，总时间不应过长")
	})
}

// ============================================================
// DefaultRetryConfig 测试
// ============================================================

func TestDefaultRetryConfig(t *testing.T) {
	assert.Equal(t, 3, DefaultRetryConfig.MaxRetries)
	assert.Equal(t, 100*time.Millisecond, DefaultRetryConfig.InitialBackoff)
	assert.Equal(t, 5*time.Second, DefaultRetryConfig.MaxBackoff)
	assert.Equal(t, 2.0, DefaultRetryConfig.Multiplier)
}

// ============================================================
// WithLogging 测试
// ============================================================

func TestWithLogging(t *testing.T) {
	t.Run("成功时记录 Debug 日志", func(t *testing.T) {
		// 使用内存 logger 来验证日志输出
		// 这里简化测试，只验证不会 panic
		handler := func(msg Message) error {
			return nil
		}

		loggingHandler := WithLogging(clog.Discard())(handler)
		err := loggingHandler(&mockMessage{})

		assert.NoError(t, err)
	})

	t.Run("失败时记录 Error 日志", func(t *testing.T) {
		handler := func(msg Message) error {
			return errors.New("handler failed")
		}

		loggingHandler := WithLogging(clog.Discard())(handler)
		err := loggingHandler(&mockMessage{})

		assert.Error(t, err)
	})

	t.Run("日志中间件不改变错误", func(t *testing.T) {
		expectedErr := errors.New("test error")
		handler := func(msg Message) error {
			return expectedErr
		}

		loggingHandler := WithLogging(clog.Discard())(handler)
		err := loggingHandler(&mockMessage{})

		assert.Equal(t, expectedErr, err)
	})
}

// ============================================================
// WithRecover 测试
// ============================================================

func TestWithRecover(t *testing.T) {
	t.Run("捕获 panic 并转换为错误", func(t *testing.T) {
		handler := func(msg Message) error {
			panic("something went wrong")
		}

		recoverHandler := WithRecover(clog.Discard())(handler)
		err := recoverHandler(&mockMessage{})

		assert.Error(t, err)
		assert.Equal(t, ErrPanicRecovered, err)
	})

	t.Run("正常情况不影响执行", func(t *testing.T) {
		handler := func(msg Message) error {
			return nil
		}

		recoverHandler := WithRecover(clog.Discard())(handler)
		err := recoverHandler(&mockMessage{})

		assert.NoError(t, err)
	})

	t.Run("正常错误不受影响", func(t *testing.T) {
		expectedErr := errors.New("normal error")
		handler := func(msg Message) error {
			return expectedErr
		}

		recoverHandler := WithRecover(clog.Discard())(handler)
		err := recoverHandler(&mockMessage{})

		assert.Equal(t, expectedErr, err)
	})

	t.Run("捕获不同类型的 panic", func(t *testing.T) {
		tests := []struct {
			name    string
			panicVal interface{}
		}{
			{"string panic", "panic string"},
			{"error panic", errors.New("panic error")},
			{"int panic", 42},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				handler := func(msg Message) error {
					panic(tt.panicVal)
				}

				recoverHandler := WithRecover(clog.Discard())(handler)
				err := recoverHandler(&mockMessage{})

				assert.Error(t, err)
				assert.Equal(t, ErrPanicRecovered, err)
			})
		}
	})
}

// ============================================================
// 组合测试
// ============================================================

func TestMiddlewareCombination(t *testing.T) {
	t.Run("Retry + Logging + Recover", func(t *testing.T) {
		callCount := 0
		handler := func(msg Message) error {
			callCount++
			if callCount == 1 {
				return errors.New("first attempt fails")
			}
			if callCount == 2 {
				return errors.New("second attempt fails")
			}
			return nil
		}

		cfg := RetryConfig{
			MaxRetries:     3,
			InitialBackoff: 5 * time.Millisecond,
			MaxBackoff:     50 * time.Millisecond,
			Multiplier:     2.0,
		}

		// Chain 顺序：Recover -> Logging -> Retry -> handler
		// 执行顺序：Recover -> Logging -> Retry -> handler
		chained := Chain(
			WithRecover(clog.Discard()),
			WithLogging(clog.Discard()),
			WithRetry(cfg, clog.Discard()),
		)(handler)

		err := chained(&mockMessage{})

		assert.NoError(t, err)
		assert.Equal(t, 3, callCount, "重试两次后成功")
	})

	t.Run("Recover 捕获 panic，不会触发 Retry 重试", func(t *testing.T) {
		// 执行顺序：WithRecover -> WithRetry -> handler
		// 当 handler panic 时，panic 会穿透 WithRetry 直接被 WithRecover 捕获
		// WithRecover 将 panic 转换为 ErrPanicRecovered 返回，但不会再次调用 handler
		callCount := 0
		handler := func(msg Message) error {
			callCount++
			panic("handler panics")
		}

		cfg := RetryConfig{
			MaxRetries:     2,
			InitialBackoff: 5 * time.Millisecond,
			MaxBackoff:     50 * time.Millisecond,
			Multiplier:     2.0,
		}

		chained := Chain(
			WithRecover(clog.Discard()),
			WithRetry(cfg, clog.Discard()),
		)(handler)

		err := chained(&mockMessage{})

		// panic 被恢复，返回 ErrPanicRecovered
		assert.Error(t, err)
		assert.Equal(t, ErrPanicRecovered, err)
		assert.Equal(t, 1, callCount, "panic 被恢复后不会重试")
	})

	t.Run("Retry 在外层时可以重试错误", func(t *testing.T) {
		// 执行顺序：WithRetry -> WithRecover -> handler
		// 当 handler panic 时，WithRecover 捕获并返回错误
		// WithRetry 收到错误后会重试
		callCount := 0
		handler := func(msg Message) error {
			callCount++
			if callCount < 3 {
				panic("handler panics")
			}
			return nil
		}

		cfg := RetryConfig{
			MaxRetries:     3,
			InitialBackoff: 5 * time.Millisecond,
			MaxBackoff:     50 * time.Millisecond,
			Multiplier:     2.0,
		}

		// Retry 在外层，可以重试 Recover 返回的错误
		chained := Chain(
			WithRetry(cfg, clog.Discard()),
			WithRecover(clog.Discard()),
		)(handler)

		err := chained(&mockMessage{})

		// 经过重试后成功
		assert.NoError(t, err)
		assert.Equal(t, 3, callCount, "前两次 panic 后恢复，第三次成功")
	})
}

// ============================================================
// 测试辅助类型
// ============================================================

// testMessageWithCancel 是一个支持 context 取消的测试消息
type testMessageWithCancel struct {
	*mockMessage
	ctx context.Context
}

func (m *testMessageWithCancel) Context() context.Context {
	return m.ctx
}
