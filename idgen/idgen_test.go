package idgen

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/metrics"
)

func TestConstructorsRejectUnconnectedConnectors(t *testing.T) {
	t.Parallel()

	redisConn, err := connector.NewRedis(&connector.RedisConfig{Addr: "127.0.0.1:6379"})
	require.NoError(t, err)
	_, err = NewSequencer(&SequencerConfig{Driver: DriverRedis}, WithRedisConnector(redisConn))
	require.ErrorIs(t, err, connector.ErrClientNil)
	_, err = NewAllocator(&AllocatorConfig{Driver: DriverRedis}, WithRedisConnector(redisConn))
	require.ErrorIs(t, err, connector.ErrClientNil)

	etcdConn, err := connector.NewEtcd(&connector.EtcdConfig{Endpoints: []string{"127.0.0.1:2379"}})
	require.NoError(t, err)
	_, err = NewAllocator(&AllocatorConfig{Driver: DriverEtcd}, WithEtcdConnector(etcdConn))
	require.ErrorIs(t, err, connector.ErrClientNil)
}

type testCounter struct {
	incCount int
	addTotal float64
}

func (c *testCounter) Inc(_ context.Context, _ ...metrics.Label) {
	c.incCount++
}

func (c *testCounter) Add(_ context.Context, val float64, _ ...metrics.Label) {
	c.addTotal += val
}

type testMeter struct {
	counter metrics.Counter
}

func (m *testMeter) Counter(name, desc string, opts ...metrics.MetricOption) (metrics.Counter, error) {
	return m.counter, nil
}

func (m *testMeter) Gauge(name, desc string, opts ...metrics.MetricOption) (metrics.Gauge, error) {
	return nil, nil
}

func (m *testMeter) Histogram(name, desc string, opts ...metrics.MetricOption) (metrics.Histogram, error) {
	return nil, nil
}

func (m *testMeter) Shutdown(ctx context.Context) error {
	return nil
}

// ========================================
// UUID 单元测试
// ========================================

