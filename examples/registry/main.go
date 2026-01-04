package main

import (
	"context"
	"fmt"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/registry"
)

func main() {
	fmt.Println("=== Registry Service Registration & Discovery Example ===")
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

	ctx := context.Background()
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

	// 5. 注册服务实例
	service := &registry.ServiceInstance{
		ID:        "user-service-001",
		Name:      "user-service",
		Version:   "1.0.0",
		Endpoints: []string{"grpc://127.0.0.1:9001"},
		Metadata: map[string]string{
			"region": "us-west-1",
			"zone":   "zone-a",
		},
	}

	fmt.Println("1. Registering service instance...")
	if err := reg.Register(ctx, service, 30*time.Second); err != nil {
		logger.Error("failed to register service", clog.Error(err))
		return
	}
	fmt.Printf("✓ Service registered: %s (ID: %s)\n\n", service.Name, service.ID)

	// 确保在退出时注销服务
	defer func() {
		fmt.Println("\n6. Deregistering service...")
		if err := reg.Deregister(ctx, service.ID); err != nil {
			logger.Error("failed to deregister service", clog.Error(err))
		} else {
			fmt.Println("✓ Service deregistered")
		}
	}()

	// 6. 服务发现
	fmt.Println("2. Discovering services...")
	time.Sleep(500 * time.Millisecond) // 等待注册生效
	instances, err := reg.GetService(ctx, "user-service")
	if err != nil {
		logger.Error("failed to get service", clog.Error(err))
		return
	}
	fmt.Printf("✓ Found %d instance(s):\n", len(instances))
	for _, inst := range instances {
		fmt.Printf("  - ID: %s, Endpoints: %v, Version: %s\n",
			inst.ID, inst.Endpoints, inst.Version)
	}
	fmt.Println()

	// 7. 监听服务变化
	fmt.Println("3. Watching service changes...")
	watchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	eventCh, err := reg.Watch(watchCtx, "user-service")
	if err != nil {
		logger.Error("failed to watch service", clog.Error(err))
		return
	}

	// 启动事件监听
	go func() {
		for event := range eventCh {
			switch event.Type {
			case registry.EventTypePut:
				fmt.Printf("✓ Event: Service %s registered/updated (ID: %s)\n",
					event.Service.Name, event.Service.ID)
			case registry.EventTypeDelete:
				fmt.Printf("✓ Event: Service %s deleted (ID: %s)\n",
					event.Service.Name, event.Service.ID)
			}
		}
	}()

	// 8. 注册第二个实例
	time.Sleep(1 * time.Second)
	fmt.Println("\n4. Registering second service instance...")
	service2 := &registry.ServiceInstance{
		ID:        "user-service-002",
		Name:      "user-service",
		Version:   "1.0.0",
		Endpoints: []string{"grpc://127.0.0.1:9002"},
		Metadata: map[string]string{
			"region": "us-west-1",
			"zone":   "zone-b",
		},
	}

	if err := reg.Register(ctx, service2, 30*time.Second); err != nil {
		logger.Error("failed to register service 2", clog.Error(err))
		return
	}
	fmt.Printf("✓ Service registered: %s (ID: %s)\n", service2.Name, service2.ID)

	defer func() {
		if err := reg.Deregister(ctx, service2.ID); err != nil {
			logger.Error("failed to deregister service 2", clog.Error(err))
		}
	}()

	// 9. 再次查询服务列表
	time.Sleep(1 * time.Second)
	fmt.Println("\n5. Discovering services again...")
	instances, err = reg.GetService(ctx, "user-service")
	if err != nil {
		logger.Error("failed to get service", clog.Error(err))
		return
	}
	fmt.Printf("✓ Found %d instance(s):\n", len(instances))
	for _, inst := range instances {
		fmt.Printf("  - ID: %s, Endpoints: %v\n", inst.ID, inst.Endpoints)
	}

	// 等待一段时间以观察事件
	fmt.Println("\nWaiting for events...")
	time.Sleep(3 * time.Second)

	fmt.Println("\n=== Example Completed ===")
	fmt.Println("\nKey Features Demonstrated:")
	fmt.Println("  ✓ Service Registration with TTL and KeepAlive")
	fmt.Println("  ✓ Service Discovery")
	fmt.Println("  ✓ Service Watch with Real-time Events")
	fmt.Println("  ✓ Multiple Service Instances")
	fmt.Println("  ✓ Graceful Deregistration")
}
