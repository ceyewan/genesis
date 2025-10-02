package db

import "context"

// Session represents a database access interface.
type Session interface {
	Exec(ctx context.Context, query string, args ...any) error
	Query(ctx context.Context, dest any, query string, args ...any) error
}

// Transaction provides transactional execution helpers.
type Transaction interface {
	ExecTx(ctx context.Context, fn func(ctx context.Context, sx Session) error) error
}

// Provider combines read/write operations exposed to callers.
type Provider interface {
	Session
	Transaction
}
