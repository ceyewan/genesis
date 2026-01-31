package dlock

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/testkit"
)

// ============================================================================
// Helper Functions
// ============================================================================

func newRedisLockerWithConn(t *testing.T, conn connector.RedisConnector) Locker {
	t.Helper()
	locker, err := New(&Config{
		Driver:        DriverRedis,
		Prefix:        "dlock:test:",
		DefaultTTL:    10 * time.Second,
		RetryInterval: 50 * time.Millisecond,
	}, WithRedisConnector(conn), WithLogger(testkit.NewLogger()))
	if err != nil {
		t.Fatalf("failed to create redis locker: %v", err)
	}
	return locker
}

func newEtcdLockerWithConn(t *testing.T, conn connector.EtcdConnector) Locker {
	t.Helper()
	locker, err := New(&Config{
		Driver:        DriverEtcd,
		Prefix:        "/dlock/test/",
		DefaultTTL:    10 * time.Second,
		RetryInterval: 50 * time.Millisecond,
	}, WithEtcdConnector(conn), WithLogger(testkit.NewLogger()))
	if err != nil {
		t.Fatalf("failed to create etcd locker: %v", err)
	}
	return locker
}

// ============================================================================
// 错误处理测试
// ============================================================================

func TestNew_ConfigNil(t *testing.T) {
	_, err := New(nil)
	if err != ErrConfigNil {
		t.Fatalf("expected ErrConfigNil, got: %v", err)
	}
}

func TestNew_InvalidDriver(t *testing.T) {
	_, err := New(&Config{
		Driver: "invalid",
	})
	if err == nil {
		t.Fatal("expected error for invalid driver")
	}
}

func TestNew_MissingConnector(t *testing.T) {
	_, err := New(&Config{
		Driver: DriverRedis,
	})
	if err == nil {
		t.Fatal("expected error for missing redis connector")
	}

	_, err = New(&Config{
		Driver: DriverEtcd,
	})
	if err == nil {
		t.Fatal("expected error for missing etcd connector")
	}
}

// ============================================================================
// Redis 集成测试
// ============================================================================

func TestRedisLocker_LockUnlock(t *testing.T) {
	ctx, cancel := testkit.NewContext(t, 30*time.Second)
	defer cancel()

	conn := testkit.NewRedisContainerConnector(t)
	locker := newRedisLockerWithConn(t, conn)
	defer locker.Close()

	key := "test:" + testkit.NewID()

	// Lock
	err := locker.Lock(ctx, key)
	if err != nil {
		t.Fatalf("Lock failed: %v", err)
	}

	// Unlock
	err = locker.Unlock(ctx, key)
	if err != nil {
		t.Fatalf("Unlock failed: %v", err)
	}
}

func TestRedisLocker_TryLock(t *testing.T) {
	ctx, cancel := testkit.NewContext(t, 30*time.Second)
	defer cancel()

	conn := testkit.NewRedisContainerConnector(t)
	locker := newRedisLockerWithConn(t, conn)
	defer locker.Close()

	key := "test:" + testkit.NewID()

	// 首次 TryLock 应该成功
	ok, err := locker.TryLock(ctx, key)
	if err != nil {
		t.Fatalf("TryLock failed: %v", err)
	}
	if !ok {
		t.Fatal("expected TryLock to succeed")
	}

	// Unlock
	err = locker.Unlock(ctx, key)
	if err != nil {
		t.Fatalf("Unlock failed: %v", err)
	}

	// Unlock 后 TryLock 应该成功
	ok, err = locker.TryLock(ctx, key)
	if err != nil {
		t.Fatalf("TryLock after unlock failed: %v", err)
	}
	if !ok {
		t.Fatal("expected TryLock to succeed after unlock")
	}

	_ = locker.Unlock(ctx, key)
}

