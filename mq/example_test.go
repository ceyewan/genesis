package mq_test

import (
	"context"
	"time"

	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/mq"
)

func Example() {
	ctx := context.Background()
	conn, err := connector.NewRedis(&connector.RedisConfig{Addr: "localhost:6379"})
	if err != nil {
		return
	}
	defer conn.Close()
	if err := conn.Connect(ctx); err != nil {
		return
	}
	queue, err := mq.New(&mq.Config{Driver: mq.DriverRedisStream}, mq.WithRedisConnector(conn))
	if err != nil {
		return
	}
	defer queue.Close()
	if err := queue.Publish(ctx, "orders", []byte(`{"id":42}`)); err != nil {
		return
	}
	drainCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = queue.Drain(drainCtx)
}
