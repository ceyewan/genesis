package cache

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

//go:fix inline
func boolPtr(b bool) *bool {
	v := b
	return &v
}

// mockLocalForMulti 用于测试 Multi 的本地缓存 mock
type mockLocalForMulti struct {
	data       map[string]any
	lastSetTTL time.Duration
	lastExpTTL time.Duration
	failGet    atomic.Bool
	failSet    atomic.Bool
	failDel    atomic.Bool
	failHas    atomic.Bool
	failExpire atomic.Bool
}

func newMockLocalForMulti() *mockLocalForMulti {
	return &mockLocalForMulti{data: make(map[string]any)}
}

func (m *mockLocalForMulti) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	if m.failSet.Load() {
		return errors.New("local set error")
	}
	m.lastSetTTL = ttl
	normalized, err := normalizeMockValue(value)
	if err != nil {
		return err
	}
	m.data[key] = normalized
	return nil
}

func (m *mockLocalForMulti) Get(ctx context.Context, key string, dest any) error {
	if m.failGet.Load() {
		return errors.New("local get error")
	}
	v, ok := m.data[key]
	if !ok {
		return ErrMiss
	}
	return assignMockValue(v, dest)
}

func (m *mockLocalForMulti) Delete(ctx context.Context, key string) error {
	if m.failDel.Load() {
		return errors.New("local delete error")
	}
	delete(m.data, key)
	return nil
}

func (m *mockLocalForMulti) Has(ctx context.Context, key string) (bool, error) {
	if m.failHas.Load() {
		return false, errors.New("local has error")
	}
	_, ok := m.data[key]
	return ok, nil
}

func (m *mockLocalForMulti) Expire(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if m.failExpire.Load() {
		return false, errors.New("local expire error")
	}
	m.lastExpTTL = ttl
	_, ok := m.data[key]
	return ok, nil
}

func (m *mockLocalForMulti) Close() error {
	return nil
}

// mockKVForMulti 用于测试 Multi 的 KV mock（简化实现）
type mockKVForMulti struct {
	data       map[string]any
	failGet    atomic.Bool
	failSet    atomic.Bool
	failDel    atomic.Bool
	failHas    atomic.Bool
	failExpire atomic.Bool
}

func newMockKVForMulti() *mockKVForMulti {
	return &mockKVForMulti{data: make(map[string]any)}
}

func (m *mockKVForMulti) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	if m.failSet.Load() {
		return errors.New("remote set error")
	}
	normalized, err := normalizeMockValue(value)
	if err != nil {
		return err
	}
	m.data[key] = normalized
	return nil
}

func (m *mockKVForMulti) Get(ctx context.Context, key string, dest any) error {
	if m.failGet.Load() {
		return errors.New("remote get error")
	}
	v, ok := m.data[key]
	if !ok {
		return ErrMiss
	}
	return assignMockValue(v, dest)
}

func (m *mockKVForMulti) Delete(ctx context.Context, key string) error {
	if m.failDel.Load() {
		return errors.New("remote delete error")
	}
	delete(m.data, key)
	return nil
}

func (m *mockKVForMulti) Has(ctx context.Context, key string) (bool, error) {
	if m.failHas.Load() {
		return false, errors.New("remote has error")
	}
	_, ok := m.data[key]
	return ok, nil
}

func (m *mockKVForMulti) Expire(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if m.failExpire.Load() {
		return false, errors.New("remote expire error")
	}
	_, ok := m.data[key]
	return ok, nil
}

func (m *mockKVForMulti) Close() error {
	return nil
}

// 以下是 Distributed 接口需要但未实现的方法（返回 ErrNotSupported）

