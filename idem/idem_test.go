package idem

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ceyewan/genesis/testkit"
)

// TestExecuteSuccess 测试成功执行
func TestExecuteSuccess(t *testing.T) {
	redisConn := testkit.GetRedisConnector(t)

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
	redisConn := testkit.GetRedisConnector(t)

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
	redisConn := testkit.GetRedisConnector(t)

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
