package connector_test

import "github.com/ceyewan/genesis/connector"

func Example() {
	conn, err := connector.NewSQLite(&connector.SQLiteConfig{Path: ":memory:"})
	if err != nil {
		return
	}
	defer conn.Close()
}
