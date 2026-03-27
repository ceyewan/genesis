package cache

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestDistributed_Integration(t *testing.T) {
	cache := setupTestDistributed(t, "test:dist:")
	ctx := context.Background()

	if err := cache.Set(ctx, "user:1", map[string]any{"name": "alice"}, time.Minute); err != nil {
		t.Fatalf("set failed: %v", err)
	}

	var got map[string]any
	if err := cache.Get(ctx, "user:1", &got); err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got["name"] != "alice" {
		t.Fatalf("unexpected value: %v", got)
	}
}

func TestMulti_Integration(t *testing.T) {
	dist := setupTestDistributed(t, "test:multi:")
	local, err := NewLocal(&LocalConfig{Driver: DriverOtter, MaxEntries: 128})
	if err != nil {
		t.Fatalf("Failed to create local cache: %v", err)
	}
	defer local.Close()

	multi, err := NewMulti(local, dist, &MultiConfig{
		BackfillTTL: time.Minute,
	})
	if err != nil {
		t.Fatalf("Failed to create multi cache: %v", err)
	}

	ctx := context.Background()
	key := "user:1"
	value := fmt.Sprintf("value-%s", key)

	if err := multi.Set(ctx, key, value, time.Minute); err != nil {
		t.Fatalf("set failed: %v", err)
	}

	var got string
	if err := multi.Get(ctx, key, &got); err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got != value {
		t.Fatalf("unexpected value: %s", got)
	}
}
