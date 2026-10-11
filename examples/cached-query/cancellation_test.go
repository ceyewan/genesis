package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/ceyewan/genesis/cache"
	"github.com/ceyewan/genesis/xerrors"
)

func TestRebuildLeaderCancellationDoesNotAbortSharedLoad(t *testing.T) {
	t.Parallel()
	values, err := cache.NewLocal(&cache.LocalConfig{MaxEntries: 128})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, values.Close()) })
	database := newTestDatabase(t)
	loading := make(chan context.Context, 1)
	release := make(chan struct{})
	defer close(release)
	require.NoError(t, database.DB(t.Context()).Callback().Query().Before("gorm:query").Register("test:pause-query", func(tx *gorm.DB) {
		loading <- tx.Statement.Context
		select {
		case <-release:
		case <-tx.Statement.Context.Done():
			tx.Error = xerrors.Join(tx.Error, tx.Statement.Context.Err())
		}
	}))
	reader := &productReader{database: database, values: values}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	leader, cancelLeader := context.WithCancel(ctx)
	defer cancelLeader()
	leaderDone := make(chan error, 1)
	go func() { _, err := reader.get(leader, "p-1001"); leaderDone <- err }()
	var loadCtx context.Context
	select {
	case loadCtx = <-loading:
	case <-ctx.Done():
		t.Fatal("Rebuild did not start")
	}
	// 确定另一个等待者已经加入共享调用，不用 Sleep 猜测调度时序。
	shared := reader.pending.DoChan("product:p-1001", func() (any, error) {
		return nil, xerrors.New("shared rebuild unexpectedly restarted")
	})
	cancelLeader()
	select {
	case err := <-leaderDone:
		require.ErrorIs(t, err, context.Canceled)
	case <-ctx.Done():
		t.Fatal("Canceled leader did not stop waiting")
	}
	require.NoError(t, loadCtx.Err(), "Leader cancellation must not cancel the shared load")
	deadline, ok := loadCtx.Deadline()
	require.True(t, ok, "Detached load must still have a bounded lifetime")
	require.Positive(t, time.Until(deadline))
	require.LessOrEqual(t, time.Until(deadline), 5*time.Second)
	followerDone := make(chan error, 1)
	go func() {
		value, err := reader.get(ctx, "p-1001")
		if err == nil && value.ID != "p-1001" {
			err = xerrors.New("unexpected product")
		}
		followerDone <- err
	}()
	// 解除数据库暂停；defer 负责失败路径，正常路径用发送唤醒当前查询。
	select {
	case release <- struct{}{}:
	case <-ctx.Done():
		t.Fatal("Shared load stopped before the query could complete")
	}
	select {
	case result := <-shared:
		require.NoError(t, result.Err)
		require.True(t, result.Shared)
		require.Equal(t, "p-1001", result.Val.(product).ID)
	case <-ctx.Done():
		t.Fatal("Shared waiter did not receive the product")
	}
	select {
	case err := <-followerDone:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("Live follower did not complete")
	}
	require.EqualValues(t, 1, reader.reads.Load())
}

func TestRebuildAlreadyCanceledRequestDoesNotLoad(t *testing.T) {
	t.Parallel()
	reader := &productReader{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := reader.get(ctx, "p-1001")
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, reader.reads.Load())
}
