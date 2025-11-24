package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ceyewan/genesis/pkg/breaker"
	"github.com/ceyewan/genesis/pkg/breaker/types"
	"github.com/ceyewan/genesis/pkg/clog"
)

func main() {
	// 1. 创建 Logger
	logger := clog.Must(&clog.Config{
		Level:  "info",
		Format: "console",
		Output: "stdout",
	}, nil)

	// 2. 创建熔断器
	b, err := breaker.New(&types.Config{
		Default: types.Policy{
			FailureThreshold:    0.5,             // 50% 失败率触发熔断
			WindowSize:          20,              // 较小的窗口便于测试
			MinRequests:         5,               // 最小请求数
			OpenTimeout:         5 * time.Second, // 较短的超时便于测试
			HalfOpenMaxRequests: 2,               // 半开状态允许2个探测请求
		},
		Services: map[string]types.Policy{
			"user.v1.UserService": {
				FailureThreshold:    0.3, // 用户服务更敏感
				WindowSize:          10,
				OpenTimeout:         5 * time.Second, // 使用与默认相同的超时
				HalfOpenMaxRequests: 2,
			},
		},
	}, breaker.WithLogger(logger))

	if err != nil {
		log.Fatal("Failed to create breaker:", err)
	}

	ctx := context.Background()

	// 3. 测试正常情况
	fmt.Println("=== 测试正常情况 ===")
	for i := 0; i < 3; i++ {
		err := b.Execute(ctx, "user.v1.UserService", func() error {
			fmt.Printf("Request %d: Success\n", i+1)
			return nil
		})
		if err != nil {
			fmt.Printf("Request %d: Error - %v\n", i+1, err)
		}
	}

	// 4. 测试触发熔断
	fmt.Println("\n=== 测试触发熔断 ===")
	for i := 0; i < 10; i++ {
		err := b.Execute(ctx, "user.v1.UserService", func() error {
			fmt.Printf("Request %d: Simulating failure\n", i+1)
			return fmt.Errorf("service unavailable")
		})
		if err != nil {
			if err == types.ErrOpenState {
				fmt.Printf("Request %d: Circuit breaker is OPEN - %v\n", i+1, err)
			} else {
				fmt.Printf("Request %d: Service error - %v\n", i+1, err)
			}
		}
	}

	// 5. 测试半开状态
	fmt.Println("\n=== 测试半开状态 (等待熔断器超时) ===")
	time.Sleep(6 * time.Second) // 等待超过 OpenTimeout

	// 发送探测请求
	for i := 0; i < 3; i++ {
		err := b.Execute(ctx, "user.v1.UserService", func() error {
			fmt.Printf("Probe request %d: Success\n", i+1)
			return nil
		})
		if err != nil {
			fmt.Printf("Probe request %d: Error - %v\n", i+1, err)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// 6. 测试降级逻辑
	fmt.Println("\n=== 测试降级逻辑 ===")
	err = b.ExecuteWithFallback(ctx, "user.v1.UserService",
		func() error {
			fmt.Println("Primary function: Simulating failure")
			return fmt.Errorf("service unavailable")
		},
		func(err error) error {
			fmt.Printf("Fallback executed due to: %v\n", err)
			fmt.Println("Returning cached data")
			return nil // 降级成功
		},
	)
	if err != nil {
		fmt.Printf("Fallback error: %v\n", err)
	} else {
		fmt.Println("Fallback succeeded")
	}

	// 7. 手动重置
	fmt.Println("\n=== 测试手动重置 ===")
	b.Reset("user.v1.UserService")
	fmt.Println("Circuit breaker reset to CLOSED")

	// 8. 最终状态检查
	fmt.Printf("\n=== 最终状态 ===\n")
	fmt.Printf("user.v1.UserService state: %s\n", b.State("user.v1.UserService"))
}
