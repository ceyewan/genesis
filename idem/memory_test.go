package idem

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMemoryExecuteCache(t *testing.T) {
	idem, err := New(&Config{
		Driver:     DriverMemory,
		Prefix:     "test:idem:mem:",
		DefaultTTL: 1 * time.Minute,
		LockTTL:    5 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create idem: %v", err)
	}

	ctx := context.Background()
	key := "execute:cache"
	execCount := 0

	result1, err := idem.Execute(ctx, key, func(ctx context.Context) (interface{}, error) {
		execCount++
		return map[string]interface{}{"value": 42}, nil
	})
	if err != nil {
		t.Fatalf("first execute failed: %v", err)
	}

	result2, err := idem.Execute(ctx, key, func(ctx context.Context) (interface{}, error) {
		execCount++
		return map[string]interface{}{"value": 99}, nil
	})
	if err != nil {
		t.Fatalf("second execute failed: %v", err)
	}

	if execCount != 1 {
		t.Fatalf("expected execute count 1, got %d", execCount)
	}

	result1Bytes, _ := json.Marshal(result1)
	result2Bytes, _ := json.Marshal(result2)
	if string(result1Bytes) != string(result2Bytes) {
		t.Fatalf("expected cached result, got %s != %s", string(result1Bytes), string(result2Bytes))
	}
}

func TestMemoryConsume(t *testing.T) {
	idem, err := New(&Config{
		Driver:     DriverMemory,
		Prefix:     "test:idem:consume:",
		DefaultTTL: 1 * time.Minute,
		LockTTL:    5 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create idem: %v", err)
	}

	ctx := context.Background()
	key := "consume:msg:1"
	execCount := 0

	executed, err := idem.Consume(ctx, key, 30*time.Second, func(ctx context.Context) error {
		execCount++
		return nil
	})
	if err != nil {
		t.Fatalf("first consume failed: %v", err)
	}
	if !executed {
		t.Fatal("expected first consume to execute")
	}

	executed, err = idem.Consume(ctx, key, 30*time.Second, func(ctx context.Context) error {
		execCount++
		return nil
	})
	if err != nil {
		t.Fatalf("second consume failed: %v", err)
	}
	if executed {
		t.Fatal("expected second consume to skip")
	}
	if execCount != 1 {
		t.Fatalf("expected execute count 1, got %d", execCount)
	}
}

func TestMemoryExecuteConcurrent(t *testing.T) {
	idem, err := New(&Config{
		Driver:       DriverMemory,
		Prefix:       "test:idem:concurrent:",
		DefaultTTL:   1 * time.Minute,
		LockTTL:      2 * time.Second,
		WaitTimeout:  2 * time.Second,
		WaitInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("failed to create idem: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var execCount int32
	results := make([]interface{}, 2)
	errs := make([]error, 2)

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			res, err := idem.Execute(ctx, "execute:concurrent", func(ctx context.Context) (interface{}, error) {
				atomic.AddInt32(&execCount, 1)
				time.Sleep(100 * time.Millisecond)
				return map[string]interface{}{"value": 42}, nil
			})
			results[idx] = res
			errs[idx] = err
		}(i)
	}
	wg.Wait()

	if execCount != 1 {
		t.Fatalf("expected execute count 1, got %d", execCount)
	}
	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("unexpected errors: %v, %v", errs[0], errs[1])
	}
	if results[0] == nil || results[1] == nil {
		t.Fatalf("results should not be nil")
	}
}
