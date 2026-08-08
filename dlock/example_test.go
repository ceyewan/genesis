package dlock_test

import (
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/dlock"
)

func Example() {
	var conn connector.RedisConnector
	_, _ = dlock.New(&dlock.Config{Driver: dlock.DriverRedis}, dlock.WithRedisConnector(conn))
}
