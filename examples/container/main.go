package main

import (
	"context"
	"fmt"
	"time"

	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/connector"
	"github.com/ceyewan/genesis/pkg/container"
	"github.com/ceyewan/genesis/pkg/dlock"
	dlocktypes "github.com/ceyewan/genesis/pkg/dlock/types"
)

func main() {
	fmt.Println("=== Container 示例 ===")
	fmt.Println()

	// 示例 1: 基础用法 - 使用默认 Logger
	example1BasicUsage()

	fmt.Println()
	fmt.Println("---")
	fmt.Println()

	// 示例 2: 使用 Option 注入自定义 Logger
	example2WithCustomLogger()
}

// example1BasicUsage 演示基础用法 - Container 会自动创建默认 Logger
func example1BasicUsage() {
	fmt.Println("示例 1: 基础用法 (使用默认 Logger)")

	cfg := &container.Config{
		Redis: &connector.RedisConfig{
			Addr:         "127.0.0.1:6379",
			Password:     "",
			DB:           0,
			PoolSize:     10,
			MinIdleConns: 5,
			MaxRetries:   3,
			DialTimeout:  5 * time.Second,
			ReadTimeout:  3 * time.Second,
			WriteTimeout: 3 * time.Second,
		},
		DLock: &dlock.Config{
			Backend:       dlocktypes.BackendRedis,
			Prefix:        "dlock:",
			DefaultTTL:    30 * time.Second,
			RetryInterval: 100 * time.Millisecond,
		},
	}

	// 创建容器 (不传 Option，使用默认配置)
	app, err := container.New(cfg)
	if err != nil {
		fmt.Printf("创建容器失败: %v\n", err)
		return
	}
	defer app.Close()

	fmt.Println("✓ 容器创建成功 (使用默认 Logger)")
	fmt.Println("✓ Logger namespace: container")

	// 使用分布式锁
	if app.DLock != nil {
		testDLock(app.DLock)
	}
}

// example2WithCustomLogger 演示使用 Option 注入自定义 Logger
func example2WithCustomLogger() {
	fmt.Println("示例 2: 使用 Option 注入自定义 Logger")

	// 1. 创建应用级 Logger
	logConfig := &clog.Config{
		Level:       "info",
		Format:      "json",
		Output:      "stdout",
		AddSource:   true,
		EnableColor: false,
	}

	appLogger, err := clog.New(logConfig, &clog.Option{
		NamespaceParts: []string{"my-service"},
	})
	if err != nil {
		fmt.Printf("创建 Logger 失败: %v\n", err)
		return
	}

	// 2. 创建容器配置
	cfg := &container.Config{
		Redis: &connector.RedisConfig{
			Addr:         "127.0.0.1:6379",
			Password:     "",
			DB:           0,
			PoolSize:     10,
			MinIdleConns: 5,
			MaxRetries:   3,
			DialTimeout:  5 * time.Second,
			ReadTimeout:  3 * time.Second,
			WriteTimeout: 3 * time.Second,
		},
		DLock: &dlock.Config{
			Backend:       dlocktypes.BackendRedis,
			Prefix:        "dlock:",
			DefaultTTL:    30 * time.Second,
			RetryInterval: 100 * time.Millisecond,
		},
	}

	// 3. 使用 Option 注入 Logger
	app, err := container.New(cfg, container.WithLogger(appLogger))
	if err != nil {
		fmt.Printf("创建容器失败: %v\n", err)
		return
	}
	defer app.Close()

	fmt.Println("✓ 容器创建成功 (使用自定义 Logger)")
	fmt.Println("✓ Logger namespace: my-service")
	fmt.Println("✓ Logger format: json")

	// 使用分布式锁
	if app.DLock != nil {
		testDLock(app.DLock)
	}
}

// testDLock 测试分布式锁功能
func testDLock(locker dlock.Locker) {
	ctx := context.Background()
	lockKey := "test:lock"

	// 尝试获取锁
	acquired, err := locker.TryLock(ctx, lockKey)
	if err != nil {
		fmt.Printf("  获取锁失败: %v\n", err)
		return
	}

	if acquired {
		fmt.Printf("✓ 成功获取锁: %s\n", lockKey)
		defer locker.Unlock(ctx, lockKey)
	} else {
		fmt.Printf("  锁已被占用: %s\n", lockKey)
	}
}
