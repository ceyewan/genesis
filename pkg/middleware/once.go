package middleware

import "context"

// ValueFunc returns a value along with an error, used by Once implementers.
type ValueFunc func(context.Context) (any, error)

// Once ensures an operation identified by key is executed only once at a time.
type Once interface {
	Do(ctx context.Context, key string, fn func(context.Context) error) error
	DoValue(ctx context.Context, key string, fn ValueFunc) (any, error)
}

// OnceConfig allows tuning of idempotency behaviour.
type OnceConfig struct {
	TTL         string `json:"ttl" yaml:"ttl"`
	Retry       int    `json:"retry" yaml:"retry"`
	StorageHint string `json:"storage_hint" yaml:"storage_hint"`
}
