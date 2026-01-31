package idgen

import (
	"strconv"
	"testing"
)

// ========================================
// UUID 单元测试
// ========================================

func TestUUID_Unit(t *testing.T) {
	t.Run("Generate UUID v7", func(t *testing.T) {
		uuid := UUID()
		if uuid == "" {
			t.Error("Expected non-empty UUID")
		}
		if len(uuid) != 36 {
			t.Errorf("Expected UUID length 36, got %d", len(uuid))
		}
	})

	t.Run("Generate unique UUIDs", func(t *testing.T) {
		uuid1 := UUID()
		uuid2 := UUID()
		if uuid1 == uuid2 {
			t.Error("Expected different UUIDs")
		}
	})

	t.Run("UUID format validation", func(t *testing.T) {
		uuid := UUID()
		// UUID v7 格式: xxxxxxxx-xxxx-7xxx-yxxx-xxxxxxxxxxxx
		if len(uuid) != 36 {
			t.Errorf("Expected UUID length 36, got %d", len(uuid))
		}
		if uuid[14] != '7' {
			t.Errorf("Expected UUID v7 version at position 14, got %c", uuid[14])
		}
	})
}

// ========================================
// Snowflake 单元测试
// ========================================

func TestNewGenerator_Unit(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *GeneratorConfig
		expectError bool
	}{
		{
			name: "valid workerID",
			cfg: &GeneratorConfig{
				WorkerID: 1,
			},
			expectError: false,
		},
		{
			name: "workerID zero",
			cfg: &GeneratorConfig{
				WorkerID: 0,
			},
			expectError: false,
		},
		{
			name: "workerID max",
			cfg: &GeneratorConfig{
				WorkerID: 1023,
			},
			expectError: false,
		},
		{
			name: "with datacenterID",
			cfg: &GeneratorConfig{
				WorkerID:     30, // Must be <= 31 when using DatacenterID
				DatacenterID: 1,
			},
			expectError: false,
		},
		{
			name:        "nil config",
			cfg:         nil,
			expectError: true,
		},
		{
			name: "negative workerID",
			cfg: &GeneratorConfig{
				WorkerID: -1,
			},
			expectError: true,
		},
		{
			name: "workerID too large",
			cfg: &GeneratorConfig{
				WorkerID: 1024,
			},
			expectError: true,
		},
		{
			name: "workerID overflow with datacenterID",
			cfg: &GeneratorConfig{
				WorkerID:     100, // > 31
				DatacenterID: 1,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sf, err := NewGenerator(tt.cfg)
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
			if sf == nil {
				t.Error("Expected generator but got nil")
			}
		})
	}
}

func TestSnowflake_Next_Unit(t *testing.T) {
	sf, err := NewGenerator(&GeneratorConfig{WorkerID: 1})
	if err != nil {
		t.Fatalf("Failed to create Snowflake: %v", err)
	}

	t.Run("Generate Snowflake ID", func(t *testing.T) {
		id := sf.Next()

		if id == 0 {
			t.Error("Expected non-zero ID")
		}
		if id < 0 {
			t.Error("Expected positive ID")
		}
	})

	t.Run("Generate unique IDs", func(t *testing.T) {
		id1 := sf.Next()

		id2 := sf.Next()

		if id1 == id2 {
			t.Error("Expected different IDs")
		}
		if id1 >= id2 {
			t.Error("Expected IDs to be in increasing order")
		}
	})

	t.Run("NextString returns string", func(t *testing.T) {
		idStr := sf.NextString()
		if idStr == "" {
			t.Error("Expected non-empty string")
		}
		// Should be parseable as int64
		if _, err := strconv.ParseInt(idStr, 10, 64); err != nil {
			t.Errorf("Failed to parse ID as int64: %v", err)
		}
	})
}

func TestSnowflake_WithLargeDatacenterID_Unit(t *testing.T) {
	sf, err := NewGenerator(&GeneratorConfig{
		WorkerID:     5,
		DatacenterID: 15,
	})
	if err != nil {
		t.Fatalf("Failed to create Snowflake with datacenterID: %v", err)
	}

	id := sf.Next()
	if id <= 0 {
		t.Error("Expected positive ID with datacenterID")
	}
}

