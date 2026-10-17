package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRecursiveEnvironmentSchemaTerminates(t *testing.T) {
	type node struct {
		Value string `mapstructure:"value"`
		Next  *node  `mapstructure:"next"`
	}
	t.Setenv("AUDIT_LEFT_VALUE", "left")
	t.Setenv("AUDIT_RIGHT_VALUE", "right")
	l, err := New(&Config{Paths: []string{t.TempDir()}, EnvPrefix: "AUDIT"}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, l.Close()) })
	require.NoError(t, l.Load(t.Context()))
	var target struct {
		Left  node `mapstructure:"left"`
		Right node `mapstructure:"right"`
	}
	require.NoError(t, l.Unmarshal(&target))
	require.Equal(t, "left", target.Left.Value)
	require.Equal(t, "right", target.Right.Value)
}

func TestExplicitReloadNotifiesNilContextWatch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("value: old\n"), 0o644))
	l, err := New(&Config{Paths: []string{dir}, EnvPrefix: "RELOAD_AUDIT"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, l.Close()) })
	require.NoError(t, l.Load(t.Context()))
	ch, err := l.Watch(nil, "value") //nolint:staticcheck // Regression: nil previously panicked in a worker.
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte("value: new\n"), 0o644))
	require.NoError(t, l.Load(t.Context()))
	select {
	case event := <-ch:
		require.Equal(t, "new", event.Value)
	case <-time.After(time.Second):
		t.Fatal("Explicit reload did not emit a change")
	}
}
