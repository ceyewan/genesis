package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/ceyewan/genesis/cache"
	"github.com/ceyewan/genesis/db"
	"github.com/ceyewan/genesis/dlock"
	"github.com/ceyewan/genesis/internal/testkit"
	"github.com/ceyewan/genesis/xerrors"
)

func newTestDatabase(t *testing.T) db.DB {
	t.Helper()
	database, err := db.New(&db.Config{Driver: "sqlite"}, db.WithSQLiteConnector(testkit.NewSQLiteConnector(t)), db.WithSilentMode())
	require.NoError(t, err)
	require.NoError(t, database.DB(context.Background()).AutoMigrate(&product{}))
	require.NoError(t, database.DB(context.Background()).Create(&product{ID: "p-1001", Name: "shirt"}).Error)
	return database
}

func TestLocalConcurrentRebuild(t *testing.T) {
	t.Parallel()
	values, err := cache.NewLocal(&cache.LocalConfig{MaxEntries: 128})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, values.Close()) })
	ctx, cancel := testkit.NewContext(t, 10*time.Second)
	defer cancel()
	reader := &productReader{database: newTestDatabase(t), values: values}
	require.NoError(t, verifyConcurrent(ctx, reader))
	for range 3 {
		_, err := reader.get(ctx, "missing")
		require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	}
	require.EqualValues(t, 2, reader.reads.Load(), "Missing records use a short negative cache")
}

func TestSharedConcurrentRebuild(t *testing.T) {
	t.Parallel()
	conn := testkit.NewRedisContainerConnector(t)
	values, err := cache.NewDistributed(&cache.DistributedConfig{KeyPrefix: testkit.NewID() + ":"}, cache.WithRedisConnector(conn))
	require.NoError(t, err)
	database := newTestDatabase(t)
	prefix := testkit.NewID() + ":"
	var readers []*productReader
	for range 3 {
		locker, err := dlock.New(&dlock.Config{Driver: dlock.DriverRedis, Prefix: prefix, DefaultTTL: 3 * time.Second}, dlock.WithRedisConnector(conn))
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, locker.Close()) })
		readers = append(readers, &productReader{database: database, values: values, locker: locker})
	}
	ctx, cancel := testkit.NewContext(t, 10*time.Second)
	defer cancel()
	require.NoError(t, verifyConcurrent(ctx, readers...))
}

type failedCache struct{ cache.KV }

func (failedCache) Get(context.Context, string, any) error { return xerrors.New("cache unavailable") }
func TestRebuildPropagatesCacheError(t *testing.T) {
	t.Parallel()
	reader := &productReader{values: failedCache{}}
	_, err := reader.get(context.Background(), "product")
	require.ErrorContains(t, err, "cache unavailable")
	require.Zero(t, reader.reads.Load())
}
