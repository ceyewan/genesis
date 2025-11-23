package main

import (
	"context"
	"fmt"
	"time"

	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/connector"
	"github.com/ceyewan/genesis/pkg/container"
	"github.com/ceyewan/genesis/pkg/dlock"
)

func main() {
	fmt.Println("=== Redis 分布式锁演示 ===")
	fmt.Println()

	// 1. 创建应用级 Logger
	logConfig := &clog.Config{
		Level:  "info",
		Format: "console",
		Output: "stdout",
	}
	appLogger, err := clog.New(logConfig, &clog.Option{
		NamespaceParts: []string{"dlock-redis-demo"},
	})
	if err != nil {
		fmt.Printf("创建 Logger 失败: %v\n", err)
		return
	}

	// 2. 配置容器
	cfg := &container.Config{
		Redis: &connector.RedisConfig{
			Addr:         "127.0.0.1:6379",
			Password:     "your_redis_password",
			DB:           0,
			PoolSize:     10,
			MinIdleConns: 5,
			DialTimeout:  5 * time.Second,
		},
		DLock: &dlock.Config{
			Backend:       dlock.BackendRedis,
			Prefix:        "dlock:",
			DefaultTTL:    10 * time.Second,
			RetryInterval: 100 * time.Millisecond,
		},
	}

	// 3. 创建容器 (使用 Option 注入 Logger)
	c, err := container.New(cfg, container.WithLogger(appLogger))
	if err != nil {
		fmt.Printf("创建容器失败: %v\n", err)
		return
	}
	defer c.Close()

	// 4. 获取锁组件
	locker := c.DLock
	if locker == nil {
		fmt.Println("锁组件未初始化")
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

	fmt.Println("\n=== Redis 分布式锁演示完成 ===")
}
