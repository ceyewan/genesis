package idgen

import (
	"context"
	"testing"

	"github.com/ceyewan/genesis/testkit"
)

// ========================================
// Sequencer 集成测试（使用 testkit）
// ========================================

func setupSequencer(t *testing.T) Sequencer {
	redis := testkit.NewRedisContainerConnector(t)
	logger := testkit.NewLogger()

	seq, err := NewSequencer(&SequencerConfig{
		KeyPrefix: "test:seq:",
		Step:      1,
	}, WithRedisConnector(redis), WithLogger(logger))
	if err != nil {
		t.Fatalf("Failed to create sequencer: %v", err)
	}

	t.Cleanup(func() {
		// 清理测试数据
		client := redis.GetClient()
		client.Keys(context.Background(), "test:seq:*")
	})

	return seq
}

func TestNewSequencer_Integration(t *testing.T) {
	redis := testkit.NewRedisContainerConnector(t)
	logger := testkit.NewLogger()

	tests := []struct {
		name        string
		cfg         *SequencerConfig
		opts        []Option
		expectError bool
	}{
		{
			name:        "nil config",
			cfg:         nil,
			expectError: true,
		},
		{
			name: "nil redis connector",
			cfg: &SequencerConfig{
				KeyPrefix: "test:",
				Step:      1,
			},
			opts:        []Option{},
			expectError: true,
		},
		{
			name: "valid config without logger",
			cfg: &SequencerConfig{
				KeyPrefix: "seq:",
				Step:      1,
			},
			opts:        []Option{WithRedisConnector(redis)},
			expectError: false,
		},
		{
			name: "valid config with logger",
			cfg: &SequencerConfig{
				KeyPrefix: "seq:",
				Step:      1,
				MaxValue:  1000000,
			},
			opts:        []Option{WithRedisConnector(redis), WithLogger(logger)},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gen, err := NewSequencer(tt.cfg, tt.opts...)
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
			if gen == nil {
				t.Error("Expected sequencer but got nil")
			}
		})
	}
}

func TestSequencer_Next_Integration(t *testing.T) {
	gen := setupSequencer(t)
	ctx := context.Background()

	t.Run("Generate sequence numbers", func(t *testing.T) {
		seq1, err := gen.Next(ctx, "user:1")
		if err != nil {
			t.Errorf("Failed to generate sequence: %v", err)
		}
		if seq1 <= 0 {
			t.Error("Expected positive sequence number")
		}

		seq2, err := gen.Next(ctx, "user:1")
		if err != nil {
			t.Errorf("Failed to generate sequence: %v", err)
		}
		if seq2 <= seq1 {
			t.Error("Expected increasing sequence numbers")
		}
	})

	t.Run("Different keys have independent sequences", func(t *testing.T) {
		seq1, err := gen.Next(ctx, "user:100")
		if err != nil {
			t.Fatalf("Failed to generate sequence: %v", err)
		}

		seq2, err := gen.Next(ctx, "user:200")
		if err != nil {
			t.Fatalf("Failed to generate sequence: %v", err)
		}

		// Both should be independent
		seq1Next, err := gen.Next(ctx, "user:100")
		if err != nil {
			t.Fatalf("Failed to generate sequence: %v", err)
		}

		if seq1Next <= seq1 {
			t.Error("Expected user:100 sequence to increment")
		}

		// seq2 is independent of seq1, just verify it's positive
		if seq2 < 1 {
			t.Error("Expected positive sequence")
		}
	})
}

func TestSequencer_NextBatch_Integration(t *testing.T) {
	redis := testkit.NewRedisContainerConnector(t)
	logger := testkit.NewLogger()

	gen, err := NewSequencer(&SequencerConfig{
		KeyPrefix: "test:batch:",
		Step:      1,
	}, WithRedisConnector(redis), WithLogger(logger))
	if err != nil {
		t.Fatalf("Failed to create sequencer: %v", err)
	}

	ctx := context.Background()

	t.Run("Batch generate sequences", func(t *testing.T) {
		seqs, err := gen.NextBatch(ctx, "batch:1", 5)
		if err != nil {
			t.Errorf("Failed to generate batch: %v", err)
		}
		if len(seqs) != 5 {
			t.Errorf("Expected 5 sequences, got %d", len(seqs))
		}

		// Check sequences are in order and consecutive
		for i := 0; i < len(seqs)-1; i++ {
			if seqs[i] >= seqs[i+1] {
				t.Errorf("Expected increasing sequences, got %d >= %d", seqs[i], seqs[i+1])
			}
		}
	})

	t.Run("Batch with step", func(t *testing.T) {
		gen2, err := NewSequencer(&SequencerConfig{
			KeyPrefix: "test:step2:",
			Step:      5,
		}, WithRedisConnector(redis))
		if err != nil {
			t.Fatalf("Failed to create sequencer with step: %v", err)
		}

		seqs, err := gen2.NextBatch(ctx, "step:new", 3)
		if err != nil {
			t.Errorf("Failed to generate batch: %v", err)
		}
		if len(seqs) != 3 {
			t.Errorf("Expected 3 sequences, got %d", len(seqs))
		}

		// First should be 5, second 10, third 15
		if seqs[0] != 5 {
			t.Errorf("Expected first sequence 5, got %d", seqs[0])
		}
		if seqs[1] != 10 {
			t.Errorf("Expected second sequence 10, got %d", seqs[1])
		}
		if seqs[2] != 15 {
			t.Errorf("Expected third sequence 15, got %d", seqs[2])
		}
	})
}

