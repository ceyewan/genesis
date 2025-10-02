package log

import "context"

// Logger is the minimal contract shared across the project.
type Logger interface {
	Debug(ctx context.Context, msg string, fields ...Field)
	Info(ctx context.Context, msg string, fields ...Field)
	Warn(ctx context.Context, msg string, fields ...Field)
	Error(ctx context.Context, msg string, fields ...Field)
	With(fields ...Field) Logger
	Sync() error
}

// Field represents a structured logging field.
type Field struct {
	Key   string
	Value any
}
