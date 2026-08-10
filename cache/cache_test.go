package cache

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/xerrors"
)

func TestNewDistributed_Unit(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "info", Output: "stdout"})

	t.Run("nil distributed config", func(t *testing.T) {
		_, err := NewDistributed(nil)
		if !xerrors.Is(err, ErrInvalidConfig) {
			t.Fatalf("expected ErrInvalidConfig, got %v", err)
		}
	})

	t.Run("nil local config", func(t *testing.T) {
		_, err := NewLocal(nil)
		if !xerrors.Is(err, ErrInvalidConfig) {
			t.Fatalf("expected ErrInvalidConfig, got %v", err)
		}
	})

	t.Run("missing connector", func(t *testing.T) {
		_, err := NewDistributed(&DistributedConfig{Driver: DriverRedis})
		if !xerrors.Is(err, ErrRedisConnectorRequired) {
			t.Fatalf("expected redis connector error, got %v", err)
		}
	})

	t.Run("unconnected connector", func(t *testing.T) {
		redisConn, err := connector.NewRedis(&connector.RedisConfig{Addr: "127.0.0.1:6379"})
		if err != nil {
			t.Fatal(err)
		}
		_, err = NewDistributed(&DistributedConfig{Driver: DriverRedis}, WithRedisConnector(redisConn))
		if !xerrors.Is(err, connector.ErrClientNil) {
			t.Fatalf("expected ErrClientNil, got %v", err)
		}
	})

	t.Run("valid local config", func(t *testing.T) {
		local, err := NewLocal(&LocalConfig{Driver: DriverOtter, MaxEntries: 32}, WithLogger(logger))
		if err != nil {
			t.Fatalf("failed to create local cache: %v", err)
		}
		defer local.Close()
	})
}

func TestNegativeDurationsAreRejected(t *testing.T) {
	t.Run("distributed default TTL", func(t *testing.T) {
		_, err := NewDistributed(&DistributedConfig{DefaultTTL: -time.Second})
		if !xerrors.Is(err, ErrInvalidTTL) {
			t.Fatalf("expected ErrInvalidTTL, got %v", err)
		}
	})

	t.Run("local default TTL", func(t *testing.T) {
		_, err := NewLocal(&LocalConfig{DefaultTTL: -time.Second})
		if !xerrors.Is(err, ErrInvalidTTL) {
			t.Fatalf("expected ErrInvalidTTL, got %v", err)
		}
	})

	t.Run("multi TTLs", func(t *testing.T) {
		local, err := NewLocal(&LocalConfig{})
		if err != nil {
			t.Fatal(err)
		}
		defer local.Close()

		for _, cfg := range []*MultiConfig{
			{LocalTTL: -time.Second},
			{BackfillTTL: -time.Second},
		} {
			_, err = NewMulti(local, newMockDistributed(), cfg)
			if !xerrors.Is(err, ErrInvalidTTL) {
				t.Fatalf("expected ErrInvalidTTL for %+v, got %v", cfg, err)
			}
		}
	})

	t.Run("operation TTL", func(t *testing.T) {
		local, err := NewLocal(&LocalConfig{})
		if err != nil {
			t.Fatal(err)
		}
		defer local.Close()
		if err := local.Set(context.Background(), "key", "value", -time.Second); !xerrors.Is(err, ErrInvalidTTL) {
			t.Fatalf("expected ErrInvalidTTL, got %v", err)
		}
	})
}

func TestLocal_Unit(t *testing.T) {
	local, err := NewLocal(&LocalConfig{Driver: DriverOtter, MaxEntries: 128})
	if err != nil {
		t.Fatalf("failed to create local cache: %v", err)
	}
	defer local.Close()

	ctx := context.Background()
	key := "local:key"
	value := map[string]string{"name": "alice"}

	if err := local.Set(ctx, key, value, time.Minute); err != nil {
		t.Fatalf("set failed: %v", err)
	}

	var got map[string]string
	if err := local.Get(ctx, key, &got); err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got["name"] != "alice" {
		t.Fatalf("unexpected value: %v", got)
	}

	value["name"] = "bob"
	if err := local.Get(ctx, key, &got); err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got["name"] != "alice" {
		t.Fatalf("expected value semantics, got %v", got["name"])
	}
}