func TestSnowflake_Monotonicity_Unit(t *testing.T) {
	sf, err := NewGenerator(&GeneratorConfig{WorkerID: 1})
	if err != nil {
		t.Fatalf("Failed to create Snowflake: %v", err)
	}

	// 生成大量 ID 验证单调性
	lastID := sf.Next()
	for i := 0; i < 10000; i++ {
		id := sf.Next()
		if id <= lastID {
			t.Errorf("ID monotonicity violated at iteration %d: %d <= %d", i, id, lastID)
			return
		}
		lastID = id
	}
}

func TestSnowflake_Uniqueness_Unit(t *testing.T) {
	sf, err := NewGenerator(&GeneratorConfig{WorkerID: 1})
	if err != nil {
		t.Fatalf("Failed to create Snowflake: %v", err)
	}

	// 使用 map 验证唯一性
	seen := make(map[int64]bool)
	for i := 0; i < 100000; i++ {
		id := sf.Next()
		if seen[id] {
			t.Errorf("Duplicate ID generated at iteration %d: %d", i, id)
			return
		}
		seen[id] = true
	}
}

// ========================================
// Sequencer 配置单元测试
// ========================================

func TestSequencerConfig_Unit(t *testing.T) {
	t.Run("nil config returns error", func(t *testing.T) {
		_, err := NewSequencer(nil)
		if err == nil {
			t.Error("Expected error for nil config")
		}
	})

	t.Run("nil redis connector returns error", func(t *testing.T) {
		_, err := NewSequencer(&SequencerConfig{
			KeyPrefix: "test:",
			Step:      1,
		})
		if err == nil {
			t.Error("Expected error for nil redis connector")
		}
	})

	t.Run("unsupported driver returns error", func(t *testing.T) {
		_, err := NewSequencer(&SequencerConfig{
			Driver:    "unsupported",
			KeyPrefix: "test:",
			Step:      1,
		})
		if err == nil {
			t.Error("Expected error for unsupported driver")
		}
	})
}

// ========================================
// Allocator 配置单元测试
// ========================================

func TestAllocatorConfig_Unit(t *testing.T) {
	t.Run("nil config returns error", func(t *testing.T) {
		_, err := NewAllocator(nil)
		if err == nil {
			t.Error("Expected error for nil config")
		}
	})

	t.Run("nil redis connector returns error", func(t *testing.T) {
		_, err := NewAllocator(&AllocatorConfig{
			Driver:    "redis",
			KeyPrefix: "test:",
		})
		if err == nil {
			t.Error("Expected error for nil redis connector")
		}
	})

	t.Run("unsupported driver returns error", func(t *testing.T) {
		_, err := NewAllocator(&AllocatorConfig{
			Driver:    "unsupported",
			KeyPrefix: "test:",
		})
		if err == nil {
			t.Error("Expected error for unsupported driver")
		}
	})
}

// ========================================
// 错误码单元测试
// ========================================

func TestErrorCodes_Unit(t *testing.T) {
	t.Run("ErrInvalidInput is defined", func(t *testing.T) {
		if ErrInvalidInput == nil {
			t.Error("ErrInvalidInput should be defined")
		}
	})

	t.Run("ErrConnectorNil is defined", func(t *testing.T) {
		if ErrConnectorNil == nil {
			t.Error("ErrConnectorNil should be defined")
		}
	})
}

// ========================================
// 选项模式单元测试
// ========================================

func TestOptions_Unit(t *testing.T) {
	t.Run("WithLogger creates option", func(t *testing.T) {
		opt := WithLogger(nil)
		if opt == nil {
			t.Error("WithLogger should return a non-nil option")
		}
		// 应用选项不应 panic
		opts := &options{}
		opt(opts)
	})

	t.Run("WithRedisConnector creates option", func(t *testing.T) {
		opt := WithRedisConnector(nil)
		if opt == nil {
			t.Error("WithRedisConnector should return a non-nil option")
		}
		// 应用选项不应 panic
		opts := &options{}
		opt(opts)
	})

	t.Run("WithMeter creates option", func(t *testing.T) {
		opt := WithMeter(nil)
		if opt == nil {
			t.Error("WithMeter should return a non-nil option")
		}
		// 应用选项不应 panic
		opts := &options{}
		opt(opts)
	})
}
