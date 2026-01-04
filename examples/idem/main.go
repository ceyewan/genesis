package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	pb "github.com/ceyewan/genesis/examples/idem/proto"
	"github.com/ceyewan/genesis/idem"
)

func main() {
	fmt.Println("\n=== Genesis 幂等性组件示例 ===")
	fmt.Println("本示例演示幂等性组件的核心功能:")
	fmt.Println("  1. HTTP 中间件 - Gin 框架集成")
	fmt.Println("  2. gRPC 一元拦截器 - 单次调用")
	fmt.Println("  3. 分布式幂等性 - Redis 存储")
	fmt.Println()

	// 1. 创建 Logger
	appLogger, err := clog.New(&clog.Config{
		Level:  "info",
		Format: "console",
		Output: "stdout",
	})
	if err != nil {
		log.Fatalf("创建 Logger 失败: %v", err)
	}

	// 2. 创建 Redis 连接器
	redisConn, err := connector.NewRedis(&connector.RedisConfig{
		Addr:         "127.0.0.1:6379",
		Password:     "genesis_redis_password",
		DB:           0,
		PoolSize:     10,
		MinIdleConns: 5,
		DialTimeout:  5 * time.Second,
	}, connector.WithLogger(appLogger))
	if err != nil {
		fmt.Printf("创建 Redis 连接器失败: %v\n", err)
		return
	}
	defer redisConn.Close()

	// 3. 创建幂等性组件
	idem, err := idem.New(&idem.Config{
		Driver:     idem.DriverRedis,
		Prefix:     "example:idem:",
		DefaultTTL: 24 * time.Hour,
		LockTTL:    30 * time.Second,
	}, idem.WithRedisConnector(redisConn), idem.WithLogger(appLogger))
	if err != nil {
		fmt.Printf("创建幂等性组件失败: %v\n", err)
		return
	}

	// 4. 启动 gRPC 服务器
	fmt.Println("=== 启动 gRPC 服务器 ===")
	grpcServer, grpcAddr := startGRPCServer(appLogger, idem)
	defer grpcServer.Stop()
	fmt.Printf("✓ gRPC 服务器已启动: %s\n\n", grpcAddr)

	// 5. 启动 HTTP 服务器
	fmt.Println("=== 启动 HTTP 服务器 ===")
	httpServer := startHTTPServer(appLogger, idem)
	defer httpServer.Close()
	fmt.Println("✓ HTTP 服务器已启动: http://localhost:8080")
	fmt.Println()

	// 6. 演示 HTTP 中间件
	fmt.Println("=== 示例 1: HTTP 中间件（Gin）===")
	demonstrateHTTP()

	// 7. 演示 gRPC 一元拦截器
	fmt.Println("\n=== 示例 2: gRPC 一元拦截器 ===")
	demonstrateGRPCUnary(grpcAddr)

	fmt.Println("\n=== 示例完成 ===")
	fmt.Println("✅ 幂等性组件成功演示了所有功能！")
	fmt.Println("\n主要特性:")
	fmt.Println("  • HTTP 中间件：自动从 X-Idempotency-Key 头提取幂等性键")
	fmt.Println("  • gRPC 一元拦截器：支持单次 RPC 调用的幂等性")
	fmt.Println("  • 分布式锁：使用 Redis 实现分布式锁，防止并发执行")
	fmt.Println("  • 结果缓存：缓存执行结果，重复请求直接返回缓存")
	fmt.Println("  • 灵活配置：支持自定义 TTL、锁超时等参数")
}

// startGRPCServer 启动 gRPC 服务器
func startGRPCServer(logger clog.Logger, idem idem.Idempotency) (*grpc.Server, string) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	server := grpc.NewServer(
		grpc.UnaryInterceptor(idem.UnaryServerInterceptor()),
	)

	svc := &orderServer{logger: logger}
	pb.RegisterOrderServiceServer(server, svc)

	go func() {
		if err := server.Serve(lis); err != nil {
			log.Printf("gRPC server exited: %v", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)
	return server, lis.Addr().String()
}

// startHTTPServer 启动 HTTP 服务器
func startHTTPServer(logger clog.Logger, idem idem.Idempotency) *http.Server {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()

	// 使用幂等性中间件
	// 注意：GinMiddleware() 返回 interface{}，但实际类型是 func(*gin.Context)
	// 可以直接传给 router，也可以显式转换为 func(*gin.Context)
	router.POST("/orders", idem.GinMiddleware().(func(*gin.Context)), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"order_id": "order-123",
			"status":   "created",
		})
	})

	server := &http.Server{
		Addr:    ":8080",
		Handler: router,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)
	return server
}

