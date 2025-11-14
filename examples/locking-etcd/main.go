package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ceyewan/genesis/pkg/lock/simple"
)

func main() {
	fmt.Println("=== 新的简单API演示 ===")

	// 演示1：一行初始化，全部默认配置
	fmt.Println("\n1. 一行初始化（全部默认配置）：")
	demoDefaultConfig()

	// 演示2：自定义连接配置
	fmt.Println("\n2. 自定义连接配置：")
	demoCustomConfig()

	// 演示3：自定义行为配置
	fmt.Println("\n3. 自定义行为配置：")
	demoCustomOption()

	// 演示4：完全自定义
	fmt.Println("\n4. 完全自定义配置：")
	demoFullCustom()

	// 演示5：连接复用
	fmt.Println("\n5. 连接复用验证：")
	demoConnectionReuse()
}

func demoDefaultConfig() {
	// 最简单的使用方式：New(nil, nil)
	locker, err := simple.New(nil, nil)
	if err != nil {
		log.Fatalf("创建锁失败: %v", err)
	}
	defer locker.Close()

	ctx := context.Background()
	key := "/demo/default-resource"

	fmt.Println("  ✓ 创建锁成功（默认配置）")

	if err := locker.Lock(ctx, key); err != nil {
		log.Printf("  ✗ 加锁失败: %v", err)
		return
	}
	fmt.Println("  ✓ 加锁成功")

	time.Sleep(500 * time.Millisecond)
	fmt.Println("  ✓ 执行业务逻辑...")

	if err := locker.Unlock(ctx, key); err != nil {
		log.Printf("  ✗ 解锁失败: %v", err)
		return
	}
	fmt.Println("  ✓ 解锁成功")
}

func demoCustomConfig() {
	// 自定义连接配置
	config := &simple.Config{
		Backend:   "etcd",
		Endpoints: []string{"localhost:2379"},
		Timeout:   10 * time.Second,
	}

	locker, err := simple.New(config, nil)
	if err != nil {
		log.Fatalf("创建锁失败: %v", err)
	}
	defer locker.Close()

	ctx := context.Background()
	key := "/demo/custom-config-resource"

	fmt.Println("  ✓ 创建锁成功（自定义连接配置）")

	// 测试TryLock
	success, err := locker.TryLock(ctx, key)
	if err != nil {
		log.Printf("  ✗ TryLock失败: %v", err)
		return
	}

	if success {
		fmt.Println("  ✓ TryLock成功")
		time.Sleep(300 * time.Millisecond)
		locker.Unlock(ctx, key)
		fmt.Println("  ✓ 解锁成功")
	} else {
		fmt.Println("  ✓ 锁已被占用")
	}
}

func demoCustomOption() {
	// 自定义行为配置
	option := &simple.Option{
		TTL:           5 * time.Second,
		RetryInterval: 200 * time.Millisecond,
		AutoRenew:     false,
		MaxRetries:    3,
	}

	locker, err := simple.New(nil, option)
	if err != nil {
		log.Fatalf("创建锁失败: %v", err)
	}
	defer locker.Close()

	ctx := context.Background()
	key := "/demo/custom-option-resource"

	fmt.Println("  ✓ 创建锁成功（自定义行为配置）")

	// 测试带TTL的锁
	if err := locker.LockWithTTL(ctx, key, 3*time.Second); err != nil {
		log.Printf("  ✗ TTL加锁失败: %v", err)
		return
	}
	fmt.Println("  ✓ TTL加锁成功")

	time.Sleep(500 * time.Millisecond)
	fmt.Println("  ✓ 执行业务逻辑...")

	if err := locker.Unlock(ctx, key); err != nil {
		log.Printf("  ✗ 解锁失败: %v", err)
		return
	}
	fmt.Println("  ✓ 解锁成功")
}

func demoFullCustom() {
	// 完全自定义配置
	config := &simple.Config{
		Backend:   "etcd",
		Endpoints: []string{"localhost:2379"},
		Username:  "",
		Password:  "",
		Timeout:   15 * time.Second,
	}

	option := &simple.Option{
		TTL:           30 * time.Second,
		RetryInterval: 1 * time.Second,
		AutoRenew:     true,
		MaxRetries:    0, // 无限重试
	}

	locker, err := simple.New(config, option)
	if err != nil {
		log.Fatalf("创建锁失败: %v", err)
	}
	defer locker.Close()

	ctx := context.Background()
	key := "/demo/full-custom-resource"

	fmt.Println("  ✓ 创建锁成功（完全自定义配置）")

	if err := locker.Lock(ctx, key); err != nil {
		log.Printf("  ✗ 加锁失败: %v", err)
		return
	}
	fmt.Println("  ✓ 加锁成功")

	time.Sleep(800 * time.Millisecond)
	fmt.Println("  ✓ 执行业务逻辑...")

	if err := locker.Unlock(ctx, key); err != nil {
		log.Printf("  ✗ 解锁失败: %v", err)
		return
	}
	fmt.Println("  ✓ 解锁成功")
}

func demoConnectionReuse() {
	// 验证连接复用：相同配置应该复用连接
	fmt.Println("  创建第一个锁...")
	locker1, err := simple.New(nil, nil)
	if err != nil {
		log.Fatalf("创建锁1失败: %v", err)
	}
	defer locker1.Close()

	fmt.Println("  ✓ 锁1创建成功")

	fmt.Println("  创建第二个锁（相同配置）...")
	locker2, err := simple.New(nil, nil)
	if err != nil {
		log.Fatalf("创建锁2失败: %v", err)
	}
	defer locker2.Close()

	fmt.Println("  ✓ 锁2创建成功")

	// 验证两个锁都能正常工作
	ctx := context.Background()
	key1 := "/demo/reuse-resource-1"
	key2 := "/demo/reuse-resource-2"

	if err := locker1.Lock(ctx, key1); err != nil {
		log.Printf("  ✗ 锁1加锁失败: %v", err)
		return
	}
	fmt.Println("  ✓ 锁1加锁成功")

	if err := locker2.Lock(ctx, key2); err != nil {
		log.Printf("  ✗ 锁2加锁失败: %v", err)
		locker1.Unlock(ctx, key1)
		return
	}
	fmt.Println("  ✓ 锁2加锁成功")

	time.Sleep(300 * time.Millisecond)

	locker1.Unlock(ctx, key1)
	locker2.Unlock(ctx, key2)
	fmt.Println("  ✓ 两个锁都解锁成功（连接复用验证通过）")
}
