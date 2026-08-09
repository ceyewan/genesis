package connector_test

import (
	"context"

	"github.com/ceyewan/genesis/connector"
)

func Example() {
	conn, err := connector.NewSQLite(&connector.SQLiteConfig{Path: ":memory:"})
	if err != nil {
		return
	}
	defer conn.Close()
	ctx := context.Background()
	if err := conn.Connect(ctx); err != nil {
		return
	}
	_ = conn.HealthCheck(ctx)
}
