package db

import (
	"context"
	"errors"
	"testing"

	"github.com/ceyewan/genesis/connector"
)

func TestNewRejectsUnreadyConnector(t *testing.T) {
	conn, err := connector.NewSQLite(&connector.SQLiteConfig{Path: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = New(&Config{Driver: "sqlite"}, WithSQLiteConnector(conn))
	if !errors.Is(err, ErrConnectorNotReady) {
		t.Fatalf("New() error = %v, want ErrConnectorNotReady", err)
	}
}

func TestTransactionRejectsNilFunction(t *testing.T) {
	conn, err := connector.NewSQLite(&connector.SQLiteConfig{Path: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	database, err := New(&Config{Driver: "sqlite"}, WithSQLiteConnector(conn))
	if err != nil {
		t.Fatal(err)
	}
	//nolint:staticcheck // verifies the documented defensive nil-context boundary
	if err := database.Transaction(nil, nil); !errors.Is(err, ErrNilTransaction) {
		t.Fatalf("Transaction(nil, nil) = %v, want ErrNilTransaction", err)
	}
	//nolint:staticcheck // verifies the documented defensive nil-context boundary
	if database.DB(nil) == nil {
		t.Fatal("DB(nil) returned nil")
	}
}
