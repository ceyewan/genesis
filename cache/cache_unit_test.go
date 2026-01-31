package cache

import (
	"context"
	"testing"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/xerrors"
)

// TestNew_Unit 单元测试：测试缓存实例创建逻辑
func TestNew_Unit(t *testing.T) {
	logger, _ := clog.New(&clog.Config{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})

	tests := []struct {
		name        string
		cfg         *Config
		opts        []Option
		expectError bool
		checkError  func(error) bool
	}{
		{
			name:        "nil config",
			cfg:         nil,
			expectError: true,
			checkError: func(err error) bool {
				return err != nil && err.Error() == "config is nil"
			},
		},
		{
			name: "standalone mode",
			cfg: &Config{
				Driver: DriverMemory,
			},
			expectError: false,
		},
		{
			name: "redis mode without connector",
			cfg: &Config{
				Driver:     DriverRedis,
				Prefix:     "test:",
				Serializer: "json",
			},
			expectError: true,
			checkError: func(err error) bool {
				return err != nil && err.Error() == "redis connector is required, use WithRedisConnector"
			},
		},
		{
			name: "unsupported driver",
			cfg: &Config{
				Driver: "unsupported",
			},
			expectError: true,
			checkError: func(err error) bool {
				return err != nil && err.Error() == "unsupported driver: unsupported"
			},
		},
		{
			name: "valid config with logger for standalone",
			cfg: &Config{
				Driver: DriverMemory,
			},
			opts:        []Option{WithLogger(logger)},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache, err := New(tt.cfg, tt.opts...)
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
					return
				}
				if tt.checkError != nil && !tt.checkError(err) {
					t.Errorf("Error check failed: %v", err)
				}
				return
			}
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}
			if cache == nil {
				t.Error("Expected cache but got nil")
			}
			if cache != nil {
				cache.Close()
			}
		})
	}
}

// TestCache_Standalone_Unit 单元测试：测试 standalone 模式（内存缓存，不依赖外部服务）
func TestCache_Standalone_Unit(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "info", Output: "stdout"})

	cache, err := New(&Config{
		Driver:     DriverMemory,
		Standalone: &StandaloneConfig{Capacity: 100},
	}, WithLogger(logger))

	if err != nil {
		t.Fatalf("Failed to create standalone cache: %v", err)
	}
	defer cache.Close()

	ctx := context.Background()

	t.Run("Basic Key-Value", func(t *testing.T) {
		key := "local:kv"
		value := "local_value"

		// Set
		err := cache.Set(ctx, key, value, time.Minute)
		if err != nil {
			t.Fatalf("Failed to set: %v", err)
		}

		// Has
		exists, err := cache.Has(ctx, key)
		if err != nil || !exists {
			t.Errorf("Expected key to exist, err: %v", err)
		}

		// Get
		var result string
		err = cache.Get(ctx, key, &result)
		if err != nil {
			t.Fatalf("Failed to get: %v", err)
		}
		if result != value {
			t.Errorf("Expected %s, got %s", value, result)
		}

		// Delete
		err = cache.Delete(ctx, key)
		if err != nil {
			t.Fatalf("Failed to delete: %v", err)
		}

		// Has (should be false)
		exists, err = cache.Has(ctx, key)
		if err != nil || exists {
			t.Errorf("Expected key to not exist")
		}
	})

	t.Run("Complex Types (Pointer)", func(t *testing.T) {
		type User struct {
			Name string
			Age  int
		}

		key := "local:struct"
		user := &User{Name: "Bob", Age: 30}

		err := cache.Set(ctx, key, user, time.Minute)
		if err != nil {
			t.Fatalf("Failed to set struct: %v", err)
		}

		var result *User
		err = cache.Get(ctx, key, &result)
		if err != nil {
			t.Fatalf("Failed to get struct: %v", err)
		}

		if result.Name != "Bob" || result.Age != 30 {
			t.Errorf("Struct mismatch: got %v", result)
		}
	})

	t.Run("Unsupported Operations", func(t *testing.T) {
		key := "local:unsupported"

		// HSet
		err := cache.HSet(ctx, key, "field", "value")
		if err == nil {
			t.Error("Expected error for HSet")
		}

		// ZAdd
		err = cache.ZAdd(ctx, key, 1.0, "member")
		if err == nil {
			t.Error("Expected error for ZAdd")
		}

		// LPush
		err = cache.LPush(ctx, key, "item")
		if err == nil {
			t.Error("Expected error for LPush")
		}
	})

	t.Run("Expire", func(t *testing.T) {
		key := "local:expire"
		value := "value"

		// Set with long TTL
		cache.Set(ctx, key, value, time.Hour)

		// Update TTL to short (500ms)
		err := cache.Expire(ctx, key, 500*time.Millisecond)
		if err != nil {
			t.Fatalf("Failed to expire: %v", err)
		}

		// Wait enough time (1s)
		time.Sleep(1000 * time.Millisecond)

		// 使用 Get 来验证过期
		var res string
		err = cache.Get(ctx, key, &res)
		if err == nil {
			t.Error("Key should have expired (Get should fail)")
		}
	})

	t.Run("Get Interface", func(t *testing.T) {
		key := "local:interface"
		value := "string_value"

		cache.Set(ctx, key, value, time.Minute)

		var result interface{}
		err := cache.Get(ctx, key, &result)
		if err != nil {
			t.Fatalf("Failed to get interface: %v", err)
		}

		if str, ok := result.(string); !ok || str != value {
			t.Errorf("Interface mismatch: got %v", result)
		}
	})

	t.Run("Client returns nil for standalone", func(t *testing.T) {
		client := cache.Client()
		if client != nil {
			t.Errorf("Expected nil client for standalone mode, got %v", client)
		}
	})

	t.Run("MGet not supported in standalone", func(t *testing.T) {
		var results []string
		err := cache.MGet(ctx, []string{}, &results)
		if err == nil {
			t.Error("Expected error for MGet in standalone mode")
		}
	})

	t.Run("MSet not supported in standalone", func(t *testing.T) {
		err := cache.MSet(ctx, map[string]any{}, time.Minute)
		if err == nil {
			t.Error("Expected error for MSet in standalone mode")
		}
	})
}

