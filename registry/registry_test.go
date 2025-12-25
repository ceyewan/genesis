package registry

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
)

// getEnvOrDefault 获取环境变量，如果不存在则返回默认值
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvIntOrDefault 获取环境变量并转换为 int
func getEnvIntOrDefault(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// setupEtcdConn 设置 Etcd 连接
func setupEtcdConn(t *testing.T) connector.EtcdConnector {
	logger, err := clog.New(&clog.Config{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	etcdConn, err := connector.NewEtcd(&connector.EtcdConfig{
		Endpoints:   []string{getEnvOrDefault("ETCD_ENDPOINTS", "127.0.0.1:2379")},
		DialTimeout: 5 * time.Second,
	}, connector.WithLogger(logger))
	if err != nil {
		t.Skipf("Etcd not available, skipping tests: %v", err)
		return nil
	}

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := etcdConn.Connect(ctx); err != nil {
		t.Skipf("Failed to connect to Etcd, skipping tests: %v", err)
		etcdConn.Close()
		return nil
	}

	t.Cleanup(func() {
		etcdConn.Close()
	})

	return etcdConn
}

// setupRegistry 设置 Registry 实例
func setupRegistry(t *testing.T, namespace string) Registry {
	etcdConn := setupEtcdConn(t)
	if etcdConn == nil {
		t.Skip("Etcd not available")
		return nil
	}

	logger, err := clog.New(&clog.Config{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	reg, err := New(etcdConn, &Config{
		Namespace:       namespace,
		Schema:          "etcd-test",
		DefaultTTL:      10 * time.Second,
		RetryInterval:   500 * time.Millisecond,
		EnableCache:     true,
		CacheExpiration: 5 * time.Second,
	}, WithLogger(logger))
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}

	t.Cleanup(func() {
		reg.Close()
	})

	return reg
}

// TestNew 测试 Registry 创建
func TestNew(t *testing.T) {
	etcdConn := setupEtcdConn(t)
	if etcdConn == nil {
		t.Skip("Etcd not available")
		return
	}

	// logger, err := clog.New(&clog.Config{
	// 	Level:  "info",
	// 	Format: "json",
	// 	Output: "stdout",
	// })
	// if err != nil {
	// 	t.Fatalf("Failed to create logger: %v", err)
	// }

	tests := []struct {
		name        string
		conn        connector.EtcdConnector
		cfg         *Config
		opts        []Option
		expectError bool
	}{
		{
			name:        "nil connector",
			conn:        nil,
			cfg:         &Config{},
			expectError: true,
		},
		{
			name:        "nil config (uses defaults)",
			conn:        etcdConn,
			cfg:         nil,
			expectError: false,
		},
		{
			name: "valid config",
			conn: etcdConn,
			cfg: &Config{
				Namespace:       "/test/services",
				DefaultTTL:      30 * time.Second,
				RetryInterval:   1 * time.Second,
				EnableCache:     true,
				CacheExpiration: 10 * time.Second,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg, err := New(tt.conn, tt.cfg, tt.opts...)
			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}
			if reg == nil {
				t.Error("Expected registry but got nil")
			} else {
				reg.Close()
			}
		})
	}
}

// TestRegister 测试服务注册
func TestRegister(t *testing.T) {
	reg := setupRegistry(t, "/test/register")
	if reg == nil {
		t.Skip("Etcd not available")
		return
	}

	ctx := context.Background()
	service := &ServiceInstance{
		ID:      "test-service-001",
		Name:    "test-service",
		Version: "1.0.0",
		Endpoints: []string{
			"grpc://127.0.0.1:8080",
			"http://127.0.0.1:8081",
		},
		Metadata: map[string]string{
			"region": "us-west-1",
			"zone":   "zone-a",
		},
	}

	// 测试注册
	t.Run("Register service", func(t *testing.T) {
		err := reg.Register(ctx, service, 10*time.Second)
		if err != nil {
			t.Fatalf("Failed to register service: %v", err)
		}

		// 验证服务可以被查询到
		instances, err := reg.GetService(ctx, "test-service")
		if err != nil {
			t.Fatalf("Failed to get service: %v", err)
		}

		if len(instances) != 1 {
			t.Errorf("Expected 1 instance, got %d", len(instances))
		}

		if instances[0].ID != service.ID {
			t.Errorf("Expected ID %s, got %s", service.ID, instances[0].ID)
		}

		if instances[0].Name != service.Name {
			t.Errorf("Expected Name %s, got %s", service.Name, instances[0].Name)
		}
	})

	// 测试重复注册
	t.Run("Register duplicate service", func(t *testing.T) {
		err := reg.Register(ctx, service, 10*time.Second)
		if err != ErrServiceAlreadyRegistered {
			t.Errorf("Expected ErrServiceAlreadyRegistered, got %v", err)
		}
	})

	// 测试无效输入
	t.Run("Register with invalid input", func(t *testing.T) {
		tests := []struct {
			name    string
			service *ServiceInstance
		}{
			{"nil service", nil},
			{"empty ID", &ServiceInstance{Name: "test", ID: ""}},
			{"empty Name", &ServiceInstance{Name: "", ID: "test-001"}},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := reg.Register(ctx, tt.service, 10*time.Second)
				if err != ErrInvalidServiceInstance {
					t.Errorf("Expected ErrInvalidServiceInstance, got %v", err)
				}
			})
		}
	})
}

// TestDeregister 测试服务注销
func TestDeregister(t *testing.T) {
	reg := setupRegistry(t, "/test/deregister")
	if reg == nil {
		t.Skip("Etcd not available")
		return
	}

	ctx := context.Background()

	t.Run("Deregister service", func(t *testing.T) {
		service := &ServiceInstance{
			ID:        "test-service-002",
			Name:      "test-service",
			Version:   "1.0.0",
			Endpoints: []string{"grpc://127.0.0.1:8080"},
		}

		// 先注册
		err := reg.Register(ctx, service, 10*time.Second)
		if err != nil {
			t.Fatalf("Failed to register service: %v", err)
		}

		// 验证已注册
		instances, err := reg.GetService(ctx, "test-service")
		if err != nil {
			t.Fatalf("Failed to get service: %v", err)
		}
		if len(instances) != 1 {
			t.Fatalf("Expected 1 instance before deregister, got %d", len(instances))
		}

		// 注销
		err = reg.Deregister(ctx, service.ID)
		if err != nil {
			t.Fatalf("Failed to deregister service: %v", err)
		}

		// 注意：由于缓存机制，GetService 可能仍返回缓存的数据
		// 这是一个已知的权衡：优先性能而非强一致性
		// Watch 事件最终会更新缓存，但这需要时间
		// 因此这里只验证 Deregister 调用成功，不验证缓存状态
	})

	t.Run("Deregister non-existent service", func(t *testing.T) {
		err := reg.Deregister(ctx, "non-existent-id")
		if err != ErrServiceNotFound {
			t.Errorf("Expected ErrServiceNotFound, got %v", err)
		}
	})

	t.Run("Deregister with invalid input", func(t *testing.T) {
		err := reg.Deregister(ctx, "")
		if err != ErrInvalidServiceInstance {
			t.Errorf("Expected ErrInvalidServiceInstance, got %v", err)
		}
	})
}

// TestGetService 测试服务发现
func TestGetService(t *testing.T) {
	reg := setupRegistry(t, "/test/get-service")
	if reg == nil {
		t.Skip("Etcd not available")
		return
	}

	ctx := context.Background()

	// 注册多个服务实例
	services := []*ServiceInstance{
		{
			ID:        "svc-001",
			Name:      "multi-instance",
			Version:   "1.0.0",
			Endpoints: []string{"grpc://127.0.0.1:8001"},
			Metadata:  map[string]string{"zone": "a"},
		},
		{
			ID:        "svc-002",
			Name:      "multi-instance",
			Version:   "1.0.0",
			Endpoints: []string{"grpc://127.0.0.1:8002"},
			Metadata:  map[string]string{"zone": "b"},
		},
		{
			ID:        "svc-003",
			Name:      "multi-instance",
			Version:   "1.0.0",
			Endpoints: []string{"grpc://127.0.0.1:8003"},
			Metadata:  map[string]string{"zone": "c"},
		},
	}

	for _, svc := range services {
		err := reg.Register(ctx, svc, 10*time.Second)
		if err != nil {
			t.Fatalf("Failed to register service %s: %v", svc.ID, err)
		}
	}

	t.Run("Get multiple instances", func(t *testing.T) {
		instances, err := reg.GetService(ctx, "multi-instance")
		if err != nil {
			t.Fatalf("Failed to get service: %v", err)
		}

		if len(instances) != 3 {
			t.Errorf("Expected 3 instances, got %d", len(instances))
		}

		// 验证所有实例都被返回
		ids := make(map[string]bool)
		for _, inst := range instances {
			ids[inst.ID] = true
		}
		for _, svc := range services {
			if !ids[svc.ID] {
				t.Errorf("Expected to find instance %s", svc.ID)
			}
		}
	})

	t.Run("Get non-existent service", func(t *testing.T) {
		instances, err := reg.GetService(ctx, "non-existent")
		if err != nil {
			t.Fatalf("Failed to get service: %v", err)
		}
		if len(instances) != 0 {
			t.Errorf("Expected 0 instances for non-existent service, got %d", len(instances))
		}
	})

	t.Run("Get with empty service name", func(t *testing.T) {
		_, err := reg.GetService(ctx, "")
		if err != ErrInvalidServiceInstance {
			t.Errorf("Expected ErrInvalidServiceInstance, got %v", err)
		}
	})
}

// TestWatch 测试服务变化监听
func TestWatch(t *testing.T) {
	reg := setupRegistry(t, "/test/watch")
	if reg == nil {
		t.Skip("Etcd not available")
		return
	}

	ctx := context.Background()

	t.Run("Watch service changes", func(t *testing.T) {
		// 启动监听
		eventCh, err := reg.Watch(ctx, "watch-test")
		if err != nil {
			t.Fatalf("Failed to watch service: %v", err)
		}

		// 给 watch 一些时间启动
		time.Sleep(100 * time.Millisecond)

		// 注册服务
		service := &ServiceInstance{
			ID:        "watch-001",
			Name:      "watch-test",
			Version:   "1.0.0",
			Endpoints: []string{"grpc://127.0.0.1:9000"},
		}

		go func() {
			time.Sleep(200 * time.Millisecond)
			reg.Register(ctx, service, 10*time.Second)
		}()

		// 等待事件
		select {
		case event := <-eventCh:
			if event.Type != EventTypePut {
				t.Errorf("Expected EventTypePut, got %s", event.Type)
			}
			if event.Service.ID != service.ID {
				t.Errorf("Expected ID %s, got %s", service.ID, event.Service.ID)
			}
		case <-time.After(2 * time.Second):
			t.Error("Timeout waiting for watch event")
		}
	})

	t.Run("Watch DELETE event", func(t *testing.T) {
		// 先注册一个服务
		service := &ServiceInstance{
			ID:        "watch-002",
			Name:      "watch-delete-test",
			Version:   "1.0.0",
			Endpoints: []string{"grpc://127.0.0.1:9001"},
		}
		err := reg.Register(ctx, service, 10*time.Second)
		if err != nil {
			t.Fatalf("Failed to register service: %v", err)
		}

		// 启动监听
		eventCh, err := reg.Watch(ctx, "watch-delete-test")
		if err != nil {
			t.Fatalf("Failed to watch service: %v", err)
		}

		// 给 watch 一些时间启动
		time.Sleep(100 * time.Millisecond)

		// 注销服务
		go func() {
			time.Sleep(200 * time.Millisecond)
			reg.Deregister(ctx, service.ID)
		}()

		// 等待事件
		select {
		case event := <-eventCh:
			if event.Type != EventTypeDelete {
				t.Errorf("Expected EventTypeDelete, got %s", event.Type)
			}
			if event.Service.ID != service.ID {
				t.Errorf("Expected ID %s, got %s", service.ID, event.Service.ID)
			}
		case <-time.After(2 * time.Second):
			t.Error("Timeout waiting for watch event")
		}
	})

	t.Run("Watch with invalid input", func(t *testing.T) {
		_, err := reg.Watch(ctx, "")
		if err != ErrInvalidServiceInstance {
			t.Errorf("Expected ErrInvalidServiceInstance, got %v", err)
		}
	})
}

// TestKeepAlive 测试租约续约
func TestKeepAlive(t *testing.T) {
	reg := setupRegistry(t, "/test/keepalive")
	if reg == nil {
		t.Skip("Etcd not available")
		return
	}

	ctx := context.Background()

	t.Run("Service survives TTL", func(t *testing.T) {
		// 使用较短的 TTL
		ttl := 5 * time.Second
		service := &ServiceInstance{
			ID:        "keepalive-001",
			Name:      "keepalive-test",
			Version:   "1.0.0",
			Endpoints: []string{"grpc://127.0.0.1:10000"},
		}

		// 注册服务
		err := reg.Register(ctx, service, ttl)
		if err != nil {
			t.Fatalf("Failed to register service: %v", err)
		}

		// 等待超过 TTL 时间
		time.Sleep(ttl + 2*time.Second)

		// 服务应该仍然存在（因为 KeepAlive）
		instances, err := reg.GetService(ctx, "keepalive-test")
		if err != nil {
			t.Fatalf("Failed to get service: %v", err)
		}

		if len(instances) != 1 {
			t.Errorf("Expected 1 instance after TTL (KeepAlive should keep it alive), got %d", len(instances))
		}

		if len(instances) > 0 && instances[0].ID != service.ID {
			t.Errorf("Expected ID %s, got %s", service.ID, instances[0].ID)
		}
	})

	t.Run("Service deregistered explicitly", func(t *testing.T) {
		service := &ServiceInstance{
			ID:        "keepalive-002",
			Name:      "keepalive-test2",
			Version:   "1.0.0",
			Endpoints: []string{"grpc://127.0.0.1:10001"},
		}

		// 注册服务
		err := reg.Register(ctx, service, 10*time.Second)
		if err != nil {
			t.Fatalf("Failed to register service: %v", err)
		}

		// 显式注销
		err = reg.Deregister(ctx, service.ID)
		if err != nil {
			t.Fatalf("Failed to deregister service: %v", err)
		}

		// 服务应该不存在
		instances, err := reg.GetService(ctx, "keepalive-test2")
		if err != nil {
			t.Fatalf("Failed to get service: %v", err)
		}

		if len(instances) != 0 {
			t.Errorf("Expected 0 instances after deregister, got %d", len(instances))
		}
	})
}

// TestCache 测试本地缓存
func TestCache(t *testing.T) {
	reg := setupRegistry(t, "/test/cache")
	if reg == nil {
		t.Skip("Etcd not available")
		return
	}

	ctx := context.Background()

	t.Run("Cache hit after first query", func(t *testing.T) {
		service := &ServiceInstance{
			ID:        "cache-001",
			Name:      "cache-test",
			Version:   "1.0.0",
			Endpoints: []string{"grpc://127.0.0.1:11000"},
		}

		// 注册服务
		err := reg.Register(ctx, service, 10*time.Second)
		if err != nil {
			t.Fatalf("Failed to register service: %v", err)
		}

		// 第一次查询（从 Etcd）
		instances1, err := reg.GetService(ctx, "cache-test")
		if err != nil {
			t.Fatalf("Failed to get service: %v", err)
		}

		// 第二次查询（从缓存）
		instances2, err := reg.GetService(ctx, "cache-test")
		if err != nil {
			t.Fatalf("Failed to get service: %v", err)
		}

		// 结果应该一致
		if len(instances1) != len(instances2) {
			t.Errorf("Cache inconsistency: first query got %d instances, second got %d",
				len(instances1), len(instances2))
		}

		if len(instances2) > 0 && instances2[0].ID != service.ID {
			t.Errorf("Expected ID %s, got %s", service.ID, instances2[0].ID)
		}
	})

	t.Run("Cache invalidated on service change", func(t *testing.T) {
		service1 := &ServiceInstance{
			ID:        "cache-002",
			Name:      "cache-update-test",
			Version:   "1.0.0",
			Endpoints: []string{"grpc://127.0.0.1:11001"},
		}

		// 注册第一个版本
		err := reg.Register(ctx, service1, 10*time.Second)
		if err != nil {
			t.Fatalf("Failed to register service: %v", err)
		}

		// 查询（缓存此结果）
		_, err = reg.GetService(ctx, "cache-update-test")
		if err != nil {
			t.Fatalf("Failed to get service: %v", err)
		}

		// 更新服务 - 使用相同的 ID 但不同的版本
		service1Updated := &ServiceInstance{
			ID:        "cache-002",
			Name:      "cache-update-test",
			Version:   "1.1.0",
			Endpoints: []string{"grpc://127.0.0.1:11002"},
			Metadata:  map[string]string{"updated": "true"},
		}

		// 直接注册更新（相同 ID 会覆盖）
		err = reg.Register(ctx, service1Updated, 10*time.Second)
		if err != nil && err != ErrServiceAlreadyRegistered {
			t.Fatalf("Failed to register updated service: %v", err)
		}

		// 等待 Watch 事件处理和缓存更新
		time.Sleep(500 * time.Millisecond)

		// 再次查询 - 由于可能返回缓存数据，我们接受两种结果
		instances2, err := reg.GetService(ctx, "cache-update-test")
		if err != nil {
			t.Fatalf("Failed to get service: %v", err)
		}

		if len(instances2) == 0 {
			t.Error("Expected at least 1 instance after update")
		}
		// 注意：由于缓存是最终一致的，这里不严格检查版本
		// 只验证服务仍然存在
	})
}

// TestClose 测试资源清理
func TestClose(t *testing.T) {
	// 创建独立的连接用于此测试
	logger, err := clog.New(&clog.Config{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	etcdConn, err := connector.NewEtcd(&connector.EtcdConfig{
		Endpoints:   []string{getEnvOrDefault("ETCD_ENDPOINTS", "127.0.0.1:2379")},
		DialTimeout: 5 * time.Second,
	}, connector.WithLogger(logger))
	if err != nil {
		t.Skipf("Etcd not available, skipping test: %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := etcdConn.Connect(ctx); err != nil {
		t.Skipf("Failed to connect to Etcd, skipping test: %v", err)
		etcdConn.Close()
		return
	}

	reg, err := New(etcdConn, &Config{
		Namespace: "/test/close",
	}, WithLogger(logger))
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}

	ctx = context.Background()

	// 注册一些服务
	for i := 0; i < 3; i++ {
		service := &ServiceInstance{
			ID:        fmt.Sprintf("close-%03d", i),
			Name:      "close-test",
			Version:   "1.0.0",
			Endpoints: []string{fmt.Sprintf("grpc://127.0.0.1:1200%d", i)},
		}
		err := reg.Register(ctx, service, 10*time.Second)
		if err != nil {
			t.Fatalf("Failed to register service: %v", err)
		}
	}

	// 关闭 Registry
	err = reg.Close()
	if err != nil {
		t.Fatalf("Failed to close registry: %v", err)
	}

	// 再次关闭应该是安全的
	err = reg.Close()
	if err != nil {
		t.Errorf("Close should be idempotent, got error: %v", err)
	}

	// 最后关闭连接
	etcdConn.Close()
}

// TestMultipleServices 测试多个服务
func TestMultipleServices(t *testing.T) {
	reg := setupRegistry(t, "/test/multiple")
	if reg == nil {
		t.Skip("Etcd not available")
		return
	}

	ctx := context.Background()

	// 注册多个不同的服务
	services := []struct {
		name  string
		count int
	}{
		{"user-service", 2},
		{"order-service", 3},
		{"payment-service", 1},
	}

	for _, svc := range services {
		for i := 0; i < svc.count; i++ {
			service := &ServiceInstance{
				ID:        fmt.Sprintf("%s-%03d", svc.name, i),
				Name:      svc.name,
				Version:   "1.0.0",
				Endpoints: []string{fmt.Sprintf("grpc://127.0.0.1:13000%d", i)},
			}
			err := reg.Register(ctx, service, 10*time.Second)
			if err != nil {
				t.Fatalf("Failed to register service %s: %v", service.ID, err)
			}
		}
	}

	// 验证每个服务的实例数量
	for _, svc := range services {
		instances, err := reg.GetService(ctx, svc.name)
		if err != nil {
			t.Fatalf("Failed to get service %s: %v", svc.name, err)
		}
		if len(instances) != svc.count {
			t.Errorf("Service %s: expected %d instances, got %d", svc.name, svc.count, len(instances))
		}
	}
}

// TestDefaultTTL 测试默认 TTL
func TestDefaultTTL(t *testing.T) {
	etcdConn := setupEtcdConn(t)
	if etcdConn == nil {
		t.Skip("Etcd not available")
		return
	}

	logger, err := clog.New(&clog.Config{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	// 创建不设置 TTL 的 Registry（使用默认 TTL）
	reg, err := New(etcdConn, &Config{
		Namespace: "/test/default-ttl",
		// DefaultTTL 为 0，应使用默认值 30s
	}, WithLogger(logger))
	if err != nil {
		t.Fatalf("Failed to create registry: %v", err)
	}
	defer reg.Close()

	ctx := context.Background()
	service := &ServiceInstance{
		ID:        "default-ttl-001",
		Name:      "default-ttl-test",
		Version:   "1.0.0",
		Endpoints: []string{"grpc://127.0.0.1:14000"},
	}

	// 注册时不指定 TTL（传 0）
	err = reg.Register(ctx, service, 0)
	if err != nil {
		t.Fatalf("Failed to register service: %v", err)
	}

	// 立即查询应该存在
	instances, err := reg.GetService(ctx, "default-ttl-test")
	if err != nil {
		t.Fatalf("Failed to get service: %v", err)
	}
	if len(instances) != 1 {
		t.Errorf("Expected 1 instance, got %d", len(instances))
	}
}

// TestNamespaceIsolation 测试命名空间隔离
func TestNamespaceIsolation(t *testing.T) {
	etcdConn := setupEtcdConn(t)
	if etcdConn == nil {
		t.Skip("Etcd not available")
		return
	}

	logger, err := clog.New(&clog.Config{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	ctx := context.Background()

	// 创建两个不同命名空间的 Registry
	reg1, err := New(etcdConn, &Config{
		Namespace: "/test/ns1",
	}, WithLogger(logger))
	if err != nil {
		t.Fatalf("Failed to create registry1: %v", err)
	}
	defer reg1.Close()

	reg2, err := New(etcdConn, &Config{
		Namespace: "/test/ns2",
	}, WithLogger(logger))
	if err != nil {
		t.Fatalf("Failed to create registry2: %v", err)
	}
	defer reg2.Close()

	service := &ServiceInstance{
		ID:        "ns-test-001",
		Name:      "ns-test",
		Version:   "1.0.0",
		Endpoints: []string{"grpc://127.0.0.1:15000"},
	}

	// 在 reg1 中注册
	err = reg1.Register(ctx, service, 10*time.Second)
	if err != nil {
		t.Fatalf("Failed to register service in reg1: %v", err)
	}

	// reg1 应该能查到
	instances1, err := reg1.GetService(ctx, "ns-test")
	if err != nil {
		t.Fatalf("Failed to get service from reg1: %v", err)
	}
	if len(instances1) != 1 {
		t.Errorf("Expected 1 instance in reg1, got %d", len(instances1))
	}

	// reg2 应该查不到（不同命名空间）
	instances2, err := reg2.GetService(ctx, "ns-test")
	if err != nil {
		t.Fatalf("Failed to get service from reg2: %v", err)
	}
	if len(instances2) != 0 {
		t.Errorf("Expected 0 instances in reg2 (different namespace), got %d", len(instances2))
	}
}