func (m *mockKVForMulti) HSet(ctx context.Context, key, field string, value any) error {
	return ErrNotSupported
}
func (m *mockKVForMulti) HGet(ctx context.Context, key, field string, dest any) error {
	return ErrNotSupported
}
func (m *mockKVForMulti) HGetAll(ctx context.Context, key string, destMap any) error {
	return ErrNotSupported
}
func (m *mockKVForMulti) HDel(ctx context.Context, key string, fields ...string) error {
	return ErrNotSupported
}
func (m *mockKVForMulti) HIncrBy(ctx context.Context, key, field string, increment int64) (int64, error) {
	return 0, ErrNotSupported
}
func (m *mockKVForMulti) ZAdd(ctx context.Context, key string, score float64, member any) error {
	return ErrNotSupported
}
func (m *mockKVForMulti) ZRem(ctx context.Context, key string, members ...any) error {
	return ErrNotSupported
}
func (m *mockKVForMulti) ZScore(ctx context.Context, key string, member any) (float64, error) {
	return 0, ErrNotSupported
}
func (m *mockKVForMulti) ZRange(ctx context.Context, key string, start, stop int64, destSlice any) error {
	return ErrNotSupported
}
func (m *mockKVForMulti) ZRevRange(ctx context.Context, key string, start, stop int64, destSlice any) error {
	return ErrNotSupported
}
func (m *mockKVForMulti) ZRangeByScore(ctx context.Context, key string, min, max float64, destSlice any) error {
	return ErrNotSupported
}
func (m *mockKVForMulti) MGet(ctx context.Context, keys []string, destSlice any) error {
	return ErrNotSupported
}
func (m *mockKVForMulti) MSet(ctx context.Context, items map[string]any, ttl time.Duration) error {
	return ErrNotSupported
}
func (m *mockKVForMulti) RawClient() any {
	return nil
}

func normalizeMockValue(value any) (any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}

	var normalized any
	if err := json.Unmarshal(data, &normalized); err != nil {
		return nil, err
	}

	return normalized, nil
}

func assignMockValue(value any, dest any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, dest)
}

