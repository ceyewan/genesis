package connector

import (
	"context"
	"errors"
	"testing"
)

type failingSQLPinger struct {
	err    error
	closed bool
}

func (p *failingSQLPinger) PingContext(context.Context) error { return p.err }

func (p *failingSQLPinger) Close() error {
	p.closed = true
	return nil
}

func TestPingAndCloseOnFailureClosesSQLDB(t *testing.T) {
	t.Parallel()

	want := errors.New("ping failed")
	db := &failingSQLPinger{err: want}
	if err := pingAndCloseOnFailure(context.Background(), db); !errors.Is(err, want) {
		t.Fatalf("pingAndCloseOnFailure() error = %v, want ping failure", err)
	}
	if !db.closed {
		t.Fatal("pingAndCloseOnFailure() did not close database after ping failure")
	}
}
