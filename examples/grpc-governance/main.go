package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/ceyewan/genesis/breaker"
	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/registry"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	logger, err := clog.New(&clog.Config{Level: "info", Format: "console", Output: "stdout"})
	if err != nil {
		log.Fatal(err)
	}

	etcdConn, err := connector.NewEtcd(&connector.EtcdConfig{Endpoints: []string{"127.0.0.1:2379"}}, connector.WithLogger(logger))
	if err != nil {
		log.Fatal(err)
	}
	defer etcdConn.Close()
	if err := etcdConn.Connect(ctx); err != nil {
		log.Fatalf("连接 Etcd 失败；请先执行 make up: %v", err)
	}
	reg, err := registry.New(etcdConn, &registry.Config{Namespace: "/genesis/scenarios", DefaultTTL: 15 * time.Second}, registry.WithLogger(logger))
	if err != nil {
		log.Fatal(err)
	}
	defer reg.Close()

	server, address := startHealthServer()
	defer server.Stop()
	instance := &registry.ServiceInstance{ID: "governance-inventory-1", Name: "governance-inventory", Version: "v1", Endpoints: []string{"grpc://" + address}}
	if err := reg.Register(ctx, instance, 15*time.Second); err != nil {
		log.Fatal(err)
	}
	defer reg.Deregister(context.Background(), instance.ID)

	brk, err := breaker.New(&breaker.Config{MinimumRequests: 2, FailureRatio: 0.5, Timeout: 2 * time.Second}, breaker.WithLogger(logger))
	if err != nil {
		log.Fatal(err)
	}
	conn, err := reg.GetConnection(ctx, instance.Name,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(brk.UnaryClientInterceptor()),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	client := grpc_health_v1.NewHealthClient(conn)
	if _, err := client.Check(ctx, &grpc_health_v1.HealthCheckRequest{}); err != nil {
		log.Fatal(err)
	}
	fmt.Println("服务发现调用成功")

	server.Stop()
	for range 3 {
		callCtx, callCancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		_, _ = client.Check(callCtx, &grpc_health_v1.HealthCheckRequest{})
		callCancel()
	}
	state, err := brk.State(conn.Target())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("实例停止后的熔断状态: %s\n", state)
}

func startHealthServer() (*grpc.Server, string) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	server := grpc.NewServer()
	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(server, healthServer)
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	go func() { _ = server.Serve(lis) }()
	return server, lis.Addr().String()
}
