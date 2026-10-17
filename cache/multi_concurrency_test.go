package cache

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type pausedRemote struct {
	Distributed
	read    chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *pausedRemote) Get(ctx context.Context, key string, dest any) error {
	err := r.Distributed.Get(ctx, key, dest)
	r.once.Do(func() { close(r.read); <-r.release })
	return err
}

func TestMultiBackfillCannotOverwriteMutation(t *testing.T) {
	t.Parallel()
	for _, remove := range []bool{false, true} {
		t.Run(map[bool]string{false: "Set", true: "Delete"}[remove], func(t *testing.T) {
			local, err := NewLocal(&LocalConfig{MaxEntries: 10})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, local.Close()) })
			remote := &pausedRemote{Distributed: newMockKVForMulti(), read: make(chan struct{}), release: make(chan struct{})}
			ctx := context.Background()
			require.NoError(t, remote.Set(ctx, "key", "old", time.Minute))
			multi, err := NewMulti(local, remote, nil)
			require.NoError(t, err)
			readDone := make(chan error, 1)
			go func() { var value string; readDone <- multi.Get(ctx, "key", &value) }()
			<-remote.read
			// TryAcquire 确定读回填仍持有协调额度，不依赖 Sleep 猜测 goroutine 时序。
			require.False(t, multi.(*multiCache).gateFor("key").TryAcquire(multiExclusiveWeight))
			writeDone := make(chan error, 1)
			go func() {
				if remove {
					writeDone <- multi.Delete(ctx, "key")
				} else {
					writeDone <- multi.Set(ctx, "key", "new", time.Minute)
				}
			}()
			close(remote.release)
			require.NoError(t, <-readDone)
			require.NoError(t, <-writeDone)
			var value string
			err = local.Get(ctx, "key", &value)
			if remove {
				require.ErrorIs(t, err, ErrMiss)
			} else {
				require.NoError(t, err)
				require.Equal(t, "new", value)
			}
		})
	}
}

func TestMultiAllowsDocumentedStaleTTL(t *testing.T) {
	t.Parallel()
	local, err := NewLocal(&LocalConfig{MaxEntries: 10})
	require.NoError(t, err)
	defer local.Close()
	remote := newMockKVForMulti()
	ctx := context.Background()
	require.NoError(t, remote.Set(ctx, "key", "old", time.Minute))
	multi, err := NewMulti(local, remote, &MultiConfig{BackfillTTL: time.Minute})
	require.NoError(t, err)
	var value string
	require.NoError(t, multi.Get(ctx, "key", &value))
	require.NoError(t, remote.Delete(ctx, "key"))
	require.NoError(t, multi.Get(ctx, "key", &value))
	require.Equal(t, "old", value)
}
