package dlock_test

import (
	"context"
	"time"

	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/dlock"
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
	locker, err := dlock.New(&dlock.Config{Driver: dlock.DriverRedis}, dlock.WithRedisConnector(conn))
	if err != nil {
		return
	}
	defer locker.Close()
	if err := locker.Lock(ctx, "orders:42", dlock.WithTTL(10*time.Second)); err != nil {
		return
	}
	lost := locker.Lost("orders:42")
	_ = lost
	_ = locker.Unlock(ctx, "orders:42")
}