// TestMulti_KV_Integration 测试多级缓存的 KV 操作
func TestMulti_KV_Integration(t *testing.T) {
	local := newMockLocalForMulti()
	remote := newMockKVForMulti()

	multi, err := NewMulti(local, remote, &MultiConfig{
		BackfillTTL: time.Minute,
	})
	require.NoError(t, err)
	ctx := context.Background()

	t.Run("Set writes to both local and remote", func(t *testing.T) {
		err := multi.Set(ctx, "key1", "value1", time.Minute)
		require.NoError(t, err)

		// 验证 remote 中有值
		var gotRemote string
		err = remote.Get(ctx, "key1", &gotRemote)
		require.NoError(t, err)
		require.Equal(t, "value1", gotRemote)

		// 验证 local 中有值
		var gotLocal string
		err = local.Get(ctx, "key1", &gotLocal)
		require.NoError(t, err)
		require.Equal(t, "value1", gotLocal)
	})

	t.Run("Get hits local cache first", func(t *testing.T) {
		// 先设置
		err := multi.Set(ctx, "key2", "value2", time.Minute)
		require.NoError(t, err)

		// 获取应该命中 local
		var got string
		err = multi.Get(ctx, "key2", &got)
		require.NoError(t, err)
		require.Equal(t, "value2", got)
	})

	t.Run("Get misses local but hits remote and backfills", func(t *testing.T) {
		// 直接写入 remote（绕过 local）
		err := remote.Set(ctx, "key3", "value3", time.Minute)
		require.NoError(t, err)

		// 获取应该 miss local, hit remote, 然后 backfill local
		var got string
		err = multi.Get(ctx, "key3", &got)
		require.NoError(t, err)
		require.Equal(t, "value3", got)

		// 验证 local 已经被回填
		var gotLocal string
		err = local.Get(ctx, "key3", &gotLocal)
		require.NoError(t, err)
		require.Equal(t, "value3", gotLocal)
	})

	t.Run("Get returns ErrMiss when both caches miss", func(t *testing.T) {
		var got string
		err := multi.Get(ctx, "nonexistent", &got)
		require.ErrorIs(t, err, ErrMiss)
	})

	t.Run("Delete removes from both caches", func(t *testing.T) {
		err := multi.Set(ctx, "key4", "value4", time.Minute)
		require.NoError(t, err)

		err = multi.Delete(ctx, "key4")
		require.NoError(t, err)

		// 验证 remote 中已删除
		var gotRemote string
		err = remote.Get(ctx, "key4", &gotRemote)
		require.ErrorIs(t, err, ErrMiss)

		// 验证 local 中已删除
		var gotLocal string
		err = local.Get(ctx, "key4", &gotLocal)
		require.ErrorIs(t, err, ErrMiss)
	})

	t.Run("Has checks local first then remote", func(t *testing.T) {
		err := multi.Set(ctx, "key5", "value5", time.Minute)
		require.NoError(t, err)

		ok, err := multi.Has(ctx, "key5")
		require.NoError(t, err)
		require.True(t, ok)

		ok, err = multi.Has(ctx, "nonexistent")
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("Expire updates both caches", func(t *testing.T) {
		err := multi.Set(ctx, "key6", "value6", time.Minute)
		require.NoError(t, err)

		ok, err := multi.Expire(ctx, "key6", 10*time.Minute)
		require.NoError(t, err)
		require.True(t, ok)

		ok, err = multi.Expire(ctx, "nonexistent", 10*time.Minute)
		require.NoError(t, err)
		require.False(t, ok)
	})
}

// TestMulti_ReadThrough 测试 Read-through 回填行为
func TestMulti_ReadThrough(t *testing.T) {
	local := newMockLocalForMulti()
	remote := newMockKVForMulti()

	multi, err := NewMulti(local, remote, &MultiConfig{
		BackfillTTL: 5 * time.Minute,
	})
	require.NoError(t, err)
	ctx := context.Background()

	t.Run("First Get misses local, hits remote, backfills local", func(t *testing.T) {
		// 清空 local
		local.data = make(map[string]any)

		// 写入 remote
		err := remote.Set(ctx, "user:1", map[string]string{"name": "alice"}, time.Minute)
		require.NoError(t, err)

		// 第一次 Get 应该 miss local, hit remote
		var got map[string]string
		err = multi.Get(ctx, "user:1", &got)
		require.NoError(t, err)
		require.Equal(t, "alice", got["name"])

		// 验证 local 已经被回填
		var gotLocal map[string]string
		err = local.Get(ctx, "user:1", &gotLocal)
		require.NoError(t, err)
		require.Equal(t, "alice", gotLocal["name"])
	})

	t.Run("Second Get hits local directly", func(t *testing.T) {
		// 基于上面的测试，local 已经有值了
		var got map[string]string
		err = multi.Get(ctx, "user:1", &got)
		require.NoError(t, err)
		require.Equal(t, "alice", got["name"])
	})
}

// TestMulti_FailOpen 测试 FailOpenOnLocalError 行为
func TestMulti_FailOpen(t *testing.T) {
	ctx := context.Background()

	t.Run("FailOpenOnLocalError=true continues on local error", func(t *testing.T) {
		local := newMockLocalForMulti()
		remote := newMockKVForMulti()

		// 设置 local Get 失败
		local.failGet.Store(true)

		multi, err := NewMulti(local, remote, &MultiConfig{
			BackfillTTL:          time.Minute,
			FailOpenOnLocalError: boolPtr(true),
		})
		require.NoError(t, err)

		// 写入 remote
		err = remote.Set(ctx, "key1", "value1", time.Minute)
		require.NoError(t, err)

		// Get 应该能成功（尽管 local 失败）
		var got string
		err = multi.Get(ctx, "key1", &got)
		require.NoError(t, err)
		require.Equal(t, "value1", got)
	})

	t.Run("FailOpenOnLocalError=false returns error on local error", func(t *testing.T) {
		local := newMockLocalForMulti()
		remote := newMockKVForMulti()

		// 设置 local Get 失败
		local.failGet.Store(true)

		multi, err := NewMulti(local, remote, &MultiConfig{
			BackfillTTL:          time.Minute,
			FailOpenOnLocalError: boolPtr(false),
		})
		require.NoError(t, err)

		// 写入 remote
		err = remote.Set(ctx, "key2", "value2", time.Minute)
		require.NoError(t, err)

		// Get 应该失败（因为 local 错误且 FailOpen=false）
		var got string
		err = multi.Get(ctx, "key2", &got)
		require.Error(t, err)
		require.ErrorContains(t, err, "local get error")
	})

	t.Run("FailOpen on Set error", func(t *testing.T) {
		local := newMockLocalForMulti()
		remote := newMockKVForMulti()

		// 设置 local Set 失败
		local.failSet.Store(true)

		multi, err := NewMulti(local, remote, &MultiConfig{
			BackfillTTL:          time.Minute,
			FailOpenOnLocalError: boolPtr(true),
		})
		require.NoError(t, err)

		// Set 应该能成功（尽管 local 失败）
		err = multi.Set(ctx, "key3", "value3", time.Minute)
		require.NoError(t, err)

		// 验证 remote 中有值
		var got string
		err = remote.Get(ctx, "key3", &got)
		require.NoError(t, err)
		require.Equal(t, "value3", got)
	})

	t.Run("FailOpen=false on Set error returns error", func(t *testing.T) {
		local := newMockLocalForMulti()
		remote := newMockKVForMulti()

		// 设置 local Set 失败
		local.failSet.Store(true)

		multi, err := NewMulti(local, remote, &MultiConfig{
			BackfillTTL:          time.Minute,
			FailOpenOnLocalError: boolPtr(false),
		})
		require.NoError(t, err)

		// Set 应该失败
		err = multi.Set(ctx, "key4", "value4", time.Minute)
		require.Error(t, err)
		require.ErrorContains(t, err, "local set error")
	})
}

// TestMulti_LocalTTL 测试 LocalTTL 配置
func TestMulti_LocalTTL(t *testing.T) {
	local := newMockLocalForMulti()
	remote := newMockKVForMulti()

	t.Run("LocalTTL=0 follows write TTL", func(t *testing.T) {
		multi, err := NewMulti(local, remote, &MultiConfig{
			LocalTTL:    0,
			BackfillTTL: 5 * time.Minute,
		})
		require.NoError(t, err)
		ctx := context.Background()

		err = multi.Set(ctx, "key1", "value1", 10*time.Minute)
		require.NoError(t, err)

		// local 应该使用写入的 TTL（10 分钟）
		_, ok := local.data["key1"]
		require.True(t, ok)
		require.Equal(t, 10*time.Minute, local.lastSetTTL)
	})

	t.Run("LocalTTL>0 uses configured TTL", func(t *testing.T) {
		local2 := newMockLocalForMulti()
		remote2 := newMockKVForMulti()

		multi, err := NewMulti(local2, remote2, &MultiConfig{
			LocalTTL:    30 * time.Minute,
			BackfillTTL: 5 * time.Minute,
		})
		require.NoError(t, err)
		ctx := context.Background()

		err = multi.Set(ctx, "key2", "value2", 10*time.Minute)
		require.NoError(t, err)

		// local 应该使用配置的 TTL（30 分钟）而不是写入 TTL
		_, ok := local2.data["key2"]
		require.True(t, ok)
		require.Equal(t, 30*time.Minute, local2.lastSetTTL)
	})

	t.Run("LocalTTL=0 preserves zero TTL for local default handling", func(t *testing.T) {
		local3 := newMockLocalForMulti()
		remote3 := newMockKVForMulti()

		multi, err := NewMulti(local3, remote3, &MultiConfig{
			LocalTTL:    0,
			BackfillTTL: 5 * time.Minute,
		})
		require.NoError(t, err)
		ctx := context.Background()

		err = multi.Set(ctx, "key3", "value3", 0)
		require.NoError(t, err)
		require.Equal(t, time.Duration(0), local3.lastSetTTL)
	})
}

// TestMulti_BackfillTTL 测试 BackfillTTL 配置
func TestMulti_BackfillTTL(t *testing.T) {
	local := newMockLocalForMulti()
	remote := newMockKVForMulti()

	multi, err := NewMulti(local, remote, &MultiConfig{
		BackfillTTL: 15 * time.Minute,
	})
	require.NoError(t, err)
	ctx := context.Background()

	t.Run("Backfill uses BackfillTTL", func(t *testing.T) {
		// 清空 local
		local.data = make(map[string]any)

		// 写入 remote
		err := remote.Set(ctx, "key1", "value1", time.Minute)
		require.NoError(t, err)

		// Get 触发 backfill
		var got string
		err = multi.Get(ctx, "key1", &got)
		require.NoError(t, err)
		require.Equal(t, "value1", got)

		// 验证 local 有值
		_, ok := local.data["key1"]
		require.True(t, ok)
		require.Equal(t, 15*time.Minute, local.lastSetTTL)
	})
}

// TestMulti_EdgeCases 测试边界情况
func TestMulti_EdgeCases(t *testing.T) {
	ctx := context.Background()

	t.Run("nil config uses defaults", func(t *testing.T) {
		local := newMockLocalForMulti()
		remote := newMockKVForMulti()

		multi, err := NewMulti(local, remote, nil)
		require.NoError(t, err)

		err = multi.Set(ctx, "key1", "value1", time.Minute)
		require.NoError(t, err)

		var got string
		err = multi.Get(ctx, "key1", &got)
		require.NoError(t, err)
		require.Equal(t, "value1", got)
	})

	t.Run("nil local returns error", func(t *testing.T) {
		remote := newMockKVForMulti()
		_, err := NewMulti(nil, remote, &MultiConfig{})
		require.ErrorIs(t, err, ErrLocalCacheRequired)
	})

	t.Run("nil remote returns error", func(t *testing.T) {
		local := newMockLocalForMulti()
		_, err := NewMulti(local, nil, &MultiConfig{})
		require.ErrorIs(t, err, ErrRemoteCacheRequired)
	})

	t.Run("Close is no-op", func(t *testing.T) {
		local := newMockLocalForMulti()
		remote := newMockKVForMulti()

		multi, err := NewMulti(local, remote, &MultiConfig{})
		require.NoError(t, err)

		err = multi.Close()
		require.NoError(t, err)
	})

	t.Run("Expire with zero TTL preserves caller intent when LocalTTL is unset", func(t *testing.T) {
		local := newMockLocalForMulti()
		remote := newMockKVForMulti()

		err := remote.Set(ctx, "key-expire", "value", time.Minute)
		require.NoError(t, err)
		err = local.Set(ctx, "key-expire", "value", time.Minute)
		require.NoError(t, err)

		multi, err := NewMulti(local, remote, &MultiConfig{
			LocalTTL:    0,
			BackfillTTL: time.Minute,
		})
		require.NoError(t, err)

		ok, err := multi.Expire(ctx, "key-expire", 0)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, time.Duration(0), local.lastExpTTL)
	})

	t.Run("Expire removes stale local entry when remote key is missing", func(t *testing.T) {
		local := newMockLocalForMulti()
		remote := newMockKVForMulti()

		err := local.Set(ctx, "stale-key", "stale-value", time.Minute)
		require.NoError(t, err)

		multi, err := NewMulti(local, remote, &MultiConfig{
			BackfillTTL: time.Minute,
		})
		require.NoError(t, err)

		ok, err := multi.Expire(ctx, "stale-key", time.Minute)
		require.NoError(t, err)
		require.False(t, ok)

		var got string
		err = local.Get(ctx, "stale-key", &got)
		require.ErrorIs(t, err, ErrMiss)
	})

	t.Run("Expire returns local delete error on remote miss when fail open is disabled", func(t *testing.T) {
		local := newMockLocalForMulti()
		remote := newMockKVForMulti()

		err := local.Set(ctx, "stale-key", "stale-value", time.Minute)
		require.NoError(t, err)
		local.failDel.Store(true)

		multi, err := NewMulti(local, remote, &MultiConfig{
			BackfillTTL:          time.Minute,
			FailOpenOnLocalError: boolPtr(false),
		})
		require.NoError(t, err)

		ok, err := multi.Expire(ctx, "stale-key", time.Minute)
		require.ErrorContains(t, err, "local delete error")
		require.False(t, ok)
	})
}
