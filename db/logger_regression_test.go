package db

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/xerrors"
)

func TestGormLoggerSilentDoesNotFormatSQL(t *testing.T) {
	t.Parallel()
	log := newGormLogger(clog.Discard(), true, 0)
	log.Trace(t.Context(), time.Now(), func() (string, int64) {
		t.Fatal("SQL formatted in silent mode")
		return "", 0
	}, nil)
}

func TestGormLoggerPreservesTraceContext(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "sql.log")
	log, err := clog.New(&clog.Config{Level: "debug", Format: "json", Output: path}, clog.WithTraceContext())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, log.Close()) })
	span := oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID: oteltrace.TraceID{1}, SpanID: oteltrace.SpanID{2}, TraceFlags: oteltrace.FlagsSampled,
	})
	ctx := oteltrace.ContextWithSpanContext(context.Background(), span)
	adapter := newGormLogger(log, false, time.Millisecond)
	var offset int
	for _, emit := range []func(){
		func() { adapter.Info(ctx, "Info %d", 1) },
		func() { adapter.Warn(ctx, "Warn") },
		func() { adapter.Error(ctx, "Error") },
		func() { adapter.Trace(ctx, time.Now(), func() (string, int64) { return "SELECT 1", 1 }, nil) },
		func() {
			adapter.Trace(ctx, time.Now().Add(-time.Second), func() (string, int64) { return "SELECT 2", 1 }, nil)
		},
		func() {
			adapter.Trace(ctx, time.Now(), func() (string, int64) { return "SELECT 3", 0 }, xerrors.New("failure"))
		},
	} {
		emit()
		output, err := os.ReadFile(path)
		require.NoError(t, err)
		line := string(output[offset:])
		offset = len(output)
		require.Contains(t, line, `"trace_id":"`+span.TraceID().String()+`"`)
		require.Contains(t, line, `"span_id":"`+span.SpanID().String()+`"`)
	}
}
