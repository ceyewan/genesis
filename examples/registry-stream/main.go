package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	pb "github.com/ceyewan/genesis/examples/breaker/proto"
	"github.com/ceyewan/genesis/registry"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	fmt.Println("=== Registry StreamManager 示例 ===")
	fmt.Println("注意：此示例需要运行 etcd 服务")
	fmt.Println()

	logger, _ := clog.New(&clog.Config{
		Level:  "info",
		Format: "console",
		Output: "stdout",
	})

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

	reg, err := registry.New(etcdConn, &registry.Config{
		Namespace:     "/genesis/services",
		DefaultTTL:    30 * time.Second,
		RetryInterval: 1 * time.Second,
	}, registry.WithLogger(logger))
	if err != nil {
		logger.Error("failed to create registry", clog.Error(err))
		return
	}
	defer reg.Close()

	serviceName := "stream-service"

	server1, addr1, err := startStreamServer("server-1", logger)
	if err != nil {
		logger.Error("failed to start server 1", clog.Error(err))
		return
	}
	defer server1.Stop()

	server2, addr2, err := startStreamServer("server-2", logger)
	if err != nil {
		logger.Error("failed to start server 2", clog.Error(err))
		return
	}
	defer server2.Stop()

	instance1 := &registry.ServiceInstance{
		ID:        "stream-1",
		Name:      serviceName,
		Version:   "1.0.0",
		Endpoints: []string{fmt.Sprintf("grpc://%s:%d", addr1.IP.String(), addr1.Port)},
	}
	instance2 := &registry.ServiceInstance{
		ID:        "stream-2",
		Name:      serviceName,
		Version:   "1.0.0",
		Endpoints: []string{fmt.Sprintf("grpc://%s:%d", addr2.IP.String(), addr2.Port)},
	}

	if err := reg.Register(ctx, instance1, 30*time.Second); err != nil {
		logger.Error("failed to register instance 1", clog.Error(err))
		return
	}
	if err := reg.Register(ctx, instance2, 30*time.Second); err != nil {
		logger.Error("failed to register instance 2", clog.Error(err))
		return
	}
	defer reg.Deregister(ctx, instance1.ID)
	defer reg.Deregister(ctx, instance2.ID)

	manager, err := registry.NewStreamManager(reg, registry.StreamManagerConfig{
		ServiceName: serviceName,
		DialOptions: []grpc.DialOption{
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		},
		Factory: func(ctx context.Context, conn *grpc.ClientConn, instance *registry.ServiceInstance) (grpc.ClientStream, error) {
			client := pb.NewTestServiceClient(conn)
			return client.StreamCall(ctx)
		},
		Handler: &streamLogger{logger: logger},
	}, registry.WithStreamLogger(logger))
	if err != nil {
		logger.Error("failed to create stream manager", clog.Error(err))
		return
	}
	defer manager.Stop(ctx)

	if err := manager.Start(ctx); err != nil {
		logger.Error("failed to start stream manager", clog.Error(err))
		return
	}

	if err := waitForStreams(ctx, manager, 2); err != nil {
		logger.Error("streams not ready", clog.Error(err))
		return
	}

	fmt.Println("开始向每个实例发送一条消息...")
	for id, stream := range manager.Streams() {
		clientStream, ok := stream.(pb.TestService_StreamCallClient)
		if !ok {
			logger.Error("stream type mismatch", clog.String("instance_id", id))
			continue
		}

		req := &pb.StreamRequest{Message: fmt.Sprintf("hello-%s", id)}
		if err := clientStream.Send(req); err != nil {
			logger.Error("failed to send stream request", clog.String("instance_id", id), clog.Error(err))
			continue
		}

		resp, err := clientStream.Recv()
		if err != nil {
			logger.Error("failed to receive stream response", clog.String("instance_id", id), clog.Error(err))
			continue
		}

		fmt.Printf("✓ 实例 %s 响应: %s\n", id, resp.Message)
	}

	fmt.Println("模拟下线一个实例...")
	_ = reg.Deregister(ctx, instance1.ID)
	server1.Stop()

	if err := waitForStreams(ctx, manager, 1); err != nil {
		logger.Error("stream remove not observed", clog.Error(err))
		return
	}

	fmt.Println("=== 示例完成 ===")
}

type streamLogger struct {
	logger clog.Logger
}

func (s *streamLogger) OnAdd(instance *registry.ServiceInstance, _ grpc.ClientStream) {
	s.logger.Info("stream added",
		clog.String("service_name", instance.Name),
		clog.String("service_id", instance.ID))
}

func (s *streamLogger) OnRemove(instance *registry.ServiceInstance) {
	s.logger.Info("stream removed",
		clog.String("service_name", instance.Name),
		clog.String("service_id", instance.ID))
}

func (s *streamLogger) OnError(instance *registry.ServiceInstance, err error) {
	s.logger.Error("stream error",
		clog.String("service_name", instance.Name),
		clog.String("service_id", instance.ID),
		clog.Error(err))
}

func waitForStreams(ctx context.Context, manager *registry.StreamManager, count int) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if len(manager.Streams()) == count {
				return nil
			}
		}
	}
}

func startStreamServer(serverID string, logger clog.Logger) (*grpc.Server, *net.TCPAddr, error) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, err
	}

	server := grpc.NewServer()
	addr := lis.Addr().(*net.TCPAddr)
	pb.RegisterTestServiceServer(server, &streamServer{serverID: serverID, logger: logger})

	go func() {
		if err := server.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			logger.Error("stream server exited",
				clog.String("server_id", serverID),
				clog.Error(err))
		}
	}()

	time.Sleep(100 * time.Millisecond)
	logger.Info("stream server started",
		clog.String("server_id", serverID),
		clog.String("addr", addr.String()))

	return server, addr, nil
}

type streamServer struct {
	pb.UnimplementedTestServiceServer
	serverID string
	logger   clog.Logger
}

func (s *streamServer) StreamCall(stream pb.TestService_StreamCallServer) error {
	for {
		req, err := stream.Recv()
		if err != nil {
			return err
		}
		resp := &pb.StreamResponse{
			Message:   fmt.Sprintf("[%s] %s", s.serverID, req.Message),
			Timestamp: time.Now().Unix(),
		}
		if err := stream.Send(resp); err != nil {
			return err
		}
	}
}
