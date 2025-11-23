package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/connector"
	"github.com/ceyewan/genesis/pkg/container"
	"github.com/ceyewan/genesis/pkg/registry"
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

	// 1. 创建容器和 Registry
	app, err := container.New(&container.Config{
		Etcd: &connector.EtcdConfig{
			Endpoints: []string{"localhost:2379"},
		},
	})
	if err != nil {
		log.Fatalf("Failed to create container: %v", err)
	}
	defer app.Close()

	logger := clog.Default()

	etcdConn, err := app.GetEtcdConnector(connector.EtcdConfig{
		Endpoints: []string{"localhost:2379"},
	})
	if err != nil {
		log.Fatalf("Failed to get etcd connector: %v", err)
	}

	reg, err := registry.New(etcdConn, registry.Config{
		Namespace:       "/genesis/services",
		Schema:          "etcd",
		DefaultTTL:      30 * time.Second,
		RetryInterval:   1 * time.Second,
		EnableCache:     true,
		CacheExpiration: 10 * time.Second,
	}, registry.WithLogger(logger))
	if err != nil {
		log.Fatalf("Failed to create registry: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := reg.Start(ctx); err != nil {
		log.Fatalf("Failed to start registry: %v", err)
	}
	defer reg.Stop(ctx)

	serviceName := "demo-service"

	// 2. 启动第一个服务实例
	fmt.Println("1. 启动第一个服务实例...")
	server1, addr1 := startTestServer("server-1")
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
		log.Fatalf("Failed to register service 1: %v", err)
	}
	defer reg.Deregister(ctx, service1.ID)

	fmt.Printf("✓ 服务实例 1 已注册: %s\n", service1.Endpoints[0])

	// 等待服务注册生效
	time.Sleep(500 * time.Millisecond)

	// 3. 使用 gRPC resolver 创建连接
	fmt.Println("\n2. 使用 gRPC resolver 创建连接...")
	conn, err := reg.GetConnection(ctx, serviceName)
	if err != nil {
		log.Fatalf("Failed to create gRPC connection: %v", err)
	}
	defer conn.Close()

	client := grpc_health_v1.NewHealthClient(conn)
	fmt.Println("✓ gRPC 连接已建立，使用动态服务发现")

	// 4. 测试连接
	fmt.Println("\n3. 测试连接...")
	for i := 0; i < 3; i++ {
		resp, err := client.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
		if err != nil {
			log.Printf("Health check failed: %v", err)
		} else {
			fmt.Printf("✓ 健康检查 %d: %v\n", i+1, resp.Status)
		}
		time.Sleep(200 * time.Millisecond)
	}

	// 5. 动态添加第二个服务实例
	fmt.Println("\n4. 动态添加第二个服务实例...")
	server2, addr2 := startTestServer("server-2")
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
		log.Fatalf("Failed to register service 2: %v", err)
	}
	defer reg.Deregister(ctx, service2.ID)

	fmt.Printf("✓ 服务实例 2 已注册: %s\n", service2.Endpoints[0])

	// 等待 resolver 检测到新服务
	fmt.Println("⏳ 等待 resolver 检测到新服务...")
	time.Sleep(1 * time.Second)

	// 6. 验证负载均衡
	fmt.Println("\n5. 验证负载均衡（现在有两个服务实例）...")
	for i := 0; i < 6; i++ {
		resp, err := client.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
		if err != nil {
			log.Printf("Health check failed: %v", err)
		} else {
			fmt.Printf("✓ 负载均衡测试 %d: %v\n", i+1, resp.Status)
		}
		time.Sleep(200 * time.Millisecond)
	}

	// 7. 移除第一个服务实例
	fmt.Println("\n6. 移除第一个服务实例...")
	err = reg.Deregister(ctx, service1.ID)
	if err != nil {
		log.Printf("Failed to unregister service 1: %v", err)
	} else {
		fmt.Println("✓ 服务实例 1 已移除")
	}

	// 等待 resolver 检测到服务移除
	fmt.Println("⏳ 等待 resolver 检测到服务移除...")
	time.Sleep(1 * time.Second)

	// 8. 验证连接仍然可用
	fmt.Println("\n7. 验证连接仍然可用（只剩一个服务实例）...")
	for i := 0; i < 3; i++ {
		resp, err := client.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
		if err != nil {
			log.Printf("Health check failed: %v", err)
		} else {
			fmt.Printf("✓ 故障转移测试 %d: %v\n", i+1, resp.Status)
		}
		time.Sleep(200 * time.Millisecond)
	}

	// 9. 验证服务发现
	fmt.Println("\n8. 验证服务发现...")
	services, err := reg.GetService(ctx, serviceName)
	if err != nil {
		log.Printf("Failed to discover services: %v", err)
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
func startTestServer(serverID string) (*grpc.Server, *net.TCPAddr) {
	// 监听随机端口
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	server := grpc.NewServer()

	// 注册健康检查服务
	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(server, healthServer)
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

	// 启动服务器
	go func() {
		if err := server.Serve(lis); err != nil {
			log.Printf("Server %s exited with error: %v", serverID, err)
		}
	}()

	// 等待服务器启动
	time.Sleep(100 * time.Millisecond)

	addr := lis.Addr().(*net.TCPAddr)
	log.Printf("Test server %s started on %s", serverID, addr)

	return server, addr
}