func TestUUID_Unit(t *testing.T) {
	t.Run("Generate UUID v7", func(t *testing.T) {
		uuid, err := UUID()
		require.NoError(t, err)
		if uuid == "" {
			t.Error("Expected non-empty UUID")
		}
		if len(uuid) != 36 {
			t.Errorf("Expected UUID length 36, got %d", len(uuid))
		}
	})

	t.Run("Generate unique UUIDs", func(t *testing.T) {
		uuid1, err := UUID()
		require.NoError(t, err)
		uuid2, err := UUID()
		require.NoError(t, err)
		if uuid1 == uuid2 {
			t.Error("Expected different UUIDs")
		}
	})

	t.Run("UUID format validation", func(t *testing.T) {
		uuid, err := UUID()
		require.NoError(t, err)
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
				Mode:     GeneratorModeMultiDC,
				WorkerID: 1,
			},
			expectError: false,
		},
		{
			name: "workerID zero",
			cfg: &GeneratorConfig{
				Mode:     GeneratorModeMultiDC,
				WorkerID: 0,
			},
			expectError: false,
		},
		{
			name: "workerID max in multi dc",
			cfg: &GeneratorConfig{
				Mode:     GeneratorModeMultiDC,
				WorkerID: 31,
			},
			expectError: false,
		},
		{
			name: "workerID max in single dc",
			cfg: &GeneratorConfig{
				Mode:     GeneratorModeSingleDC,
				WorkerID: 1023,
			},
			expectError: false,
		},
		{
			name: "with datacenterID",
			cfg: &GeneratorConfig{
				Mode:         GeneratorModeMultiDC,
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
				Mode:     GeneratorModeMultiDC,
				WorkerID: -1,
			},
			expectError: true,
		},
		{
			name: "workerID too large in multi dc",
			cfg: &GeneratorConfig{
				Mode:     GeneratorModeMultiDC,
				WorkerID: 32,
			},
			expectError: true,
		},
		{
			name: "workerID too large in single dc",
			cfg: &GeneratorConfig{
				Mode:     GeneratorModeSingleDC,
				WorkerID: 1024,
			},
			expectError: true,
		},
		{
			name: "single dc requires zero datacenter",
			cfg: &GeneratorConfig{
				Mode:         GeneratorModeSingleDC,
				WorkerID:     1,
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
	sf, err := NewGenerator(&GeneratorConfig{Mode: GeneratorModeMultiDC, WorkerID: 1})
	if err != nil {
		t.Fatalf("Failed to create Snowflake: %v", err)
	}

	t.Run("Generate Snowflake ID", func(t *testing.T) {
		id, err := sf.Next()
		require.NoError(t, err)

		if id == 0 {
			t.Error("Expected non-zero ID")
		}
		if id < 0 {
			t.Error("Expected positive ID")
		}
	})

	t.Run("Generate unique IDs", func(t *testing.T) {
		id1, err := sf.Next()
		require.NoError(t, err)

		id2, err := sf.Next()
		require.NoError(t, err)

		if id1 == id2 {
			t.Error("Expected different IDs")
		}
		if id1 >= id2 {
			t.Error("Expected IDs to be in increasing order")
		}
	})

	t.Run("NextString returns string", func(t *testing.T) {
		idStr, err := sf.NextString()
		require.NoError(t, err)
		if idStr == "" {
			t.Error("Expected non-empty string")
		}
		// Should be parseable as int64
		if _, parseErr := strconv.ParseInt(idStr, 10, 64); parseErr != nil {
			t.Errorf("Failed to parse ID as int64: %v", parseErr)
		}
	})
}

func TestSnowflake_WithLargeDatacenterID_Unit(t *testing.T) {
	sf, err := NewGenerator(&GeneratorConfig{
		Mode:         GeneratorModeMultiDC,
		WorkerID:     5,
		DatacenterID: 15,
	})
	if err != nil {
		t.Fatalf("Failed to create Snowflake with datacenterID: %v", err)
	}

	id, err := sf.Next()
	require.NoError(t, err)
	if id <= 0 {
		t.Error("Expected positive ID with datacenterID")
	}
}

func TestParseGeneratorID_RoundTrip_MultiDC_Unit(t *testing.T) {
	t.Parallel()

	gen, err := NewGenerator(&GeneratorConfig{
		Mode:         GeneratorModeMultiDC,
		WorkerID:     17,
		DatacenterID: 9,
	})
	require.NoError(t, err)

	id, err := gen.Next()
	require.NoError(t, err)
	require.Positive(t, id)

	timestamp, datacenterID, workerID, sequence, err := ParseGeneratorID(id, GeneratorModeMultiDC)
	require.NoError(t, err)
	require.GreaterOrEqual(t, timestamp, int64(1704067200000))
	require.EqualValues(t, 9, datacenterID)
	require.EqualValues(t, 17, workerID)
	require.GreaterOrEqual(t, sequence, int64(0))
}

func TestParseGeneratorID_RoundTrip_SingleDC_Unit(t *testing.T) {
	t.Parallel()

	gen, err := NewGenerator(&GeneratorConfig{
		Mode:     GeneratorModeSingleDC,
		WorkerID: 513,
	})
	require.NoError(t, err)

	id, err := gen.Next()
	require.NoError(t, err)
	require.Positive(t, id)

	timestamp, datacenterID, workerID, sequence, err := ParseGeneratorID(id, GeneratorModeSingleDC)
	require.NoError(t, err)
	require.GreaterOrEqual(t, timestamp, int64(1704067200000))
	require.EqualValues(t, 0, datacenterID)
	require.EqualValues(t, 513, workerID)
	require.GreaterOrEqual(t, sequence, int64(0))
}

func TestSnowflake_NextString_CountsMetric_Unit(t *testing.T) {
	t.Parallel()

	counter := &testCounter{}
	gen, err := NewGenerator(
		&GeneratorConfig{WorkerID: 1},
		WithMeter(&testMeter{counter: counter}),
	)
	require.NoError(t, err)

	idStr, err := gen.NextString()
	require.NoError(t, err)
	require.NotEmpty(t, idStr)
	require.Equal(t, 1, counter.incCount)
}

func TestSnowflake_DefaultMode_MultiDC_Unit(t *testing.T) {
	t.Parallel()

	gen, err := NewGenerator(&GeneratorConfig{WorkerID: 1})
	require.NoError(t, err)

	id, err := gen.Next()
	require.NoError(t, err)
	_, datacenterID, workerID, _, err := ParseGeneratorID(id, GeneratorModeMultiDC)
	require.NoError(t, err)
	require.EqualValues(t, 0, datacenterID)
	require.EqualValues(t, 1, workerID)
}

func TestNewGenerator_CopiesConfig_Unit(t *testing.T) {
	t.Parallel()

	cfg := &GeneratorConfig{WorkerID: 1}
	gen, err := NewGenerator(cfg)
	require.NoError(t, err)
	require.Empty(t, cfg.Mode)

	cfg.WorkerID = 31
	id, err := gen.Next()
	require.NoError(t, err)
	_, _, workerID, _, err := ParseGeneratorID(id, GeneratorModeMultiDC)
	require.NoError(t, err)
	require.EqualValues(t, 1, workerID)
}

func TestSnowflake_ClockRollback_Unit(t *testing.T) {
	t.Parallel()

	now := time.UnixMilli(genesisEpochMilli + 10_000)
	sf := &snowflake{
		mode:     GeneratorModeMultiDC,
		workerID: 1,
		now:      func() time.Time { return now },
		sleep:    func(time.Duration) {},
	}

	sf.state.Store(uint64(12_000) << 12)
	_, err := sf.nextInt64()
	require.ErrorIs(t, err, ErrClockBackwards)

	sf.state.Store((uint64(10_004) << 12) | 7)
	id, err := sf.nextInt64()
	require.NoError(t, err)
	timestamp, _, _, sequence, err := ParseGeneratorID(id, GeneratorModeMultiDC)
	require.NoError(t, err)
	require.Equal(t, genesisEpochMilli+10_004, timestamp)
	require.EqualValues(t, 8, sequence)
	require.False(t, errors.Is(err, ErrClockBackwards))
}

func TestParseGeneratorIDRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	_, _, _, _, err := ParseGeneratorID(1, GeneratorMode("unknown"))
	require.ErrorIs(t, err, ErrInvalidInput)
	_, _, _, _, err = ParseGeneratorID(-1, GeneratorModeMultiDC)
	require.ErrorIs(t, err, ErrInvalidInput)
}

func TestSnowflake_Monotonicity_Unit(t *testing.T) {
	sf, err := NewGenerator(&GeneratorConfig{Mode: GeneratorModeMultiDC, WorkerID: 1})
	if err != nil {
		t.Fatalf("Failed to create Snowflake: %v", err)
	}

	// 生成大量 ID 验证单调性
	lastID, err := sf.Next()
	require.NoError(t, err)
	for i := range 10000 {
		id, err := sf.Next()
		require.NoError(t, err)
		if id <= lastID {
			t.Errorf("ID monotonicity violated at iteration %d: %d <= %d", i, id, lastID)
			return
		}
		lastID = id
	}
}

func TestSnowflake_Uniqueness_Unit(t *testing.T) {
	sf, err := NewGenerator(&GeneratorConfig{Mode: GeneratorModeMultiDC, WorkerID: 1})
	if err != nil {
		t.Fatalf("Failed to create Snowflake: %v", err)
	}

	// 使用 map 验证唯一性
	seen := make(map[int64]bool)
	for i := range 100000 {
		id, err := sf.Next()
		require.NoError(t, err)
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

	t.Run("negative TTL returns error", func(t *testing.T) {
		_, err := NewSequencer(&SequencerConfig{TTL: -time.Second})
		require.Error(t, err)
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

	t.Run("negative TTL returns error", func(t *testing.T) {
		_, err := NewAllocator(&AllocatorConfig{TTL: -time.Second})
		require.Error(t, err)
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