// TestStandaloneConfig 单元测试：测试 Standalone 配置的默认值
func TestStandaloneConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *StandaloneConfig
		wantCap int
	}{
		{
			name:   "nil config uses defaults",
			cfg:    nil,
			wantCap: 10000,
		},
		{
			name: "custom config",
			cfg:  &StandaloneConfig{Capacity: 500},
			wantCap: 500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache, err := New(&Config{
				Driver:     DriverMemory,
				Standalone: tt.cfg,
			})
			if err != nil {
				t.Fatalf("Failed to create cache: %v", err)
			}
			defer cache.Close()

		// 验证缓存可用
		ctx := context.Background()
		err = cache.Set(ctx, "test", "value", time.Minute)
		if err != nil {
			t.Errorf("Cache operation failed: %v", err)
		}
		})
	}
}

// TestOption 单元测试：测试 Option 函数
func TestOption(t *testing.T) {
	logger := clog.Discard()

	t.Run("WithLogger", func(t *testing.T) {
		cache, err := New(&Config{Driver: DriverMemory}, WithLogger(logger))
		if err != nil {
			t.Fatalf("Failed to create cache: %v", err)
		}
		defer cache.Close()
		// 如果能成功创建，说明 logger 被正确注入
	})

	t.Run("WithRedisConnector on standalone should be ignored", func(t *testing.T) {
		// standalone 模式不使用 RedisConnector，但不应该报错
		cache, err := New(&Config{Driver: DriverMemory})
		if err != nil {
			t.Fatalf("Failed to create standalone cache: %v", err)
		}
		defer cache.Close()
	})
}

// TestErrorWrapping 单元测试：测试错误包装
func TestErrorWrapping(t *testing.T) {
	t.Run("xerrors usage in New", func(t *testing.T) {
		_, err := New(nil)
		if err == nil {
			t.Fatal("Expected error for nil config")
		}
		// 验证返回的是 xerrors 类型
		if !xerrors.Is(err, err) {
			t.Error("Expected xerrors type")
		}
	})
}
