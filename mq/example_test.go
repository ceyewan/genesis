package mq_test

import (
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/mq"
)

func Example() {
	var conn connector.RedisConnector
	_, _ = mq.New(&mq.Config{Driver: mq.DriverRedisStream}, mq.WithRedisConnector(conn))
}
