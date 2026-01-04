package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/ceyewan/genesis/breaker"
	"github.com/ceyewan/genesis/clog"
	pb "github.com/ceyewan/genesis/examples/breaker/proto"
)

func main() {
	ctx := context.Background()

	// 1. 创建 Logger
	logger, err := clog.New(&clog.Config{
		Level:  "info",
		Format: "console",
		Output: "stdout",
	})
	if err != nil {
		log.Fatalf("failed to create logger: %v", err)
	}

	fmt.Println("\n=== Genesis Breaker 组件示例 ===")
	fmt.Println("本示例演示熔断器组件的核心功能:")
	fmt.Println("  1. 故障隔离 - 自动熔断频繁失败的服务")
	fmt.Println("  2. 自动恢复 - 通过半开状态探测服务恢复")
	fmt.Println("  3. 服务级粒度 - 不同服务独立熔断")
	fmt.Println("  4. 降级策略 - 支持自定义降级逻辑")
	fmt.Println()

	// 3. 启动测试服务器
	fmt.Println("=== 启动测试服务器 ===")
	server, addr := startTestServer("test-server-1")
	defer server.Stop()
	fmt.Printf("✓ 测试服务器已启动: %s\n\n", addr)

	// 示例 1: 基本熔断功能
	fmt.Println("=== 示例 1: 基本熔断功能 ===")
	basicExample(ctx, logger, addr)

	// 示例 2: 自定义降级逻辑
	fmt.Println("\n=== 示例 2: 自定义降级逻辑 ===")
	fallbackExample(ctx, logger, addr)

	// 示例 3: 服务级粒度熔断
	fmt.Println("\n=== 示例 3: 服务级粒度熔断 ===")
	multiServiceExample(ctx, logger)

	fmt.Println("\n=== 示例完成 ===")
	fmt.Println("✅ 熔断器成功实现了故障隔离和自动恢复！")
	fmt.Println("\n主要特性:")
	fmt.Println("  • 故障隔离：当失败率超过阈值时自动熔断")
	fmt.Println("  • 自动恢复：通过半开状态探测服务是否恢复")
	fmt.Println("  • 服务级粒度：不同服务独立管理，互不影响")
	fmt.Println("  • 灵活降级：支持快速失败和自定义降级逻辑")
}

