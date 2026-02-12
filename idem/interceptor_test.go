package idem

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/ceyewan/genesis/testkit"
)

func TestUnaryServerInterceptor(t *testing.T) {
	redisConn := testkit.NewRedisContainerConnector(t)

	prefix := "test:idem:interceptor:" + testkit.NewID() + ":"
	idemComp, err := New(&Config{
		Driver:     DriverRedis,
		Prefix:     prefix,
		DefaultTTL: 1 * time.Hour,
		LockTTL:    5 * time.Second,
	}, WithRedisConnector(redisConn))
	if err != nil {
		t.Fatalf("failed to create idem: %v", err)
	}

	interceptor := idemComp.UnaryServerInterceptor()

	// 模拟 Handler
	var handlerExecCount int32
	//nolint:unparam
	handler := func(_ context.Context, _ interface{}) (interface{}, error) {
		atomic.AddInt32(&handlerExecCount, 1)
		// 返回一个 proto.Message
		return wrapperspb.String("success"), nil
	}

	//nolint:unparam
	errorHandler := func(_ context.Context, _ interface{}) (interface{}, error) {
		atomic.AddInt32(&handlerExecCount, 1)
		return nil, errors.New("rpc error")
	}

	info := &grpc.UnaryServerInfo{
		FullMethod: "/test.Service/Method",
	}

	// 1. 测试正常调用
	t.Run("Normal Call", func(t *testing.T) {
		ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-idem-key", "rpc-1"))
		resp, err := interceptor(ctx, "req", info, handler)

		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if atomic.LoadInt32(&handlerExecCount) != 1 {
			t.Errorf("expected exec count 1, got %d", handlerExecCount)
		}

		// 验证返回值
		msg, ok := resp.(*wrapperspb.StringValue)
		if !ok || msg.Value != "success" {
			t.Errorf("unexpected response: %v", resp)
		}
	})

	// 2. 测试重复调用（缓存命中）
	t.Run("Duplicate Call", func(t *testing.T) {
		ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-idem-key", "rpc-1"))
		resp, err := interceptor(ctx, "req", info, handler)

		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		// handlerExecCount 应该仍然是 1
		if atomic.LoadInt32(&handlerExecCount) != 1 {
			t.Errorf("expected exec count 1, got %d", handlerExecCount)
		}

		msg, ok := resp.(*wrapperspb.StringValue)
		if !ok || msg.Value != "success" {
			t.Errorf("unexpected response: %v", resp)
		}
	})

	// 3. 测试无 Metadata 调用
	t.Run("No Metadata", func(t *testing.T) {
		ctx := context.Background()
		resp, err := interceptor(ctx, "req", info, handler)

		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		// handlerExecCount 应该增加到 2
		if atomic.LoadInt32(&handlerExecCount) != 2 {
			t.Errorf("expected exec count 2, got %d", handlerExecCount)
		}

		msg, ok := resp.(*wrapperspb.StringValue)
		if !ok || msg.Value != "success" {
			t.Errorf("unexpected response: %v", resp)
		}
	})

	// 4. 测试错误调用（不缓存）
	t.Run("Error Call", func(t *testing.T) {
		key := "rpc-error"
		ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-idem-key", key))

		// 第一次调用
		_, err := interceptor(ctx, "req", info, errorHandler)
		if err == nil {
			t.Error("expected error")
		}

		currentCount := atomic.LoadInt32(&handlerExecCount)

		// 第二次调用，应该再次执行
		_, err = interceptor(ctx, "req", info, errorHandler)
		if err == nil {
			t.Error("expected error")
		}

		if atomic.LoadInt32(&handlerExecCount) != currentCount+1 {
			t.Errorf("expected exec count to increment")
		}
	})

	// 5. 测试非 Proto 消息返回（不缓存但成功返回）
	t.Run("Non-Proto Response", func(t *testing.T) {
		nonProtoHandler := func(ctx context.Context, req interface{}) (interface{}, error) {
			atomic.AddInt32(&handlerExecCount, 1)
			return "not-a-proto-message", nil
		}

		key := "rpc-non-proto"
		ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-idem-key", key))

		// 第一次调用
		resp, err := interceptor(ctx, "req", info, nonProtoHandler)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if resp != "not-a-proto-message" {
			t.Errorf("unexpected response: %v", resp)
		}

		currentCount := atomic.LoadInt32(&handlerExecCount)

		// 第二次调用，因为没有缓存（类型不对），应该再次执行
		resp, err = interceptor(ctx, "req", info, nonProtoHandler)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if resp != "not-a-proto-message" {
			t.Errorf("unexpected response: %v", resp)
		}

		if atomic.LoadInt32(&handlerExecCount) != currentCount+1 {
			t.Errorf("expected exec count to increment for non-proto response")
		}
	})
}

// 辅助函数，确保 anypb 能够工作
func init() {
	// 注册 wrapper 类型（通常由 protoc 生成代码自动完成）
	// 这里直接使用 known types，应该无需额外操作
	_ = proto.Message(&wrapperspb.StringValue{})
}
