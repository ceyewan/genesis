package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"net"
	"os"
	"slices"
	"strings"
	"time"

	"google.golang.org/grpc"
	_ "google.golang.org/grpc/balancer/roundrobin"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"

	"github.com/ceyewan/genesis/breaker"
	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/registry"
	"github.com/ceyewan/genesis/xerrors"
)

type healthInstance struct {
	grpc_health_v1.UnimplementedHealthServer
	id string
}

func (s *healthInstance) Check(ctx context.Context, _ *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	if err := grpc.SetHeader(ctx, metadata.Pairs("instance-id", s.id)); err != nil {
		return nil, err
	}
	return &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_SERVING}, nil
}

func startHealthServer(id string, logger clog.Logger) (*grpc.Server, string, error) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", err
	}
	server := grpc.NewServer()
	grpc_health_v1.RegisterHealthServer(server, &healthInstance{id: id})
	go func() {
		if err := server.Serve(lis); err != nil {
			logger.Error("Health server failed", clog.Error(err))
		}
	}()
	return server, lis.Addr().String(), nil
}

func waitEvent(ctx context.Context, events <-chan registry.ServiceEvent, id string, kind registry.EventType) error {
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return xerrors.New("registry watch closed before expected event")
			}
			if event.Service.ID == id && event.Type == kind {
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func waitCallSet(ctx context.Context, conn *grpc.ClientConn, wanted ...string) error {
	seen := make(map[string]bool, len(wanted))
	successes := 0
	client := grpc_health_v1.NewHealthClient(conn)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		callCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
		var headers metadata.MD
		_, err := client.Check(callCtx, &grpc_health_v1.HealthCheckRequest{}, grpc.Header(&headers))
		cancel()
		ids := headers.Get("instance-id")
		if err == nil && len(ids) == 1 && slices.Contains(wanted, ids[0]) {
			seen[ids[0]] = true
			successes++
			// 要求连续成功的观察窗口，而不是偶然调用一次存活实例就通过。
			if successes >= 5*len(wanted) && len(seen) == len(wanted) {
				return nil
			}
		} else {
			successes = 0
			clear(seen)
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return xerrors.Wrap(ctx.Err(), "wait for discovered instance calls")
		}
	}
}

func cleanupRegistration(reg registry.Registry, id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := reg.Deregister(ctx, id)
	if xerrors.Is(err, registry.ErrServiceNotFound) {
		return nil
	}
	return err
}

func verifyDiscovery(ctx context.Context, reg registry.Registry, logger clog.Logger) (err error) {
	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	events, err := reg.Watch(watchCtx, "inventory")
	if err != nil {
		return err
	}
	first, firstAddr, err := startHealthServer("first", logger)
	if err != nil {
		return err
	}
	defer first.Stop()
	firstService := &registry.ServiceInstance{ID: "first", Name: "inventory", Endpoints: []string{"grpc://" + firstAddr}}
	if err := reg.Register(ctx, firstService, 15*time.Second); err != nil {
		return err
	}
	defer func() { err = xerrors.Join(err, cleanupRegistration(reg, "first")) }()
	if err := waitEvent(ctx, events, "first", registry.EventTypePut); err != nil {
		return err
	}
	brk, err := breaker.New(&breaker.Config{MinimumRequests: 10, FailureRatio: 0.9}, breaker.WithLogger(logger))
	if err != nil {
		return err
	}
	conn, err := reg.GetConnection(ctx, "inventory", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDefaultServiceConfig(`{"loadBalancingConfig":[{"round_robin":{}}]}`), grpc.WithUnaryInterceptor(brk.UnaryClientInterceptor()))
	if err != nil {
		return err
	}
	defer func() { err = xerrors.Join(err, conn.Close()) }()
	if err := waitCallSet(ctx, conn, "first"); err != nil {
		return err
	}
	second, secondAddr, err := startHealthServer("second", logger)
	if err != nil {
		return err
	}
	defer second.Stop()
	if err := reg.Register(ctx, &registry.ServiceInstance{ID: "second", Name: "inventory", Endpoints: []string{"grpc://" + secondAddr}}, 15*time.Second); err != nil {
		return err
	}
	defer func() { err = xerrors.Join(err, cleanupRegistration(reg, "second")) }()
	if err := waitEvent(ctx, events, "second", registry.EventTypePut); err != nil {
		return err
	}
	if err := waitCallSet(ctx, conn, "first", "second"); err != nil {
		return err
	}
	fmt.Println("动态加入实例后，两个实例均被实际调用")
	if err := reg.Deregister(ctx, "first"); err != nil {
		return err
	}
	if err := waitEvent(ctx, events, "first", registry.EventTypeDelete); err != nil {
		return err
	}
	first.Stop()
	if err := waitCallSet(ctx, conn, "second"); err != nil {
		return err
	}
	fmt.Println("第一个实例注销并停止后，调用继续成功")
	restarted, address, err := startHealthServer("first", logger)
	if err != nil {
		return err
	}
	defer restarted.Stop()
	firstService.Endpoints = []string{"grpc://" + address}
	if err := reg.Register(ctx, firstService, 15*time.Second); err != nil {
		return err
	}
	if err := waitEvent(ctx, events, "first", registry.EventTypePut); err != nil {
		return err
	}
	if err := waitCallSet(ctx, conn, "first", "second"); err != nil {
		return err
	}
	fmt.Println("相同实例 ID 以新地址重新加入后，调用恢复")
	return nil
}

func run(ctx context.Context) (err error) {
	logger, err := clog.New(&clog.Config{Format: "console", Output: "stdout"})
	if err != nil {
		return err
	}
	defer func() { err = xerrors.Join(err, logger.Close()) }()
	endpoints := os.Getenv("ETCD_ENDPOINTS")
	if endpoints == "" {
		endpoints = "127.0.0.1:2379"
	}
	conn, err := connector.NewEtcd(&connector.EtcdConfig{Endpoints: strings.Split(endpoints, ",")}, connector.WithLogger(logger))
	if err != nil {
		return err
	}
	defer func() { err = xerrors.Join(err, conn.Close()) }()
	if err := conn.Connect(ctx); err != nil {
		return xerrors.Wrap(err, "connect Etcd; run make up for this example")
	}
	reg, err := registry.New(&registry.Config{Namespace: "/genesis/scenarios/" + rand.Text()}, registry.WithEtcdConnector(conn), registry.WithLogger(logger))
	if err != nil {
		return err
	}
	defer func() { err = xerrors.Join(err, reg.Close()) }()
	return verifyDiscovery(ctx, reg, logger)
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