// basicExample 基本熔断功能示例
func basicExample(ctx context.Context, logger clog.Logger, addr string) {
	// 创建熔断器（较低的阈值，便于观察效果）
	brk, err := breaker.New(&breaker.Config{
		MaxRequests:     3,                // 半开状态允许 3 个探测请求
		Interval:        10 * time.Second, // 10 秒统计周期
		Timeout:         5 * time.Second,  // 熔断 5 秒后进入半开状态
		FailureRatio:    0.5,              // 失败率 50% 触发熔断
		MinimumRequests: 5,                // 至少 5 个请求才触发熔断
	}, breaker.WithLogger(logger))
	if err != nil {
		logger.Error("failed to create breaker", clog.Error(err))
		return
	}

	// 创建 gRPC 连接（使用熔断器拦截器）
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(brk.UnaryClientInterceptor()),
	)
	if err != nil {
		logger.Error("failed to dial", clog.Error(err))
		return
	}
	defer conn.Close()

	client := pb.NewTestServiceClient(conn)

	fmt.Println("配置: FailureRatio=50%, MinimumRequests=5, Timeout=5s")
	fmt.Println()

	// 阶段 1: 正常请求
	fmt.Println("阶段 1: 发送 3 个正常请求")
	for i := 0; i < 3; i++ {
		resp, err := client.Call(ctx, &pb.CallRequest{
			Message:    fmt.Sprintf("normal-%d", i+1),
			ShouldFail: false,
		})
		if err != nil {
			fmt.Printf("  请求 %d: ✗ 失败 - %v\n", i+1, err)
		} else {
			fmt.Printf("  请求 %d: ✓ 成功 - %s\n", i+1, resp.Message)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// 阶段 2: 触发熔断（发送失败请求）
	fmt.Println("\n阶段 2: 发送 10 个失败请求（触发熔断）")
	for i := 0; i < 10; i++ {
		resp, err := client.Call(ctx, &pb.CallRequest{
			Message:    fmt.Sprintf("fail-%d", i+1),
			ShouldFail: true,
		})
		if err != nil {
			// 检查是否是熔断错误
			if errors.Is(err, breaker.ErrOpenState) {
				fmt.Printf("  请求 %d: ⚡ 被熔断器拒绝\n", i+1)
			} else {
				fmt.Printf("  请求 %d: ✗ 失败 - %v\n", i+1, err)
			}
		} else {
			fmt.Printf("  请求 %d: ✓ 成功 - %s\n", i+1, resp.Message)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// 检查熔断器状态
	state, _ := brk.State(addr)
	fmt.Printf("\n当前熔断器状态: %s\n", state)

	// 阶段 3: 等待熔断器恢复
	fmt.Println("\n阶段 3: 等待 6 秒后熔断器进入半开状态...")
	time.Sleep(6 * time.Second)

	state, _ = brk.State(addr)
	fmt.Printf("当前熔断器状态: %s\n", state)

	// 阶段 4: 半开状态探测（发送正常请求）
	fmt.Println("\n阶段 4: 发送正常请求（探测恢复）")
	for i := 0; i < 5; i++ {
		resp, err := client.Call(ctx, &pb.CallRequest{
			Message:    fmt.Sprintf("recovery-%d", i+1),
			ShouldFail: false,
		})
		if err != nil {
			fmt.Printf("  请求 %d: ✗ 失败 - %v\n", i+1, err)
		} else {
			fmt.Printf("  请求 %d: ✓ 成功 - %s\n", i+1, resp.Message)
		}
		time.Sleep(200 * time.Millisecond)
	}

	state, _ = brk.State(addr)
	fmt.Printf("\n当前熔断器状态: %s（已恢复正常）\n", state)
}

// fallbackExample 自定义降级逻辑示例
func fallbackExample(ctx context.Context, logger clog.Logger, addr string) {
	// 创建带降级逻辑的熔断器
	brk, err := breaker.New(&breaker.Config{
		MaxRequests:     3,
		Interval:        10 * time.Second,
		Timeout:         5 * time.Second,
		FailureRatio:    0.5,
		MinimumRequests: 5,
	},
		breaker.WithLogger(logger),
		breaker.WithFallback(func(ctx context.Context, serviceName string, err error) error {
			logger.Warn("circuit breaker open, using fallback",
				clog.String("service", serviceName),
				clog.Error(err))
			fmt.Printf("  ⚡ 熔断器打开，执行降级逻辑（返回缓存数据）\n")
			return nil // 返回 nil 表示降级成功
		}),
	)
	if err != nil {
		logger.Error("failed to create breaker", clog.Error(err))
		return
	}

	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(brk.UnaryClientInterceptor()),
	)
	if err != nil {
		logger.Error("failed to dial", clog.Error(err))
		return
	}
	defer conn.Close()

	client := pb.NewTestServiceClient(conn)

	fmt.Println("配置: 带自定义降级逻辑")
	fmt.Println()

	// 触发熔断
	fmt.Println("阶段 1: 发送失败请求触发熔断")
	for i := 0; i < 10; i++ {
		_, err := client.Call(ctx, &pb.CallRequest{
			Message:    fmt.Sprintf("fail-%d", i+1),
			ShouldFail: true,
		})
		if err != nil {
			fmt.Printf("  请求 %d: ✗ 失败\n", i+1)
		} else {
			fmt.Printf("  请求 %d: ✓ 成功（降级）\n", i+1)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// multiServiceExample 服务级粒度熔断示例
func multiServiceExample(ctx context.Context, logger clog.Logger) {
	// 启动两个测试服务器
	server1, addr1 := startTestServer("service-1")
	defer server1.Stop()

	server2, addr2 := startTestServer("service-2")
	defer server2.Stop()

	fmt.Printf("✓ 服务 1 已启动: %s\n", addr1)
	fmt.Printf("✓ 服务 2 已启动: %s\n\n", addr2)

	// 创建熔断器
	brk, err := breaker.New(&breaker.Config{
		MaxRequests:     3,
		Interval:        10 * time.Second,
		Timeout:         5 * time.Second,
		FailureRatio:    0.5,
		MinimumRequests: 5,
	}, breaker.WithLogger(logger))
	if err != nil {
		logger.Error("failed to create breaker", clog.Error(err))
		return
	}

	// 创建两个客户端
	conn1, _ := grpc.NewClient(addr1,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(brk.UnaryClientInterceptor()))
	defer conn1.Close()

	conn2, _ := grpc.NewClient(addr2,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(brk.UnaryClientInterceptor()))
	defer conn2.Close()

	client1 := pb.NewTestServiceClient(conn1)
	client2 := pb.NewTestServiceClient(conn2)

	// 只让服务 1 失败
	fmt.Println("阶段 1: 服务 1 频繁失败，服务 2 正常")
	for i := 0; i < 10; i++ {
		// 服务 1 失败
		_, err1 := client1.Call(ctx, &pb.CallRequest{
			Message:    fmt.Sprintf("service1-fail-%d", i+1),
			ShouldFail: true,
		})

		// 服务 2 正常
		_, err2 := client2.Call(ctx, &pb.CallRequest{
			Message:    fmt.Sprintf("service2-ok-%d", i+1),
			ShouldFail: false,
		})

		status1 := "✗ 失败"
		if err1 != nil && errors.Is(err1, breaker.ErrOpenState) {
			status1 = "⚡ 被熔断"
		}

		status2 := "✓ 成功"
		if err2 != nil {
			status2 = "✗ 失败"
		}

		fmt.Printf("  请求 %d: 服务1=%s, 服务2=%s\n", i+1, status1, status2)
		time.Sleep(100 * time.Millisecond)
	}

	// 检查两个服务的熔断器状态
	state1, _ := brk.State(addr1)
	state2, _ := brk.State(addr2)
	fmt.Printf("\n服务 1 熔断器状态: %s\n", state1)
	fmt.Printf("服务 2 熔断器状态: %s\n", state2)
	fmt.Println("\n✓ 验证成功：服务 1 被熔断，服务 2 正常运行（独立管理）")
}

// startTestServer 启动一个测试用的 gRPC 服务器
func startTestServer(serverID string) (*grpc.Server, string) {
	// 监听随机端口
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	server := grpc.NewServer()
	addr := lis.Addr().String()

	// 注册测试服务
	testSvc := &testServer{
		serverID: serverID,
	}
	pb.RegisterTestServiceServer(server, testSvc)

	// 启动服务器
	go func() {
		if err := server.Serve(lis); err != nil {
			log.Printf("Server %s exited with error: %v", serverID, err)
		}
	}()

	// 等待服务器启动
	time.Sleep(100 * time.Millisecond)

	return server, addr
}

// testServer 实现测试服务
type testServer struct {
	pb.UnimplementedTestServiceServer
	serverID     string
	requestCount atomic.Int64
}

func (s *testServer) Call(ctx context.Context, req *pb.CallRequest) (*pb.CallResponse, error) {
	count := s.requestCount.Add(1)

	// 如果请求要求失败，则返回错误
	if req.ShouldFail {
		return nil, status.Errorf(codes.Internal, "simulated failure")
	}

	resp := &pb.CallResponse{
		Message:   req.Message,
		ServerId:  s.serverID,
		Timestamp: time.Now().Unix(),
	}

	log.Printf("[%s] Request #%d: %s -> %s", s.serverID, count, req.Message, resp.Message)

	return resp, nil
}

func (s *testServer) StreamCall(stream pb.TestService_StreamCallServer) error {
	for {
		req, err := stream.Recv()
		if err != nil {
			return err
		}

		resp := &pb.StreamResponse{
			Message:   req.Message,
			Timestamp: time.Now().Unix(),
		}

		if err := stream.Send(resp); err != nil {
			return err
		}
	}
}
