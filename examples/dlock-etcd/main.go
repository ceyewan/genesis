package main

import (
	"context"
	"fmt"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/dlock"
)

func main() {
	fmt.Println("=== Etcd 分布式锁演示 ===")
	fmt.Println()

	// 1. 创建应用级 Logger
	appLogger, err := clog.New(&clog.Config{
		Level:  "info",
		Format: "console",
		Output: "stdout",
	})
	if err != nil {
		fmt.Printf("创建 Logger 失败: %v\n", err)
		return
	}

	// 2. 创建 Etcd 连接器
	etcdConn, err := connector.NewEtcd(&connector.EtcdConfig{
		Endpoints: []string{"127.0.0.1:2379"},
		Timeout:   5 * time.Second,
	}, connector.WithLogger(appLogger))
	if err != nil {
		fmt.Printf("创建 Etcd 连接器失败: %v\n", err)
		return
	}
	defer etcdConn.Close()

	// 3. 创建分布式锁组件
	locker, err := dlock.NewEtcd(etcdConn, &dlock.Config{
		Prefix:        "dlock:",
		DefaultTTL:    10 * time.Second,
		RetryInterval: 100 * time.Millisecond,
	}, dlock.WithLogger(appLogger))
	if err != nil {
		fmt.Printf("创建分布式锁失败: %v\n", err)
		return
	}

	// 4. 测试加锁
	ctx := context.Background()
	key := "resource:1"

	fmt.Println("尝试加锁...")
	if err := locker.Lock(ctx, key); err != nil {
		fmt.Printf("加锁失败: %v\n", err)
		return
	}
	fmt.Println("加锁成功")

	// 5. 业务逻辑
	fmt.Println("执行业务逻辑...")
	time.Sleep(1 * time.Second)

	// 6. 释放锁
	fmt.Println("尝试释放锁...")
	if err := locker.Unlock(ctx, key); err != nil {
		fmt.Printf("释放锁失败: %v\n", err)
		return
	}
	fmt.Println("释放锁成功")

	// 7. 测试 TryLock
	fmt.Println("\n测试 TryLock...")
	success, err := locker.TryLock(ctx, key)
	if err != nil {
		fmt.Printf("TryLock 失败: %v\n", err)
		return
	}
	if success {
		fmt.Println("TryLock 成功")
		locker.Unlock(ctx, key)
	} else {
		fmt.Println("TryLock 失败（锁被占用）")
	}

	// 8. 测试 WithTTL
	fmt.Println("\n测试 WithTTL...")
	if err := locker.Lock(ctx, key, dlock.WithTTL(2*time.Second)); err != nil {
		fmt.Printf("WithTTL 加锁失败: %v\n", err)
		return
	}
	fmt.Println("WithTTL 加锁成功")
	locker.Unlock(ctx, key)

	// 9. 测试多次重试
	fmt.Println("\n测试多次重试...")
	// 先获取锁
	locker.Lock(ctx, key)
	// 在另一个 goroutine 中尝试获取锁
	go func() {
		if err := locker.Lock(ctx, key); err != nil {
			fmt.Printf("重试加锁失败（预期）: %v\n", err)
		} else {
			fmt.Println("重试加锁成功")
			locker.Unlock(ctx, key)
		}
	}()
	// 等待一下然后释放锁
	time.Sleep(1 * time.Second)
	locker.Unlock(ctx, key)
	time.Sleep(2 * time.Second) // 等待重试完成

	fmt.Println("\n=== Etcd 分布式锁演示完成 ===")
}
