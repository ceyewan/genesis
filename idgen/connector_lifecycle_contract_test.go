package idgen_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/redis/go-redis/v9"

	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/idgen"
)

type closeableRedisConnector struct {
	mu     sync.RWMutex
	client *redis.Client
}

func (c *closeableRedisConnector) Connect(context.Context) error { return nil }

func (c *closeableRedisConnector) Close() error {
	c.mu.Lock()
	c.client = nil
	c.mu.Unlock()
	return nil
}

func (c *closeableRedisConnector) HealthCheck(context.Context) error {
	if c.GetClient() == nil {
		return connector.ErrClientNil
	}
	return nil
}

func (c *closeableRedisConnector) IsHealthy() bool { return c.GetClient() != nil }
func (*closeableRedisConnector) Name() string      { return "idgen-contract" }

func (c *closeableRedisConnector) GetClient() *redis.Client {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.client
}

func TestSequencerRejectsCallsAfterConnectorClose(t *testing.T) {
	t.Parallel()

	rawClient := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	t.Cleanup(func() { _ = rawClient.Close() })
	conn := &closeableRedisConnector{client: rawClient}
	sequencer, err := idgen.NewSequencer(
		&idgen.SequencerConfig{Driver: idgen.DriverRedis},
		idgen.WithRedisConnector(conn),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		call func() error
	}{
		{name: "Next", call: func() error { _, callErr := sequencer.Next(context.Background(), "key"); return callErr }},
		{name: "NextBatch", call: func() error { _, callErr := sequencer.NextBatch(context.Background(), "key", 2); return callErr }},
		{name: "Set", call: func() error { return sequencer.Set(context.Background(), "key", 1) }},
		{name: "SetIfNotExists", call: func() error { _, callErr := sequencer.SetIfNotExists(context.Background(), "key", 1); return callErr }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if callErr := tt.call(); !errors.Is(callErr, connector.ErrClientNil) {
				t.Fatalf("call error = %v, want connector.ErrClientNil", callErr)
			}
		})
	}
}
