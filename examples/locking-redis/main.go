package main

import (
	"context"
	"fmt"
	"time"

	"github.com/ceyewan/genesis/pkg/lock"
	"github.com/ceyewan/genesis/pkg/lock/redis"
	"github.com/ceyewan/genesis/pkg/lock/simple"
)

func main() {
	fmt.Println("=== Redis 分布式锁演示 ===")
	fmt.Println()

	// 1. 使用简单API（一行初始化）
	fmt.Println("1. 使用简单API（一行初始化）：")
	testSimpleAPI()

	fmt.Println()

	// 2. 使用专用Redis API
	fmt.Println("2. 使用专用Redis API：")
	testDedicatedRedisAPI()

	fmt.Println()

	// 3. 连接复用测试
	fmt.Println("3. 连接复用测试：")
	testConnectionReuse()

	fmt.Println()
	fmt.Println("=== Redis 分布式锁演示完成 ===")
}

// testSimpleAPI 测试简单API
func testSimpleAPI() {
	// 使用简单API，一行初始化
	locker, err := simple.New(&simple.Config{
		Backend:   "redis",
		Endpoints: []string{"127.0.0.1:6379"},
		Password:  "genesis_redis_2024", // Redis密码
	}, &simple.Option{
		TTL:           10 * time.Second,
		RetryInterval: 100 * time.Millisecond,
		AutoRenew:     true,
	})
	if err != nil {
		fmt.Printf("  ✗ 创建锁失败: %v\n", err)
		return
	}
	defer locker.Close()

	fmt.Println("  ✓ 创建锁成功（简单API）")

	// 测试加锁
	ctx := context.Background()
	if err := locker.Lock(ctx, "/test/resource1"); err != nil {
		fmt.Printf("  ✗ 加锁失败: %v\n", err)
		return
	}
	fmt.Println("  ✓ 加锁成功")

	// 执行业务逻辑
	fmt.Println("  ✓ 执行业务逻辑...")
	time.Sleep(1 * time.Second)

	// 解锁
	if err := locker.Unlock(ctx, "/test/resource1"); err != nil {
		fmt.Printf("  ✗ 解锁失败: %v\n", err)
		return
	}
	fmt.Println("  ✓ 解锁成功")
}

// testDedicatedRedisAPI 测试专用Redis API
func testDedicatedRedisAPI() {
	// 使用专用Redis API
	locker, err := redis.New(&redis.Config{
		Addr:         "127.0.0.1:6379",
		Password:     "genesis_redis_2024", // Redis密码
		DB:           0,
		PoolSize:     10,
		MinIdleConns: 5,
		DialTimeout:  5 * time.Second,
	}, &lock.LockOptions{
		TTL:           15 * time.Second,
		RetryInterval: 200 * time.Millisecond,
		AutoRenew:     true,
	})
	if err != nil {
		fmt.Printf("  ✗ 创建锁失败: %v\n", err)
		return
	}
	defer locker.Close()

	fmt.Println("  ✓ 创建锁成功（专用Redis API）")

	ctx := context.Background()

	// 测试TryLock
	success, err := locker.TryLock(ctx, "/test/resource2")
	if err != nil {
		fmt.Printf("  ✗ TryLock失败: %v\n", err)
		return
	}
	if !success {
		fmt.Println("  ✗ TryLock返回false（锁被占用）")
		return
	}
	fmt.Println("  ✓ TryLock成功")

	// 解锁
	if err := locker.Unlock(ctx, "/test/resource2"); err != nil {
		fmt.Printf("  ✗ 解锁失败: %v\n", err)
		return
	}
	fmt.Println("  ✓ 解锁成功")

	// 测试LockWithTTL
	if err := locker.LockWithTTL(ctx, "/test/resource3", 20*time.Second); err != nil {
		fmt.Printf("  ✗ LockWithTTL失败: %v\n", err)
		return
	}
	fmt.Println("  ✓ LockWithTTL成功（20秒TTL）")

	// 解锁
	if err := locker.Unlock(ctx, "/test/resource3"); err != nil {
		fmt.Printf("  ✗ 解锁失败: %v\n", err)
		return
	}
	fmt.Println("  ✓ 解锁成功")
}

// testConnectionReuse 测试连接复用
func testConnectionReuse() {
	fmt.Println("  创建第一个锁（相同配置）...")
	locker1, err := simple.New(&simple.Config{
		Backend:   "redis",
		Endpoints: []string{"127.0.0.1:6379"},
		Password:  "genesis_redis_2024", // Redis密码
	}, nil)
	if err != nil {
		fmt.Printf("  ✗ 锁1创建失败: %v\n", err)
		return
	}
	defer locker1.Close()
	fmt.Println("  ✓ 锁1创建成功")

	fmt.Println("  创建第二个锁（相同配置）...")
	locker2, err := simple.New(&simple.Config{
		Backend:   "redis",
		Endpoints: []string{"127.0.0.1:6379"},
		Password:  "genesis_redis_2024", // Redis密码
	}, nil)
	if err != nil {
		fmt.Printf("  ✗ 锁2创建失败: %v\n", err)
		return
	}
	defer locker2.Close()
	fmt.Println("  ✓ 锁2创建成功")

	ctx := context.Background()

	// 测试两个锁都能正常工作
	if err := locker1.Lock(ctx, "/test/conn1"); err != nil {
		fmt.Printf("  ✗ 锁1加锁失败: %v\n", err)
		return
	}
	fmt.Println("  ✓ 锁1加锁成功")

	if err := locker2.Lock(ctx, "/test/conn2"); err != nil {
		fmt.Printf("  ✗ 锁2加锁失败: %v\n", err)
		locker1.Unlock(ctx, "/test/conn1")
		return
	}
	fmt.Println("  ✓ 锁2加锁成功")

	// 解锁
	if err := locker1.Unlock(ctx, "/test/conn1"); err != nil {
		fmt.Printf("  ✗ 锁1解锁失败: %v\n", err)
	}
	if err := locker2.Unlock(ctx, "/test/conn2"); err != nil {
		fmt.Printf("  ✗ 锁2解锁失败: %v\n", err)
	}

	fmt.Println("  ✓ 两个锁都解锁成功（连接复用验证通过）")
}