func TestSequencer_Set_Integration(t *testing.T) {
	gen := setupSequencer(t)
	ctx := context.Background()

	t.Run("Set sequence value", func(t *testing.T) {
		key := "key:set"

		// 设置初始值
		err := gen.Set(ctx, key, 100)
		if err != nil {
			t.Fatalf("Failed to set sequence: %v", err)
		}

		// 验证 Next 从设置值之后开始
		seq, err := gen.Next(ctx, key)
		if err != nil {
			t.Fatalf("Failed to get next sequence: %v", err)
		}
		if seq != 101 {
			t.Errorf("Expected sequence 101, got %d", seq)
		}
	})

	t.Run("Set negative value should fail", func(t *testing.T) {
		key := "key:neg"

		err := gen.Set(ctx, key, -1)
		if err == nil {
			t.Error("Expected error for negative value")
		}
	})

	t.Run("Set overwrites existing value", func(t *testing.T) {
		key := "key:overwrite"

		// 先设置一个值
		_ = gen.Set(ctx, key, 50)

		// 覆盖设置
		err := gen.Set(ctx, key, 200)
		if err != nil {
			t.Fatalf("Failed to overwrite sequence: %v", err)
		}

		// 验证新值
		seq, err := gen.Next(ctx, key)
		if err != nil {
			t.Fatalf("Failed to get next sequence: %v", err)
		}
		if seq != 201 {
			t.Errorf("Expected sequence 201, got %d", seq)
		}
	})

	t.Run("Set with prefix", func(t *testing.T) {
		// 验证 KeyPrefix 正确工作
		key := "key:prefix"

		err := gen.Set(ctx, key, 999)
		if err != nil {
			t.Fatalf("Failed to set with prefix: %v", err)
		}

		seq, err := gen.Next(ctx, key)
		if err != nil {
			t.Fatalf("Failed to get next: %v", err)
		}
		if seq != 1000 {
			t.Errorf("Expected 1000, got %d", seq)
		}
	})
}

func TestSequencer_SetIfNotExists_Integration(t *testing.T) {
	gen := setupSequencer(t)
	ctx := context.Background()

	t.Run("SetIfNotExists on new key", func(t *testing.T) {
		key := "key:new"

		ok, err := gen.SetIfNotExists(ctx, key, 100)
		if err != nil {
			t.Fatalf("Failed to set if not exists: %v", err)
		}
		if !ok {
			t.Error("Expected true for new key")
		}

		// 验证值已设置
		seq, err := gen.Next(ctx, key)
		if err != nil {
			t.Fatalf("Failed to get next: %v", err)
		}
		if seq != 101 {
			t.Errorf("Expected 101, got %d", seq)
		}
	})

	t.Run("SetIfNotExists on existing key", func(t *testing.T) {
		key := "key:existing"

		// 首次设置
		ok, err := gen.SetIfNotExists(ctx, key, 50)
		if err != nil {
			t.Fatalf("Failed to set first time: %v", err)
		}
		if !ok {
			t.Error("Expected true on first set")
		}

		// 再次设置应该失败
		ok, err = gen.SetIfNotExists(ctx, key, 999)
		if err != nil {
			t.Fatalf("Failed on second set: %v", err)
		}
		if ok {
			t.Error("Expected false on existing key")
		}

		// 验证原值未被覆盖
		seq, err := gen.Next(ctx, key)
		if err != nil {
			t.Fatalf("Failed to get next: %v", err)
		}
		if seq != 51 {
			t.Errorf("Expected 51 (original value+1), got %d", seq)
		}
	})

	t.Run("SetIfNotExists negative value should fail", func(t *testing.T) {
		key := "key:neg"

		ok, err := gen.SetIfNotExists(ctx, key, -10)
		if err == nil {
			t.Error("Expected error for negative value")
		}
		if ok {
			t.Error("Expected false for error case")
		}
	})

	t.Run("IM scenario: initialize only once", func(t *testing.T) {
		// 模拟 IM 系统场景：已有历史消息，最大 seq_id=100
		key := "key:im:conv"

		// 第一次初始化
		ok, err := gen.SetIfNotExists(ctx, key, 100)
		if err != nil {
			t.Fatalf("Failed to initialize: %v", err)
		}
		if !ok {
			t.Error("Expected true on first initialization")
		}

		// 模拟另一个进程也尝试初始化（应该失败）
		ok, err = gen.SetIfNotExists(ctx, key, 200)
		if err != nil {
			t.Fatalf("Failed on duplicate init: %v", err)
		}
		if ok {
			t.Error("Expected false on duplicate initialization")
		}

		// 验证后续 Next 正常工作
		seq1, _ := gen.Next(ctx, key)
		seq2, _ := gen.Next(ctx, key)

		if seq1 != 101 {
			t.Errorf("Expected 101, got %d", seq1)
		}
		if seq2 != 102 {
			t.Errorf("Expected 102, got %d", seq2)
		}
	})

	t.Run("SetIfNotExists with Next after Set", func(t *testing.T) {
		key := "key:combo"

		// 先用 SetIfNotExists 初始化
		ok, err := gen.SetIfNotExists(ctx, key, 10)
		if err != nil || !ok {
			t.Fatalf("SetIfNotExists failed: %v", err)
		}

		// 多次 Next
		for i := 0; i < 5; i++ {
			seq, err := gen.Next(ctx, key)
			if err != nil {
				t.Fatalf("Next failed: %v", err)
			}
			expected := int64(11 + i)
			if seq != expected {
				t.Errorf("Expected %d, got %d", expected, seq)
			}
		}
	})
}