func TestRedisLocker_TryLock_LocallyHeld(t *testing.T) {
	ctx, cancel := testkit.NewContext(t, 30*time.Second)
	defer cancel()

	conn := testkit.NewRedisContainerConnector(t)
	locker := newRedisLockerWithConn(t, conn)
	defer locker.Close()

	key := "test:" + testkit.NewID()

	// 首次 TryLock 应该成功
	ok, err := locker.TryLock(ctx, key)
	if err != nil {
		t.Fatalf("TryLock failed: %v", err)
	}
	if !ok {
		t.Fatal("expected TryLock to succeed")
	}

	// 同一个 locker 再次 TryLock 同一个 key 应该返回错误
	_, err = locker.TryLock(ctx, key)
	if err == nil {
		t.Fatal("expected error for double TryLock on same key")
	}

	_ = locker.Unlock(ctx, key)
}

func TestRedisLocker_TryLock_Contention(t *testing.T) {
	ctx, cancel := testkit.NewContext(t, 30*time.Second)
	defer cancel()

	// 共享同一个 Redis 连接
	conn := testkit.NewRedisContainerConnector(t)
	locker1 := newRedisLockerWithConn(t, conn)
	defer locker1.Close()
	locker2 := newRedisLockerWithConn(t, conn)
	defer locker2.Close()

	key := "test:" + testkit.NewID()

	// locker1 获取锁
	ok, err := locker1.TryLock(ctx, key)
	if err != nil {
		t.Fatalf("locker1 TryLock failed: %v", err)
	}
	if !ok {
		t.Fatal("expected locker1 TryLock to succeed")
	}

	// locker2 应该无法获取锁
	ok, err = locker2.TryLock(ctx, key)
	if err != nil {
		t.Fatalf("locker2 TryLock returned error: %v", err)
	}
	if ok {
		t.Fatal("expected locker2 TryLock to fail")
	}

	// locker1 释放锁
	err = locker1.Unlock(ctx, key)
	if err != nil {
		t.Fatalf("locker1 Unlock failed: %v", err)
	}

	// 现在 locker2 应该能获取锁
	ok, err = locker2.TryLock(ctx, key)
	if err != nil {
		t.Fatalf("locker2 TryLock after unlock failed: %v", err)
	}
	if !ok {
		t.Fatal("expected locker2 TryLock to succeed after unlock")
	}

	_ = locker2.Unlock(ctx, key)
}

func TestRedisLocker_Lock_ContextCancel(t *testing.T) {
	ctx, cancel := testkit.NewContext(t, 30*time.Second)
	defer cancel()

	// 共享同一个 Redis 连接
	conn := testkit.NewRedisContainerConnector(t)
	locker1 := newRedisLockerWithConn(t, conn)
	defer locker1.Close()
	locker2 := newRedisLockerWithConn(t, conn)
	defer locker2.Close()

	key := "test:" + testkit.NewID()

	// locker1 获取锁
	err := locker1.Lock(ctx, key)
	if err != nil {
		t.Fatalf("locker1 Lock failed: %v", err)
	}

	// locker2 尝试获取锁，设置短超时
	shortCtx, shortCancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer shortCancel()

	err = locker2.Lock(shortCtx, key)
	if err != context.DeadlineExceeded {
		t.Fatalf("expected DeadlineExceeded, got: %v", err)
	}

	_ = locker1.Unlock(ctx, key)
}

func TestRedisLocker_WithTTL(t *testing.T) {
	ctx, cancel := testkit.NewContext(t, 30*time.Second)
	defer cancel()

	conn := testkit.NewRedisContainerConnector(t)
	locker := newRedisLockerWithConn(t, conn)
	defer locker.Close()

	key := "test:" + testkit.NewID()

	// 使用短 TTL
	err := locker.Lock(ctx, key, WithTTL(2*time.Second))
	if err != nil {
		t.Fatalf("Lock with TTL failed: %v", err)
	}

	// Unlock
	err = locker.Unlock(ctx, key)
	if err != nil {
		t.Fatalf("Unlock failed: %v", err)
	}
}

