package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/internal/testkit"
	"github.com/ceyewan/genesis/registry"
)

func TestDynamicDiscoveryAndRejoin(t *testing.T) {
	// Registry 的 resolver 是进程全局状态，这个初始化测试不能并行。
	conn := testkit.NewEtcdContainerConnector(t)
	reg, err := registry.New(&registry.Config{Namespace: "/test/" + testkit.NewID()}, registry.WithEtcdConnector(conn))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reg.Close()) })
	ctx, cancel := testkit.NewContext(t, 15*time.Second)
	defer cancel()
	require.NoError(t, verifyDiscovery(ctx, reg, clog.Discard()))
	services, err := reg.GetService(ctx, "inventory")
	require.NoError(t, err)
	require.Empty(t, services)
	require.NoError(t, reg.Close())
	select {
	case _, ok := <-reg.LeaseFailures():
		require.False(t, ok)
	case <-ctx.Done():
		t.Fatal("Registry did not release its lifecycle workers")
	}
}
