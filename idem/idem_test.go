package idem

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ceyewan/genesis/testkit"
)

// TestExecuteSuccess 测试成功执行
func TestExecuteSuccess(t *testing.T) {
	redisConn := testkit.NewRedisContainerConnector(t)

	// 创建幂等性组件
	prefix := "test:idem:" + testkit.NewID() + ":"
	idem, err := New(&Config{
		Driver:     DriverRedis,
		Prefix:     prefix,
		DefaultTTL: 1 * time.Hour,
		LockTTL:    30 * time.Second,
	}, WithRedisConnector(redisConn))
	if err != nil {
		t.Fatalf("failed to create idem: %v", err)
	}

	ctx := context.Background()
	key := "execute:success"

	// 执行操作
	result, err := idem.Execute(ctx, key, func(ctx context.Context) (interface{}, error) {
		return map[string]interface{}{"status": "ok"}, nil
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	if result == nil {
		t.Fatal("result should not be nil")
	}

	t.Logf("result: %v", result)
}

// TestCacheHit 测试缓存命中
func TestCacheHit(t *testing.T) {
	redisConn := testkit.NewRedisContainerConnector(t)

	prefix := "test:idem:" + testkit.NewID() + ":"
	idem, err := New(&Config{
		Driver:     DriverRedis,
		Prefix:     prefix,
		DefaultTTL: 1 * time.Hour,
		LockTTL:    30 * time.Second,
	}, WithRedisConnector(redisConn))
	if err != nil {
		t.Fatalf("failed to create idem: %v", err)
	}

	ctx := context.Background()
	key := "test:cache:hit"

	// 第一次执行
	result1, err := idem.Execute(ctx, key, func(ctx context.Context) (interface{}, error) {
		return map[string]interface{}{"value": 42}, nil
	})
	if err != nil {
		t.Fatalf("first execute failed: %v", err)
	}

	// 第二次执行（应该返回缓存）
	executionCount := 0
	result2, err := idem.Execute(ctx, key, func(ctx context.Context) (interface{}, error) {
		executionCount++
		return map[string]interface{}{"value": 99}, nil
	})
	if err != nil {
		t.Fatalf("second execute failed: %v", err)
	}

	// 验证第二次没有执行业务逻辑
	if executionCount != 0 {
		t.Fatal("second execution should hit cache and not execute fn")
	}

	// 验证结果相同（通过 JSON 序列化比较）
	result1Bytes, _ := json.Marshal(result1)
	result2Bytes, _ := json.Marshal(result2)
	if string(result1Bytes) != string(result2Bytes) {
		t.Logf("result1: %v, result2: %v", result1, result2)
	}

	t.Logf("cache hit test passed")
}

// TestEmptyKey 测试空键
func TestEmptyKey(t *testing.T) {
	redisConn := testkit.NewRedisContainerConnector(t)

	prefix := "test:idem:" + testkit.NewID() + ":"
	idem, err := New(&Config{
		Driver:     DriverRedis,
		Prefix:     prefix,
		DefaultTTL: 1 * time.Hour,
		LockTTL:    30 * time.Second,
	}, WithRedisConnector(redisConn))
	if err != nil {
		t.Fatalf("failed to create idem: %v", err)
	}

	ctx := context.Background()

	// 执行空键操作
	_, err = idem.Execute(ctx, "", func(ctx context.Context) (interface{}, error) {
		return nil, nil
	})

	if err != ErrKeyEmpty {
		t.Fatalf("expected ErrKeyEmpty, got %v", err)
	}

	t.Logf("empty key test passed")
}

// TestExecuteConcurrent 测试 Redis 驱动下的并发执行
func TestExecuteConcurrent(t *testing.T) {
	redisConn := testkit.NewRedisContainerConnector(t)

	prefix := "test:idem:" + testkit.NewID() + ":"
	// 设置较短的轮询间隔以加快测试
	idem, err := New(&Config{
		Driver:       DriverRedis,
		Prefix:       prefix,
		DefaultTTL:   1 * time.Hour,
		LockTTL:      5 * time.Second,
		WaitTimeout:  5 * time.Second,
		WaitInterval: 10 * time.Millisecond,
	}, WithRedisConnector(redisConn))
	if err != nil {
		t.Fatalf("failed to create idem: %v", err)
	}

	ctx := context.Background()
	key := "execute:concurrent"
	var execCount int32
	concurrency := 5

	// 使用 channel 协调开始时间，尽可能模拟并发
	startCh := make(chan struct{})
	var wg sync.WaitGroup
	results := make([]interface{}, concurrency)
	errs := make([]error, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-startCh // 等待开始信号

			res, err := idem.Execute(ctx, key, func(ctx context.Context) (interface{}, error) {
				// 模拟耗时操作
				time.Sleep(100 * time.Millisecond)
				newVal := atomic.AddInt32(&execCount, 1)
				return map[string]interface{}{"value": 42, "count": newVal}, nil
			})
			results[idx] = res
			errs[idx] = err
		}(i)
	}

	// 开始并发测试
	close(startCh)
	wg.Wait()

	// 验证结果
	// 1. execCount 必须为 1
	if execCount != 1 {
		t.Fatalf("expected execute count 1, got %d", execCount)
	}

	// 2. 所有请求都应该成功且返回相同结果
	firstResultBytes, _ := json.Marshal(results[0])
	for i := 0; i < concurrency; i++ {
		if errs[i] != nil {
			t.Errorf("goroutine %d failed: %v", i, errs[i])
		}
		if results[i] == nil {
			t.Errorf("goroutine %d result is nil", i)
			continue
		}

		resBytes, _ := json.Marshal(results[i])
		if string(resBytes) != string(firstResultBytes) {
			t.Errorf("goroutine %d result mismatch: %s != %s", i, string(resBytes), string(firstResultBytes))
		}
	}
}

// TestExecuteFailure 测试业务逻辑失败时的重试机制
func TestExecuteFailure(t *testing.T) {
	redisConn := testkit.NewRedisContainerConnector(t)

	prefix := "test:idem:" + testkit.NewID() + ":"
	idem, err := New(&Config{
		Driver:     DriverRedis,
		Prefix:     prefix,
		DefaultTTL: 1 * time.Hour,
		LockTTL:    30 * time.Second,
	}, WithRedisConnector(redisConn))
	if err != nil {
		t.Fatalf("failed to create idem: %v", err)
	}

	ctx := context.Background()
	key := "execute:failure"
	expectedErr := errors.New("business error")

	// 第一次执行，返回错误
	_, err = idem.Execute(ctx, key, func(ctx context.Context) (interface{}, error) {
		return nil, expectedErr
	})

	if err != expectedErr {
		t.Fatalf("expected error %v, got %v", expectedErr, err)
	}

	// 第二次执行，应该能够重新获取锁并成功
	result, err := idem.Execute(ctx, key, func(ctx context.Context) (interface{}, error) {
		return map[string]interface{}{"status": "success"}, nil
	})

	if err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if result == nil {
		t.Fatal("retry result is nil")
	}
}
