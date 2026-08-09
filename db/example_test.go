package db_test

import (
	"context"

	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/db"
)

func Example() {
	conn, err := connector.NewSQLite(&connector.SQLiteConfig{Path: ":memory:"})
	if err != nil || conn.Connect(context.Background()) != nil {
		return
	}
	defer conn.Close()
	database, err := db.New(&db.Config{Driver: "sqlite"}, db.WithSQLiteConnector(conn))
	if err != nil {
		return
	}
	_ = database.DB(context.Background())
}
