package cache

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

// getEnvIntOrDefault 获取环境变量并转换为 int，如果不存在或转换失败则返回默认值
func getEnvIntOrDefault(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// setupRedisConn 设置 Redis 连接
func setupRedisConn(t *testing.T) connector.RedisConnector {
	logger, err := clog.New(&clog.Config{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	redisConn, err := connector.NewRedis(&connector.RedisConfig{
		Addr:         getEnvOrDefault("REDIS_ADDR", "127.0.0.1:6379"),
		Password:     getEnvOrDefault("REDIS_PASSWORD", ""),
		DB:           getEnvIntOrDefault("REDIS_DB", 2), // 使用 DB 1 进行测试
		DialTimeout:  2 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     10,
	}, connector.WithLogger(logger))
	if err != nil {
		t.Skipf("Redis not available, skipping tests: %v", err)
		return nil
	}

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := redisConn.Connect(ctx); err != nil {
		t.Skipf("Failed to connect to Redis, skipping tests: %v", err)
		redisConn.Close()
		return nil
	}

	t.Cleanup(func() {
		redisConn.Close()
	})

	return redisConn
}

// setupCache 设置 Cache 实例
func setupCache(t *testing.T, prefix string) Cache {
	redisConn := setupRedisConn(t)
	if redisConn == nil {
		t.Skip("Redis not available")
		return nil
	}

	// 清理测试前缀的数据，防止历史残留
	ctx := context.Background()
	client := redisConn.GetClient()
	keys, _ := client.Keys(ctx, prefix+"*").Result()
	if len(keys) > 0 {
		client.Del(ctx, keys...)
	}

	logger, err := clog.New(&clog.Config{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	cache, err := New(&Config{
		Prefix:     prefix,
		Serializer: "json",
	}, WithRedisConnector(redisConn), WithLogger(logger))
	if err != nil {
		t.Fatalf("Failed to create cache: %v", err)
	}

	t.Cleanup(func() {
		cache.Close()
	})

	return cache
}

func TestNew(t *testing.T) {
	logger, err := clog.New(&clog.Config{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	tests := []struct {
		name        string
		cfg         *Config
		opts        []Option
		expectError bool
	}{
		{
			name:        "nil config",
			cfg:         nil,
			expectError: true,
		},
		{
			name: "valid config without logger",
			cfg: &Config{
				Prefix:     "test:",
				Serializer: "json",
			},
			opts:        []Option{WithRedisConnector(setupRedisConn(t))},
			expectError: false,
		},
		{
			name: "valid config with logger",
			cfg: &Config{
				Prefix:     "test:",
				Serializer: "json",
			},
			opts:        []Option{WithRedisConnector(setupRedisConn(t)), WithLogger(logger)},
			expectError: false,
		},
		{
			name: "standalone mode",
			cfg: &Config{
				Driver: DriverMemory,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache, err := New(tt.cfg, tt.opts...)
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
			if cache == nil {
				t.Error("Expected cache but got nil")
			}
		})
	}
}

func TestCache_KeyValue(t *testing.T) {
	cache := setupCache(t, "test:kv:")
	if cache == nil {
		t.Skip("Redis not available")
		return
	}

	ctx := context.Background()

	t.Run("Set and Get", func(t *testing.T) {
		key := "user:123"
		value := map[string]interface{}{
			"id":   123,
			"name": "Alice",
		}

		// Set
		err := cache.Set(ctx, key, value, time.Hour)
		if err != nil {
			t.Fatalf("Failed to set value: %v", err)
		}

		// Get
		var result map[string]interface{}
		err = cache.Get(ctx, key, &result)
		if err != nil {
			t.Fatalf("Failed to get value: %v", err)
		}

		if result["id"] != float64(123) { // JSON unmarshal converts numbers to float64
			t.Errorf("Expected id 123, got %v", result["id"])
		}
		if result["name"] != "Alice" {
			t.Errorf("Expected name Alice, got %v", result["name"])
		}
	})

	t.Run("Has", func(t *testing.T) {
		key := "exists:123"
		value := "test value"

		// Set a value
		err := cache.Set(ctx, key, value, time.Hour)
		if err != nil {
			t.Fatalf("Failed to set value: %v", err)
		}

		// Check exists
		exists, err := cache.Has(ctx, key)
		if err != nil {
			t.Fatalf("Failed to check exists: %v", err)
		}
		if !exists {
			t.Error("Expected key to exist")
		}

		// Check non-existent key
		exists, err = cache.Has(ctx, "nonexistent")
		if err != nil {
			t.Fatalf("Failed to check exists: %v", err)
		}
		if exists {
			t.Error("Expected key to not exist")
		}
	})

	t.Run("Delete", func(t *testing.T) {
		key := "delete:123"
		value := "test value"

		// Set a value
		err := cache.Set(ctx, key, value, time.Hour)
		if err != nil {
			t.Fatalf("Failed to set value: %v", err)
		}

		// Verify it exists
		exists, err := cache.Has(ctx, key)
		if err != nil {
			t.Fatalf("Failed to check exists: %v", err)
		}
		if !exists {
			t.Error("Expected key to exist before delete")
		}

		// Delete
		err = cache.Delete(ctx, key)
		if err != nil {
			t.Fatalf("Failed to delete: %v", err)
		}

		// Verify it's gone
		exists, err = cache.Has(ctx, key)
		if err != nil {
			t.Fatalf("Failed to check exists: %v", err)
		}
		if exists {
			t.Error("Expected key to not exist after delete")
		}
	})

	t.Run("Expire", func(t *testing.T) {
		key := "expire:123"
		value := "test value"

		// Set without TTL
		err := cache.Set(ctx, key, value, 0)
		if err != nil {
			t.Fatalf("Failed to set value: %v", err)
		}

		// Set TTL
		err = cache.Expire(ctx, key, time.Second)
		if err != nil {
			t.Fatalf("Failed to set expire: %v", err)
		}

		// Wait for expiration
		time.Sleep(2 * time.Second)

		// Check if expired
		exists, err := cache.Has(ctx, key)
		if err != nil {
			t.Fatalf("Failed to check exists: %v", err)
		}
		if exists {
			t.Error("Expected key to be expired")
		}
	})
}

func TestCache_Hash(t *testing.T) {
	cache := setupCache(t, "test:hash:")
	if cache == nil {
		t.Skip("Redis not available")
		return
	}

	ctx := context.Background()
	key := "user:123"

	t.Run("HSet and HGet", func(t *testing.T) {
		// Set hash fields
		err := cache.HSet(ctx, key, "name", "Alice")
		if err != nil {
			t.Fatalf("Failed to set hash field: %v", err)
		}
		err = cache.HSet(ctx, key, "age", 25)
		if err != nil {
			t.Fatalf("Failed to set hash field: %v", err)
		}

		// Get hash fields
		var name string
		err = cache.HGet(ctx, key, "name", &name)
		if err != nil {
			t.Fatalf("Failed to get hash field: %v", err)
		}
		if name != "Alice" {
			t.Errorf("Expected name Alice, got %v", name)
		}

		var age int
		err = cache.HGet(ctx, key, "age", &age)
		if err != nil {
			t.Fatalf("Failed to get hash field: %v", err)
		}
		if age != 25 {
			t.Errorf("Expected age 25, got %v", age)
		}
	})

	t.Run("HGetAll", func(t *testing.T) {
		// Add more fields - use string values to avoid JSON unmarshaling issues
		err := cache.HSet(ctx, key, "email", "alice@example.com")
		if err != nil {
			t.Fatalf("Failed to set hash field: %v", err)
		}
		err = cache.HSet(ctx, key, "age", "25") // Set as string
		if err != nil {
			t.Fatalf("Failed to set hash field: %v", err)
		}

		// Get all fields
		var allFields map[string]string
		err = cache.HGetAll(ctx, key, &allFields)
		if err != nil {
			t.Fatalf("Failed to get all hash fields: %v", err)
		}

		if allFields["name"] != "Alice" {
			t.Errorf("Expected name Alice, got %v", allFields["name"])
		}
		if allFields["age"] != "25" {
			t.Errorf("Expected age 25, got %v", allFields["age"])
		}
		if allFields["email"] != "alice@example.com" {
			t.Errorf("Expected email alice@example.com, got %v", allFields["email"])
		}
	})

	t.Run("HDel", func(t *testing.T) {
		// Delete fields
		err := cache.HDel(ctx, key, "age", "email")
		if err != nil {
			t.Fatalf("Failed to delete hash fields: %v", err)
		}

		// Verify deleted
		var age int
		err = cache.HGet(ctx, key, "age", &age)
		if err == nil {
			t.Error("Expected error when getting deleted field")
		}

		// Verify remaining field
		var name string
		err = cache.HGet(ctx, key, "name", &name)
		if err != nil {
			t.Errorf("Failed to get remaining field: %v", err)
		}
		if name != "Alice" {
			t.Errorf("Expected name Alice, got %v", name)
		}
	})

	t.Run("HIncrBy", func(t *testing.T) {
		key := "counter:123"

		// Initialize counter
		err := cache.HSet(ctx, key, "count", 10)
		if err != nil {
			t.Fatalf("Failed to set hash field: %v", err)
		}

		// Increment
		newVal, err := cache.HIncrBy(ctx, key, "count", 5)
		if err != nil {
			t.Fatalf("Failed to increment hash field: %v", err)
		}
		if newVal != 15 {
			t.Errorf("Expected 15, got %v", newVal)
		}

		// Verify new value
		var count int
		err = cache.HGet(ctx, key, "count", &count)
		if err != nil {
			t.Fatalf("Failed to get incremented value: %v", err)
		}
		if count != 15 {
			t.Errorf("Expected count 15, got %v", count)
		}
	})
}

func TestCache_SortedSet(t *testing.T) {
	cache := setupCache(t, "test:zset:")
	if cache == nil {
		t.Skip("Redis not available")
		return
	}

	ctx := context.Background()
	key := "leaderboard"

	t.Run("ZAdd and ZScore", func(t *testing.T) {
		// Add members
		err := cache.ZAdd(ctx, key, 100.0, "alice")
		if err != nil {
			t.Fatalf("Failed to add to sorted set: %v", err)
		}
		err = cache.ZAdd(ctx, key, 200.0, "bob")
		if err != nil {
			t.Fatalf("Failed to add to sorted set: %v", err)
		}

		// Get scores
		score, err := cache.ZScore(ctx, key, "alice")
		if err != nil {
			t.Fatalf("Failed to get score: %v", err)
		}
		if score != 100.0 {
			t.Errorf("Expected score 100.0, got %v", score)
		}
	})

	t.Run("ZRange", func(t *testing.T) {
		// Add more members
		err := cache.ZAdd(ctx, key, 150.0, "charlie")
		if err != nil {
			t.Fatalf("Failed to add to sorted set: %v", err)
		}

		// Get range (0 to -1 = all)
		var members []string
		err = cache.ZRange(ctx, key, 0, -1, &members)
		if err != nil {
			t.Fatalf("Failed to get range: %v", err)
		}
		if len(members) != 3 {
			t.Errorf("Expected 3 members, got %d", len(members))
		}
		// Should be in ascending order: alice, charlie, bob
		if members[0] != "alice" || members[1] != "charlie" || members[2] != "bob" {
			t.Errorf("Expected [alice, charlie, bob], got %v", members)
		}
	})

	t.Run("ZRevRange", func(t *testing.T) {
		// Get reverse range (0 to 1 = top 2)
		var topMembers []string
		err := cache.ZRevRange(ctx, key, 0, 1, &topMembers)
		if err != nil {
			t.Fatalf("Failed to get reverse range: %v", err)
		}
		if len(topMembers) != 2 {
			t.Errorf("Expected 2 members, got %d", len(topMembers))
		}
		// Should be in descending order: bob, charlie
		if topMembers[0] != "bob" || topMembers[1] != "charlie" {
			t.Errorf("Expected [bob, charlie], got %v", topMembers)
		}
	})

	t.Run("ZRangeByScore", func(t *testing.T) {
		// Get members by score range
		var members []string
		err := cache.ZRangeByScore(ctx, key, 120.0, 180.0, &members)
		if err != nil {
			t.Fatalf("Failed to get range by score: %v", err)
		}
		if len(members) != 1 {
			t.Errorf("Expected 1 member, got %d", len(members))
		}
		if members[0] != "charlie" {
			t.Errorf("Expected charlie, got %v", members[0])
		}
	})

	t.Run("ZRem", func(t *testing.T) {
		// Remove member
		err := cache.ZRem(ctx, key, "charlie")
		if err != nil {
			t.Fatalf("Failed to remove from sorted set: %v", err)
		}

		// Verify removed
		_, err = cache.ZScore(ctx, key, "charlie")
		if err == nil {
			t.Error("Expected error when getting removed member")
		}

		// Verify remaining members
		var members []string
		err = cache.ZRange(ctx, key, 0, -1, &members)
		if err != nil {
			t.Fatalf("Failed to get range: %v", err)
		}
		if len(members) != 2 {
			t.Errorf("Expected 2 remaining members, got %d", len(members))
		}
	})
}

func TestCache_List(t *testing.T) {
	cache := setupCache(t, "test:list:")
	if cache == nil {
		t.Skip("Redis not available")
		return
	}

	ctx := context.Background()
	key := "messages"

	t.Run("LPush and RPop", func(t *testing.T) {
		testKey := key + ":lpush"

		// Test single operation first
		err := cache.LPush(ctx, testKey, "msg1")
		if err != nil {
			t.Fatalf("Failed to push to list: %v", err)
		}

		// Should get msg1 back
		var result string
		err = cache.RPop(ctx, testKey, &result)
		if err != nil {
			t.Fatalf("Failed to pop from list: %v", err)
		}
		if result != "msg1" {
			t.Errorf("Expected msg1, got %v", result)
		}

		// Test multiple operations
		err = cache.LPush(ctx, testKey, "msg3", "msg2", "msg1")
		if err != nil {
			t.Fatalf("Failed to push multiple to list: %v", err)
		}

		// Redis LPush batch operation: LPush(key, "msg3", "msg2", "msg1")
		// Results in: [msg1, msg2, msg3] (msg1 is at the left/head)
		// So RPop (right/tail) should get msg3 first
		err = cache.RPop(ctx, testKey, &result)
		if err != nil {
			t.Fatalf("Failed to pop from list: %v", err)
		}
		if result != "msg3" {
			t.Errorf("Expected msg3, got %v", result)
		}

		// Next should get msg2
		err = cache.RPop(ctx, testKey, &result)
		if err != nil {
			t.Fatalf("Failed to pop from list: %v", err)
		}
		if result != "msg2" {
			t.Errorf("Expected msg2, got %v", result)
		}
	})

	t.Run("RPush and LPop", func(t *testing.T) {
		testKey := key + ":rpush"
		messages := []string{"msg1", "msg2", "msg3"}

		// First set up initial state
		for _, msg := range messages {
			err := cache.LPush(ctx, testKey, msg)
			if err != nil {
				t.Fatalf("Failed to push to list: %v", err)
			}
		}

		// Push more messages to right
		err := cache.RPush(ctx, testKey, "msg4", "msg5")
		if err != nil {
			t.Fatalf("Failed to push to list: %v", err)
		}

		// Pop from left (should get msg3, the first message pushed)
		var result string
		err = cache.LPop(ctx, testKey, &result)
		if err != nil {
			t.Fatalf("Failed to pop from list: %v", err)
		}
		if result != "msg3" {
			t.Errorf("Expected msg3, got %v", result)
		}
	})

	t.Run("LRange", func(t *testing.T) {
		testKey := key + ":lrange"
		messages := []string{"msg1", "msg2", "msg3"}

		// Set up initial state
		for _, msg := range messages {
			err := cache.LPush(ctx, testKey, msg)
			if err != nil {
				t.Fatalf("Failed to push to list: %v", err)
			}
		}

		// Push more messages to right
		err := cache.RPush(ctx, testKey, "msg4", "msg5")
		if err != nil {
			t.Fatalf("Failed to push to list: %v", err)
		}

		// Pop from right to match the "RPush and LPop" scenario
		var unused string
		err = cache.RPop(ctx, testKey, &unused)
		if err != nil {
			t.Fatalf("Failed to pop from list: %v", err)
		}

		// Pop from left to match the "RPush and LPop" scenario
		err = cache.LPop(ctx, testKey, &unused)
		if err != nil {
			t.Fatalf("Failed to pop from list: %v", err)
		}

		// Get all remaining messages
		var remainingMessages []string
		err = cache.LRange(ctx, testKey, 0, -1, &remainingMessages)
		if err != nil {
			t.Fatalf("Failed to get range: %v", err)
		}
		// Analysis:
		// 1. LPush msg1, msg2, msg3 -> [msg3, msg2, msg1]
		// 2. RPush msg4, msg5 -> [msg3, msg2, msg1, msg4, msg5]
		// 3. RPop -> removes msg5 -> [msg3, msg2, msg1, msg4]
		// 4. LPop -> removes msg3 -> [msg2, msg1, msg4]
		expected := []string{"msg2", "msg1", "msg4"}
		if len(remainingMessages) != len(expected) {
			t.Errorf("Expected %d messages, got %d", len(expected), len(remainingMessages))
		}
		for i, expectedMsg := range expected {
			if i < len(remainingMessages) && remainingMessages[i] != expectedMsg {
				t.Errorf("Expected message %d to be %s, got %s", i, expectedMsg, remainingMessages[i])
			}
		}
	})

	t.Run("LPushCapped", func(t *testing.T) {
		cappedKey := "capped:list"

		// Push to capped list with limit 3
		values := []any{"item1", "item2", "item3", "item4", "item5"}
		err := cache.LPushCapped(ctx, cappedKey, 3, values...)
		if err != nil {
			t.Fatalf("Failed to push to capped list: %v", err)
		}

		// Check that only the last 3 items remain
		var items []string
		err = cache.LRange(ctx, cappedKey, 0, -1, &items)
		if err != nil {
			t.Fatalf("Failed to get range: %v", err)
		}
		if len(items) != 3 {
			t.Errorf("Expected 3 items in capped list, got %d", len(items))
		}
		// Should contain the last 3 items: item5, item4, item3
		expected := []string{"item5", "item4", "item3"}
		for i, expectedItem := range expected {
			if i < len(items) && items[i] != expectedItem {
				t.Errorf("Expected item %d to be %s, got %s", i, expectedItem, items[i])
			}
		}
	})
}

func TestCache_Serializer(t *testing.T) {
	redisConn := setupRedisConn(t)
	if redisConn == nil {
		t.Skip("Redis not available")
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

	tests := []struct {
		serializer string
		expectWork bool
	}{
		{"json", true},
		{"msgpack", true}, // Assuming msgpack serializer is available
		{"invalid", false},
	}

	for _, tt := range tests {
		t.Run(tt.serializer, func(t *testing.T) {
			cache, err := New(&Config{
				Prefix:     fmt.Sprintf("test:ser:%s:", tt.serializer),
				Serializer: tt.serializer,
			}, WithRedisConnector(redisConn), WithLogger(logger))

			if !tt.expectWork {
				if err == nil {
					t.Error("Expected error for invalid serializer")
				}
				return
			}

			if err != nil {
				// If it's a serializer availability issue, skip
				if err.Error() == "msgpack serializer not implemented yet" {
					t.Skipf("Serializer %s not available: %v", tt.serializer, err)
				}
				t.Fatalf("Failed to create cache with serializer %s: %v", tt.serializer, err)
			}

			ctx := context.Background()
			key := "test_key"
			value := map[string]interface{}{
				"string": "hello",
				"number": 42,
				"bool":   true,
			}

			// Test set and get
			err = cache.Set(ctx, key, value, time.Hour)
			if err != nil {
				t.Errorf("Failed to set value with %s: %v", tt.serializer, err)
				return
			}

			var result map[string]interface{}
			err = cache.Get(ctx, key, &result)
			if err != nil {
				t.Errorf("Failed to get value with %s: %v", tt.serializer, err)
				return
			}

			if result["string"] != "hello" {
				t.Errorf("Expected string hello, got %v", result["string"])
			}
		})
	}
}

func TestCache_Standalone(t *testing.T) {
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
		if err == nil || err.Error() != "operation not supported in standalone mode" {
			t.Errorf("Expected unsupported error for HSet, got %v", err)
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
}
