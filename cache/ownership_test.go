package cache

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type trackingRedisConnector struct {
	client     *redis.Client
	closeCalls atomic.Int32
}

func (c *trackingRedisConnector) Connect(context.Context) error     { return nil }
func (c *trackingRedisConnector) HealthCheck(context.Context) error { return nil }
func (c *trackingRedisConnector) IsHealthy() bool                   { return true }
func (c *trackingRedisConnector) Name() string                      { return "tracking-redis" }
func (c *trackingRedisConnector) GetClient() *redis.Client          { return c.client }
func (c *trackingRedisConnector) Close() error {
	c.closeCalls.Add(1)
	return nil
}

func TestDistributedCloseDoesNotCloseBorrowedConnector(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	conn := &trackingRedisConnector{client: client}

	distributed, err := NewDistributed(
		&DistributedConfig{Driver: DriverRedis},
		WithRedisConnector(conn),
	)
	require.NoError(t, err)
	require.NoError(t, distributed.Close())
	require.NoError(t, distributed.Close())
	require.Zero(t, conn.closeCalls.Load())
	require.Same(t, client, conn.GetClient())
}