func TestRedisLocker_Watchdog(t *testing.T) {
	ctx, cancel := testkit.NewContext(t, 30*time.Second)
	defer cancel()

	conn := testkit.NewRedisContainerConnector(t)
	locker := newRedisLockerWithConn(t, conn)
	defer locker.Close()

	key := "test:" + testkit.NewID()

	// 使用短 TTL 但等待超过 TTL 时间
	// 如果 watchdog 正常工作，锁不会丢失
	err := locker.Lock(ctx, key, WithTTL(2*time.Second))
	if err != nil {
		t.Fatalf("Lock failed: %v", err)
	}

	// 等待超过 TTL
	time.Sleep(3 * time.Second)

	// 应该仍能正常 Unlock（watchdog 续期了）
	err = locker.Unlock(ctx, key)
	if err != nil {
		t.Fatalf("Unlock failed (watchdog may not be working): %v", err)
	}
}

func TestRedisLocker_UnlockNotHeld(t *testing.T) {
	ctx, cancel := testkit.NewContext(t, 30*time.Second)
	defer cancel()

	conn := testkit.NewRedisContainerConnector(t)
	locker := newRedisLockerWithConn(t, conn)
	defer locker.Close()

	key := "test:" + testkit.NewID()

	// Unlock 一个没有持有的锁应该报错
	err := locker.Unlock(ctx, key)
	if err == nil {
		t.Fatal("expected error for unlocking not held lock")
	}
}

func TestRedisLocker_ConcurrentLock(t *testing.T) {
	ctx, cancel := testkit.NewContext(t, 60*time.Second)
	defer cancel()

	// 共享同一个 Redis 连接，但每个 goroutine 使用独立的 locker
	// 这模拟了多个客户端/进程竞争同一把分布式锁的场景
	conn := testkit.NewRedisContainerConnector(t)
	key := "test:" + testkit.NewID()

	var counter int64
	var wg sync.WaitGroup
	numGoroutines := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// 每个 goroutine 使用独立的 locker（模拟独立客户端）
			locker := newRedisLockerWithConn(t, conn)
			defer locker.Close()

			err := locker.Lock(ctx, key)
			if err != nil {
				t.Errorf("Lock failed: %v", err)
				return
			}

			// 临界区
			atomic.AddInt64(&counter, 1)

			err = locker.Unlock(ctx, key)
			if err != nil {
				t.Errorf("Unlock failed: %v", err)
			}
		}()
	}

	wg.Wait()

	if counter != int64(numGoroutines) {
		t.Fatalf("expected counter=%d, got=%d", numGoroutines, counter)
	}
}

// TestRedisLocker_ReentrantFails 验证同一 locker 重入会报错
func TestRedisLocker_ReentrantFails(t *testing.T) {
	ctx, cancel := testkit.NewContext(t, 30*time.Second)
	defer cancel()

	conn := testkit.NewRedisContainerConnector(t)
	locker := newRedisLockerWithConn(t, conn)
	defer locker.Close()

	key := "test:" + testkit.NewID()

	// 第一次 Lock 成功
	err := locker.Lock(ctx, key)
	if err != nil {
		t.Fatalf("first Lock failed: %v", err)
	}

	// 同一 locker 再次 Lock 同一 key 应该报错
	err = locker.Lock(ctx, key)
	if err == nil {
		t.Fatal("expected error for reentrant lock")
	}

	_ = locker.Unlock(ctx, key)
}

// ============================================================================
// Etcd 集成测试
// ============================================================================

func TestEtcdLocker_LockUnlock(t *testing.T) {
	ctx, cancel := testkit.NewContext(t, 30*time.Second)
	defer cancel()

	conn := testkit.NewEtcdContainerConnector(t)
	locker := newEtcdLockerWithConn(t, conn)
	defer locker.Close()

	key := "test:" + testkit.NewID()

	// Lock
	err := locker.Lock(ctx, key)
	if err != nil {
		t.Fatalf("Lock failed: %v", err)
	}

	// Unlock
	err = locker.Unlock(ctx, key)
	if err != nil {
		t.Fatalf("Unlock failed: %v", err)
	}
}