// ========================================
// Allocator 集成测试（使用 testkit）
// ========================================

func TestRedisAllocator_Integration(t *testing.T) {
	redis := testkit.NewRedisContainerConnector(t)

	t.Run("Allocate successfully", func(t *testing.T) {
		ctx := context.Background()
		allocator, err := NewAllocator(&AllocatorConfig{
			Driver:    "redis",
			KeyPrefix: "test:allocator",
			MaxID:     100,
			TTL:       30,
		}, WithRedisConnector(redis))
		if err != nil {
			t.Fatalf("Failed to create allocator: %v", err)
		}

		instanceID, err := allocator.Allocate(ctx)
		if err != nil {
			t.Fatalf("Failed to allocate: %v", err)
		}
		defer allocator.Stop()

		if instanceID < 0 || instanceID >= 100 {
			t.Errorf("Expected instanceID in [0, 100), got %d", instanceID)
		}
	})

	t.Run("Multiple allocations get unique IDs", func(t *testing.T) {
		ctx := context.Background()

		alloc1, err := NewAllocator(&AllocatorConfig{
			Driver:    "redis",
			KeyPrefix: "test:unique",
			MaxID:     50,
			TTL:       30,
		}, WithRedisConnector(redis))
		if err != nil {
			t.Fatalf("Failed to create allocator: %v", err)
		}

		alloc2, err := NewAllocator(&AllocatorConfig{
			Driver:    "redis",
			KeyPrefix: "test:unique",
			MaxID:     50,
			TTL:       30,
		}, WithRedisConnector(redis))
		if err != nil {
			t.Fatalf("Failed to create allocator: %v", err)
		}

		id1, err := alloc1.Allocate(ctx)
		if err != nil {
			t.Fatalf("Failed to allocate first ID: %v", err)
		}
		defer alloc1.Stop()

		id2, err := alloc2.Allocate(ctx)
		if err != nil {
			t.Fatalf("Failed to allocate second ID: %v", err)
		}
		defer alloc2.Stop()

		if id1 == id2 {
			t.Errorf("Expected unique IDs, got both %d", id1)
		}
	})

	t.Run("Exhaust all IDs", func(t *testing.T) {
		ctx := context.Background()
		maxID := 5

		// 分配所有 ID
		allocators := make([]Allocator, 0, maxID)
		for i := 0; i < maxID; i++ {
			alloc, err := NewAllocator(&AllocatorConfig{
				Driver:    "redis",
				KeyPrefix: "test:exhaust",
				MaxID:     maxID,
				TTL:       30,
			}, WithRedisConnector(redis))
			if err != nil {
				t.Fatalf("Failed to create allocator: %v", err)
			}

			_, err = alloc.Allocate(ctx)
			if err != nil {
				t.Fatalf("Failed to allocate ID %d: %v", i, err)
			}
			allocators = append(allocators, alloc)
		}

		// 清理
		for _, alloc := range allocators {
			alloc.Stop()
		}

		// 等待 Redis 释放
		// time.Sleep(100 * time.Millisecond)

		// 再次分配应该成功（因为前面的 allocator 已经 stop）
		alloc, err := NewAllocator(&AllocatorConfig{
			Driver:    "redis",
			KeyPrefix: "test:exhaust",
			MaxID:     maxID,
			TTL:       30,
		}, WithRedisConnector(redis))
		if err != nil {
			t.Fatalf("Failed to create allocator: %v", err)
		}

		_, err = alloc.Allocate(ctx)
		if err != nil {
			t.Errorf("Expected to allocate ID after cleanup, got error: %v", err)
		}
		defer alloc.Stop()
	})
}
