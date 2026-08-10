package db_test

import (
	"errors"
	"testing"

	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/db"
)

func TestUnconnectedConnectorPreservesBothPublicErrorClasses(t *testing.T) {
	t.Parallel()

	conn, err := connector.NewSQLite(&connector.SQLiteConfig{Path: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}

	_, err = db.New(&db.Config{Driver: "sqlite"}, db.WithSQLiteConnector(conn))
	if !errors.Is(err, db.ErrConnectorNotReady) {
		t.Fatalf("New() error = %v, want db.ErrConnectorNotReady", err)
	}
	if !errors.Is(err, connector.ErrClientNil) {
		t.Fatalf("New() error = %v, want connector.ErrClientNil", err)
	}
}