func TestEtcdLocker_TryLock(t *testing.T) {
	ctx, cancel := testkit.NewContext(t, 30*time.Second)
	defer cancel()

	conn := testkit.NewEtcdContainerConnector(t)
	locker := newEtcdLockerWithConn(t, conn)
	defer locker.Close()

	key := "test:" + testkit.NewID()

	// 首次 TryLock 应该成功
	ok, err := locker.TryLock(ctx, key)
	if err != nil {
		t.Fatalf("TryLock failed: %v", err)
	}
	if !ok {
		t.Fatal("expected TryLock to succeed")
	}

	// Unlock
	err = locker.Unlock(ctx, key)
	if err != nil {
		t.Fatalf("Unlock failed: %v", err)
	}

	// Unlock 后 TryLock 应该成功
	ok, err = locker.TryLock(ctx, key)
	if err != nil {
		t.Fatalf("TryLock after unlock failed: %v", err)
	}
	if !ok {
		t.Fatal("expected TryLock to succeed after unlock")
	}

	_ = locker.Unlock(ctx, key)
}

func TestEtcdLocker_TryLock_Contention(t *testing.T) {
	ctx, cancel := testkit.NewContext(t, 30*time.Second)
	defer cancel()

	// 共享同一个 Etcd 连接
	conn := testkit.NewEtcdContainerConnector(t)
	locker1 := newEtcdLockerWithConn(t, conn)
	defer locker1.Close()
	locker2 := newEtcdLockerWithConn(t, conn)
	defer locker2.Close()

	key := "test:" + testkit.NewID()

	// locker1 获取锁
	ok, err := locker1.TryLock(ctx, key)
	if err != nil {
		t.Fatalf("locker1 TryLock failed: %v", err)
	}
	if !ok {
		t.Fatal("expected locker1 TryLock to succeed")
	}

	// locker2 应该无法获取锁
	ok, err = locker2.TryLock(ctx, key)
	if err != nil {
		t.Fatalf("locker2 TryLock returned error: %v", err)
	}
	if ok {
		t.Fatal("expected locker2 TryLock to fail")
	}

	// locker1 释放锁
	err = locker1.Unlock(ctx, key)
	if err != nil {
		t.Fatalf("locker1 Unlock failed: %v", err)
	}

	// 现在 locker2 应该能获取锁
	ok, err = locker2.TryLock(ctx, key)
	if err != nil {
		t.Fatalf("locker2 TryLock after unlock failed: %v", err)
	}
	if !ok {
		t.Fatal("expected locker2 TryLock to succeed after unlock")
	}

	_ = locker2.Unlock(ctx, key)
}

func TestEtcdLocker_UnlockNotHeld(t *testing.T) {
	ctx, cancel := testkit.NewContext(t, 30*time.Second)
	defer cancel()

	conn := testkit.NewEtcdContainerConnector(t)
	locker := newEtcdLockerWithConn(t, conn)
	defer locker.Close()

	key := "test:" + testkit.NewID()

	// Unlock 一个没有持有的锁应该报错
	err := locker.Unlock(ctx, key)
	if err == nil {
		t.Fatal("expected error for unlocking not held lock")
	}
}

func TestEtcdLocker_WithTTL(t *testing.T) {
	ctx, cancel := testkit.NewContext(t, 30*time.Second)
	defer cancel()

	conn := testkit.NewEtcdContainerConnector(t)
	locker := newEtcdLockerWithConn(t, conn)
	defer locker.Close()

	key := "test:" + testkit.NewID()

	// 使用短 TTL
	err := locker.Lock(ctx, key, WithTTL(5*time.Second))
	if err != nil {
		t.Fatalf("Lock with TTL failed: %v", err)
	}

	// Unlock
	err = locker.Unlock(ctx, key)
	if err != nil {
		t.Fatalf("Unlock failed: %v", err)
	}
}