func TestMulti_Unit(t *testing.T) {
	ctx := context.Background()

	local, err := NewLocal(&LocalConfig{Driver: DriverOtter, MaxEntries: 128})
	if err != nil {
		t.Fatalf("failed to create local cache: %v", err)
	}
	defer local.Close()

	remote := newMockDistributed()

	multi, err := NewMulti(local, remote, &MultiConfig{BackfillTTL: time.Minute})
	if err != nil {
		t.Fatalf("failed to create multi cache: %v", err)
	}

	if err := multi.Set(ctx, "k1", "v1", time.Minute); err != nil {
		t.Fatalf("set failed: %v", err)
	}

	var got string
	if err := multi.Get(ctx, "k1", &got); err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got != "v1" {
		t.Fatalf("unexpected value: %s", got)
	}
}

type mockDistributed struct {
	data map[string]any
}

func newMockDistributed() *mockDistributed {
	return &mockDistributed{data: make(map[string]any)}
}

func (m *mockDistributed) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	m.data[key] = value
	return nil
}

func (m *mockDistributed) Get(ctx context.Context, key string, dest any) error {
	v, ok := m.data[key]
	if !ok {
		return ErrMiss
	}
	switch d := dest.(type) {
	case *string:
		*d = v.(string)
		return nil
	default:
		return nil
	}
}

func (m *mockDistributed) Delete(ctx context.Context, key string) error {
	delete(m.data, key)
	return nil
}

func (m *mockDistributed) Has(ctx context.Context, key string) (bool, error) {
	_, ok := m.data[key]
	return ok, nil
}

func (m *mockDistributed) Expire(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	_, ok := m.data[key]
	return ok, nil
}

func (m *mockDistributed) Close() error { return nil }
func (m *mockDistributed) HSet(ctx context.Context, key, field string, value any) error {
	return errMockOperationUnsupported
}

func (m *mockDistributed) HGet(ctx context.Context, key, field string, dest any) error {
	return errMockOperationUnsupported
}

func (m *mockDistributed) HGetAll(ctx context.Context, key string, destMap any) error {
	return errMockOperationUnsupported
}

func (m *mockDistributed) HDel(ctx context.Context, key string, fields ...string) error {
	return errMockOperationUnsupported
}

func (m *mockDistributed) HIncrBy(ctx context.Context, key, field string, increment int64) (int64, error) {
	return 0, errMockOperationUnsupported
}

func (m *mockDistributed) ZAdd(ctx context.Context, key string, score float64, member any) error {
	return errMockOperationUnsupported
}

func (m *mockDistributed) ZRem(ctx context.Context, key string, members ...any) error {
	return errMockOperationUnsupported
}

func (m *mockDistributed) ZScore(ctx context.Context, key string, member any) (float64, error) {
	return 0, errMockOperationUnsupported
}

func (m *mockDistributed) ZRange(ctx context.Context, key string, start, stop int64, destSlice any) error {
	return errMockOperationUnsupported
}

func (m *mockDistributed) ZRevRange(ctx context.Context, key string, start, stop int64, destSlice any) error {
	return errMockOperationUnsupported
}

func (m *mockDistributed) ZRangeByScore(ctx context.Context, key string, min, max float64, destSlice any) error {
	return errMockOperationUnsupported
}

func (m *mockDistributed) MGet(ctx context.Context, keys []string, destSlice any) error {
	return errMockOperationUnsupported
}

func (m *mockDistributed) MSet(ctx context.Context, items map[string]any, ttl time.Duration) error {
	return errMockOperationUnsupported
}
func (m *mockDistributed) RawClient() *redis.Client { return nil }