// demonstrateHTTP 演示 HTTP 中间件
func demonstrateHTTP() {
	time.Sleep(500 * time.Millisecond)

	fmt.Println("配置: 使用 X-Idempotency-Key 头")
	fmt.Println()

	// 第一次请求
	fmt.Println("第一次请求（创建订单）...")
	req, _ := http.NewRequest("POST", "http://localhost:8080/orders", nil)
	req.Header.Set("X-Idempotency-Key", "order-key-001")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("  ✗ 请求失败: %v\n", err)
		return
	}
	defer resp.Body.Close()
	fmt.Printf("  ✓ 第一次请求成功 (HTTP %d)\n", resp.StatusCode)

	// 第二次请求（相同的幂等性键）
	fmt.Println("\n第二次请求（相同的幂等性键，应该返回缓存）...")
	req2, _ := http.NewRequest("POST", "http://localhost:8080/orders", nil)
	req2.Header.Set("X-Idempotency-Key", "order-key-001")
	resp2, err := client.Do(req2)
	if err != nil {
		fmt.Printf("  ✗ 请求失败: %v\n", err)
		return
	}
	defer resp2.Body.Close()
	fmt.Printf("  ✓ 第二次请求成功 (HTTP %d，来自缓存)\n", resp2.StatusCode)
}

// demonstrateGRPCUnary 演示 gRPC 一元拦截器
func demonstrateGRPCUnary(addr string) {
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		fmt.Printf("连接失败: %v\n", err)
		return
	}
	defer conn.Close()

	client := pb.NewOrderServiceClient(conn)
	ctx := context.Background()

	fmt.Println("配置: 一元 RPC 调用")
	fmt.Println()

	// 第一次调用
	fmt.Println("第一次调用（创建订单）...")
	ctx1 := metadata.AppendToOutgoingContext(ctx, "x-idem-key", "grpc-order-001")
	resp1, err := client.CreateOrder(ctx1, &pb.CreateOrderRequest{
		IdempotencyKey: "grpc-order-001",
		OrderId:        "order-001",
		CustomerId:     "cust-001",
		Amount:         99.99,
		Description:    "Test Order",
	})
	if err != nil {
		fmt.Printf("  ✗ 调用失败: %v\n", err)
		return
	}
	fmt.Printf("  ✓ 第一次调用成功: %s\n", resp1.OrderId)

	// 第二次调用（相同的幂等性键）
	fmt.Println("\n第二次调用（相同的幂等性键，应该返回缓存）...")
	ctx2 := metadata.AppendToOutgoingContext(ctx, "x-idem-key", "grpc-order-001")
	resp2, err := client.CreateOrder(ctx2, &pb.CreateOrderRequest{
		IdempotencyKey: "grpc-order-001",
		OrderId:        "order-002",
		CustomerId:     "cust-002",
		Amount:         199.99,
		Description:    "Different Order",
	})
	if err != nil {
		fmt.Printf("  ✗ 调用失败: %v\n", err)
		return
	}
	fmt.Printf("  ✓ 第二次调用成功: %s\n", resp2.OrderId)
	fmt.Printf("  ✓ 验证：两次调用返回相同的订单 ID: %v\n", resp1.OrderId == resp2.OrderId)
}

// orderServer 实现 OrderService
type orderServer struct {
	pb.UnimplementedOrderServiceServer
	logger clog.Logger
}

// CreateOrder 创建订单（一元调用）
func (s *orderServer) CreateOrder(ctx context.Context, req *pb.CreateOrderRequest) (*pb.CreateOrderResponse, error) {
	resp := &pb.CreateOrderResponse{
		OrderId:   req.OrderId,
		Status:    "created",
		Amount:    req.Amount,
		Timestamp: time.Now().Unix(),
	}

	return resp, nil
}
