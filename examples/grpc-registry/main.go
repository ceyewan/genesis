package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	pb "github.com/ceyewan/genesis/examples/grpc-registry/proto"
	"github.com/ceyewan/genesis/registry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// 演示 gRPC Resolver 动态服务发现功能
func main() {
	fmt.Println("=== gRPC Resolver 动态服务发现演示 ===")
	fmt.Println("注意：此演示需要运行 etcd 服务")
	fmt.Println("启动命令：etcd --listen-client-urls=http://localhost:2379 --advertise-client-urls=http://localhost:2379")
	fmt.Println()

	// 1. 创建 Logger
	logger, _ := clog.New(&clog.Config{
		Level:       "info",
		Format:      "console",
		Output:      "stdout",
		EnableColor: true,
		AddSource:   true,
		SourceRoot:  "genesis",
	})

	// 2. 创建 Etcd 连接器
	etcdConn, err := connector.NewEtcd(&connector.EtcdConfig{
		Endpoints: []string{"localhost:2379"},
	}, connector.WithLogger(logger))
	if err != nil {
		logger.Error("failed to create etcd connector", clog.Error(err))
		return
	}
	defer etcdConn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := etcdConn.Connect(ctx); err != nil {
		logger.Error("failed to connect etcd", clog.Error(err))
		return
	}

	// 3. 创建 Registry 实例
	reg, err := registry.New(etcdConn, &registry.Config{
		Namespace:     "/genesis/services",
		DefaultTTL:    30 * time.Second,
		RetryInterval: 1 * time.Second,
	}, registry.WithLogger(logger))
	if err != nil {
		logger.Error("failed to create registry", clog.Error(err))
		return
	}

	// 4. 延迟关闭 Registry
	defer reg.Close()

	serviceName := "demo-service"

	// 5. 启动第一个服务实例
	fmt.Println("1. 启动第一个服务实例...")
	server1, addr1, err := startTestServer("server-1", logger)
	if err != nil {
		logger.Error("failed to start server 1", clog.Error(err))
		return
	}
	defer server1.Stop()

	service1 := &registry.ServiceInstance{
		ID:        "demo-service-1",
		Name:      serviceName,
		Version:   "1.0.0",
		Endpoints: []string{fmt.Sprintf("grpc://%s:%d", addr1.IP.String(), addr1.Port)},
		Metadata: map[string]string{
			"version": "1.0.0",
			"server":  "server-1",
		},
	}

	err = reg.Register(ctx, service1, 30*time.Second)
	if err != nil {
		logger.Error("failed to register service 1", clog.Error(err))
		return
	}
	defer reg.Deregister(ctx, service1.ID)

	fmt.Printf("✓ 服务实例 1 已注册: %s\n", service1.Endpoints[0])

	// 等待服务注册生效
	time.Sleep(500 * time.Millisecond)

	// 6. 使用 gRPC resolver 创建连接
	fmt.Println("\n2. 使用 gRPC resolver 创建连接...")
	conn, err := reg.GetConnection(ctx, serviceName)
	if err != nil {
		logger.Error("failed to create gRPC connection", clog.Error(err))
		return
	}
	defer conn.Close()

	echoClient := pb.NewEchoServiceClient(conn)
	healthClient := grpc_health_v1.NewHealthClient(conn)
	fmt.Println("✓ gRPC 连接已建立，使用动态服务发现")

	// 7. 测试连接
	fmt.Println("\n3. 测试连接...")
	for i := 0; i < 3; i++ {
		resp, err := healthClient.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
		if err != nil {
			logger.Error("health check failed", clog.Error(err))
		} else {
			state := conn.GetState()
			fmt.Printf("✓ 健康检查 %d: %v (连接状态: %v)\n", i+1, resp.Status, state)
		}
		time.Sleep(200 * time.Millisecond)
	}

	// 调用 Echo 服务查看服务器信息
	fmt.Println("\n调用 Echo 服务查看服务器信息:")
	for i := 0; i < 3; i++ {
		echoResp, err := echoClient.Echo(ctx, &pb.EchoRequest{
			Message: fmt.Sprintf("test-%d", i+1),
		})
		if err != nil {
			fmt.Printf("✗ Echo 调用 %d 失败: %v\n", i+1, err)
		} else {
			fmt.Printf("✓ Echo 调用 %d: 消息='%s', 服务器='%s', 地址='%s'\n",
				i+1, echoResp.Message, echoResp.ServerId, echoResp.ServerAddr)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// 8. 动态添加第二个服务实例
	fmt.Println("\n4. 动态添加第二个服务实例...")
	server2, addr2, err := startTestServer("server-2", logger)
	if err != nil {
		logger.Error("failed to start server 2", clog.Error(err))
		return
	}
	defer server2.Stop()

	service2 := &registry.ServiceInstance{
		ID:        "demo-service-2",
		Name:      serviceName,
		Version:   "1.0.1",
		Endpoints: []string{fmt.Sprintf("grpc://%s:%d", addr2.IP.String(), addr2.Port)},
		Metadata: map[string]string{
			"version": "1.0.1",
			"server":  "server-2",
		},
	}

	err = reg.Register(ctx, service2, 30*time.Second)
	if err != nil {
		logger.Error("failed to register service 2", clog.Error(err))
		return
	}
	defer reg.Deregister(ctx, service2.ID)

	fmt.Printf("✓ 服务实例 2 已注册: %s\n", service2.Endpoints[0])

	// 等待 resolver 检测到新服务
	fmt.Println("⏳ 等待 resolver 检测到新服务...")
	time.Sleep(1 * time.Second)

	// 9. 验证负载均衡
	fmt.Println("\n5. 验证负载均衡（现在有两个服务实例）...")
	fmt.Printf("已注册的服务实例:\n")
	services, _ := reg.GetService(ctx, serviceName)
	for i, svc := range services {
		fmt.Printf("  %d. ID: %s, Endpoints: %v, Server: %s\n", i+1, svc.ID, svc.Endpoints, svc.Metadata["server"])
	}
	fmt.Println()

	for i := 0; i < 6; i++ {
		// 调用 Echo 服务来观察负载均衡效果
		echoResp, err := echoClient.Echo(ctx, &pb.EchoRequest{
			Message: fmt.Sprintf("loadbalancer-test-%d", i+1),
		})
		if err != nil {
			fmt.Printf("✗ 负载均衡测试 %d 失败: %v\n", i+1, err)
		} else {
			fmt.Printf("✓ 负载均衡测试 %d: 消息='%s', 服务器='%s', 地址='%s'\n",
				i+1, echoResp.Message, echoResp.ServerId, echoResp.ServerAddr)
		}
		time.Sleep(200 * time.Millisecond)
	}

	// 10. 移除第一个服务实例
	fmt.Println("\n6. 移除第一个服务实例...")
	err = reg.Deregister(ctx, service1.ID)
	if err != nil {
		logger.Error("failed to unregister service 1", clog.Error(err))
	} else {
		fmt.Println("✓ 服务实例 1 已移除")
	}

	// 等待 resolver 检测到服务移除
	fmt.Println("⏳ 等待 resolver 检测到服务移除...")
	time.Sleep(1 * time.Second)

	// 11. 验证连接仍然可用
	fmt.Println("\n7. 验证连接仍然可用（只剩一个服务实例）...")
	for i := 0; i < 3; i++ {
		echoResp, err := echoClient.Echo(ctx, &pb.EchoRequest{
			Message: fmt.Sprintf("failover-test-%d", i+1),
		})
		if err != nil {
			fmt.Printf("✗ 故障转移测试 %d 失败: %v\n", i+1, err)
		} else {
			fmt.Printf("✓ 故障转移测试 %d: 消息='%s', 服务器='%s', 地址='%s'\n",
				i+1, echoResp.Message, echoResp.ServerId, echoResp.ServerAddr)
		}
		time.Sleep(200 * time.Millisecond)
	}

	// 12. 验证服务发现
	fmt.Println("\n8. 验证服务发现...")
	services, err = reg.GetService(ctx, serviceName)
	if err != nil {
		logger.Error("failed to discover services", clog.Error(err))
	} else {
		fmt.Printf("✓ 发现 %d 个服务实例:\n", len(services))
		for _, svc := range services {
			fmt.Printf("  - ID: %s, Endpoints: %v, Version: %s\n",
				svc.ID, svc.Endpoints, svc.Metadata["version"])
		}
	}

	fmt.Println("\n=== 演示完成 ===")
	fmt.Println("✅ gRPC resolver 成功实现了动态服务发现和负载均衡！")
	fmt.Println("\n主要特性:")
	fmt.Println("  • 动态服务发现：自动检测新增的服务实例")
	fmt.Println("  • 负载均衡：使用 round_robin 策略分发请求")
	fmt.Println("  • 故障转移：服务实例下线时自动切换到可用实例")
	fmt.Println("  • 实时更新：通过 etcd watch 机制实时感知服务变化")
}

// startTestServer 启动一个测试用的 gRPC 服务器
func startTestServer(serverID string, logger clog.Logger) (*grpc.Server, *net.TCPAddr, error) {
	// 监听随机端口
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, err
	}

	server := grpc.NewServer()
	addr := lis.Addr().(*net.TCPAddr)

	// 注册 Echo 服务
	pb.RegisterEchoServiceServer(server, &echoServer{
		serverID:   serverID,
		serverAddr: addr.String(),
		logger:     logger,
	})

	// 注册健康检查服务
	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(server, healthServer)
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

	// 启动服务器
	go func() {
		if err := server.Serve(lis); err != nil {
			if !errors.Is(err, grpc.ErrServerStopped) {
				logger.Error("server exited with error",
					clog.String("server_id", serverID),
					clog.Error(err))
			}
		}
	}()

	// 等待服务器启动
	time.Sleep(100 * time.Millisecond)

	logger.Info("test server started",
		clog.String("server_id", serverID),
		clog.String("addr", addr.String()))

	return server, addr, nil
}

// echoServer 实现 Echo 服务
type echoServer struct {
	pb.UnimplementedEchoServiceServer
	serverID   string
	serverAddr string
	logger     clog.Logger
}

func (s *echoServer) Echo(ctx context.Context, req *pb.EchoRequest) (*pb.EchoResponse, error) {
	s.logger.Info("echo request received",
		clog.String("server_id", s.serverID),
		clog.String("message", req.Message))

	resp := &pb.EchoResponse{
		Message:    req.Message,
		ServerId:   s.serverID,
		ServerAddr: s.serverAddr,
		Timestamp:  time.Now().Unix(),
	}

	s.logger.Info("echo response sent",
		clog.String("server_id", s.serverID),
		clog.String("message", resp.Message),
		clog.String("addr", resp.ServerAddr))

	return resp, nil
}
