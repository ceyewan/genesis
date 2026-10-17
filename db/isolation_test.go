package db

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"

	"github.com/ceyewan/genesis/internal/testkit"
	"github.com/ceyewan/genesis/xerrors"
)

func TestBorrowedPoolHasIsolatedPluginsAndCallbacks(t *testing.T) {
	t.Parallel()
	conn := testkit.NewSQLiteConnector(t)
	base := conn.GetClient()
	pool, err := base.DB()
	require.NoError(t, err)
	require.NoError(t, base.Exec("CREATE TABLE shared_rows (id INTEGER)").Error)
	require.NoError(t, base.Exec("INSERT INTO shared_rows VALUES (1)").Error)
	var baseQueries atomic.Int32
	require.NoError(t, base.Callback().Query().Before("gorm:query").Register("base-only", func(*gorm.DB) { baseQueries.Add(1) }))
	exporterA, exporterB := tracetest.NewInMemoryExporter(), tracetest.NewInMemoryExporter()
	providerA := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporterA))
	providerB := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporterB))
	t.Cleanup(func() {
		require.NoError(t, providerA.Shutdown(context.Background()))
		require.NoError(t, providerB.Shutdown(context.Background()))
	})
	a, err := New(&Config{Driver: "sqlite"}, WithSQLiteConnector(conn), WithTracer(providerA))
	require.NoError(t, err)
	b, err := New(&Config{Driver: "sqlite"}, WithSQLiteConnector(conn), WithTracer(providerB))
	require.NoError(t, err)
	c, err := New(&Config{Driver: "sqlite"}, WithSQLiteConnector(conn))
	require.NoError(t, err)
	for _, component := range []DB{a, b, c} {
		borrowed, err := component.DB(t.Context()).DB()
		require.NoError(t, err)
		require.Same(t, pool, borrowed)
		var count int64
		require.NoError(t, component.DB(t.Context()).Table("shared_rows").Count(&count).Error)
		require.EqualValues(t, 1, count)
		require.NoError(t, component.Close())
	}
	require.Zero(t, baseQueries.Load(), "connector callbacks must not leak into components")
	var count int64
	require.NoError(t, base.Table("shared_rows").Count(&count).Error)
	require.EqualValues(t, 1, baseQueries.Load())
	require.Len(t, exporterA.GetSpans(), 1)
	require.Len(t, exporterB.GetSpans(), 1)
	require.Empty(t, base.Plugins)
	require.NoError(t, pool.PingContext(t.Context()))

	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for range 12 {
		wg.Go(func() {
			component, err := New(&Config{Driver: "sqlite"}, WithSQLiteConnector(conn), WithTracer(providerA))
			if err == nil {
				err = component.DB(t.Context()).Exec("SELECT 1").Error
			}
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.NoError(t, pool.PingContext(t.Context()))
}

func TestBorrowedHandlePreservesSettingsAndOwnsStatementCache(t *testing.T) {
	t.Parallel()
	conn := testkit.NewSQLiteConnector(t)
	base := conn.GetClient()
	base.NamingStrategy = schema.NamingStrategy{TablePrefix: "isolated_"}
	fixed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	base.NowFunc = func() time.Time { return fixed }
	base.PrepareStmt = true
	component, err := New(&Config{Driver: "sqlite"}, WithSQLiteConnector(conn))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, component.Close()) })
	handle := component.DB(t.Context())
	require.Equal(t, "isolated_records", handle.NamingStrategy.TableName("Record"))
	require.Equal(t, fixed, handle.NowFunc())
	require.NoError(t, handle.Exec("SELECT 1").Error)
	statements, ok := handle.ConnPool.(*gorm.PreparedStmtDB)
	require.True(t, ok)
	require.NotEmpty(t, statements.Stmts.Keys())
	require.NoError(t, component.Close())
	require.Empty(t, statements.Stmts.Keys())
	pool, err := base.DB()
	require.NoError(t, err)
	require.NoError(t, pool.PingContext(t.Context()))
}

type failingBorrowedDialector struct {
	gorm.Dialector
	pool gorm.ConnPool
	err  error
}

func (d failingBorrowedDialector) Initialize(db *gorm.DB) error {
	db.ConnPool = d.pool
	return d.err
}

func TestGormInitializeFailureDoesNotCloseBorrowedPool(t *testing.T) {
	t.Parallel()
	base := testkit.NewSQLiteDB(t)
	pool, err := base.DB()
	require.NoError(t, err)
	failure := xerrors.New("dialect initialization failed")
	_, err = gorm.Open(borrowedDialector{failingBorrowedDialector{Dialector: base.Dialector, pool: pool, err: failure}}, &gorm.Config{DisableAutomaticPing: true})
	require.ErrorIs(t, err, failure)
	require.NoError(t, pool.PingContext(t.Context()))
	require.NoError(t, base.Exec("SELECT 1").Error)
}
