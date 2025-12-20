package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/ceyewan/genesis/breaker"
	"github.com/ceyewan/genesis/breaker/adapter"
	"github.com/ceyewan/genesis/breaker/types"
	"github.com/ceyewan/genesis/clog"
)

// 模拟的 gRPC 服务定义（实际使用时需要导入真实的 protobuf 定义）
type UserServiceClient interface {
	GetUser(ctx context.Context, req *GetUserRequest, opts ...grpc.CallOption) (*GetUserResponse, error)
}

type GetUserRequest struct {
	Id string
}

type GetUserResponse struct {
	Id   string
	Name string
}

// 模拟的 gRPC 客户端实现
type mockUserServiceClient struct {
	conn *grpc.ClientConn
}

func (c *mockUserServiceClient) GetUser(ctx context.Context, req *GetUserRequest, opts ...grpc.CallOption) (*GetUserResponse, error) {
	// 模拟服务调用失败
	return nil, fmt.Errorf("service unavailable")
}

func runGRPCExample() {
	// 1. 创建 Logger
	logger, err := clog.New(&clog.Config{
		Level:  "info",
		Format: "json",
	}, nil)
	if err != nil {
		log.Fatal("Failed to create logger:", err)
	}

	// 2. 创建熔断器
	b, err := breaker.New(&types.Config{
		Default: types.DefaultPolicy(),
	}, breaker.WithLogger(logger))

	if err != nil {
		log.Fatal("Failed to create breaker:", err)
	}

	// 3. 创建 gRPC 连接，集成熔断拦截器
	conn, err := grpc.Dial(
		"localhost:9090", // 假设的服务地址
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(
			adapter.UnaryClientInterceptor(b,
				// 可选：添加降级处理
				adapter.WithFallbackHandler(func(ctx context.Context, method string, err error) error {
					logger.Warn("circuit breaker triggered, using fallback",
						clog.String("method", method),
						clog.Error(err))

					// 根据方法返回不同的降级数据
					switch {
					case contains(method, "GetUser"):
						fmt.Println("Returning cached user data")
						return nil // 模拟返回缓存数据
					case contains(method, "ListOrders"):
						fmt.Println("Returning empty order list")
						return nil // 模拟返回空列表
					default:
						return err
					}
				}),
			),
		),
	)
	if err != nil {
		log.Fatal("Failed to create gRPC connection:", err)
	}
	defer conn.Close()

	// 4. 创建 gRPC 客户端
	client := &mockUserServiceClient{conn: conn}

	// 5. 测试正常调用（会被熔断器保护）
	ctx := context.Background()

	fmt.Println("=== 测试 gRPC 调用保护 ===")

	// 模拟多次失败调用以触发熔断
	for i := 0; i < 15; i++ {
		resp, err := client.GetUser(ctx, &GetUserRequest{Id: "123"})
		if err != nil {
			fmt.Printf("Call %d: Error - %v\n", i+1, err)
		} else {
			fmt.Printf("Call %d: Success - User: %s\n", i+1, resp.Name)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// 6. 等待熔断器进入半开状态
	fmt.Println("\n=== 等待熔断器超时 ===")
	time.Sleep(35 * time.Second) // 等待超过默认的 30s OpenTimeout

	// 7. 测试半开状态
	fmt.Println("=== 测试半开状态 ===")
	for i := 0; i < 5; i++ {
		resp, err := client.GetUser(ctx, &GetUserRequest{Id: "123"})
		if err != nil {
			fmt.Printf("Probe call %d: Error - %v\n", i+1, err)
		} else {
			fmt.Printf("Probe call %d: Success - User: %s\n", i+1, resp.Name)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// contains 检查字符串是否包含子串
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(s) > len(substr) && (s[:len(substr)] == substr ||
			s[len(s)-len(substr):] == substr ||
			containsSubstring(s, substr))))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
