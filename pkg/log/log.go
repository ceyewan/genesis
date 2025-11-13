package log

import (
	"context"
)

// Level defines log levels.
type Level int

const (
	DebugLevel Level = iota
	InfoLevel
	WarnLevel
	ErrorLevel
)

// Logger defines the interface for logging operations.
type Logger interface {
	// Debug logs at debug level.
	Debug(msg string, fields ...Field)
	// Info logs at info level.
	Info(msg string, fields ...Field)
	// Warn logs at warn level.
	Warn(msg string, fields ...Field)
	// Error logs at error level.
	Error(msg string, fields ...Field)

	// WithFields returns a new logger with additional fields.
	WithFields(fields ...Field) Logger
	// WithContext returns a new logger with context attached.
	WithContext(ctx context.Context) Logger

	// Sync flushes any buffered log entries. Should be called on shutdown.
	Sync() error
}

// Field represents a structured log field.
type Field interface{}

// String returns a field for string key-value pairs.
func String(key, value string) Field {
	return stringField{key: key, value: value}
}

type stringField struct {
	key   string
	value string
}

// Int returns a field for int key-value pairs.
func Int(key string, value int) Field {
	return intField{key: key, value: value}
}

type intField struct {
	key   string
	value int
}

// Error returns a field for error key-value pairs.
func Error(err error) Field {
	return errorField{err: err}
}

type errorField struct {
	err error
}

// Any returns a field for any key-value pairs.
func Any(key string, value interface{}) Field {
	return anyField{key: key, value: value}
}

type anyField struct {
	key   string
	value interface{}
}

// ContextKey is the type for context keys related to logging.
type ContextKey string

const (
	// TraceIDKey is the context key for trace ID.
	TraceIDKey ContextKey = "trace_id"
	// RequestIDKey is the context key for request ID.
	RequestIDKey ContextKey = "request_id"
	// UserIDKey is the context key for user ID.
	UserIDKey ContextKey = "user_id"
)

// StringFromContext retrieves a string value from context using the given key.
func StringFromContext(ctx context.Context, key ContextKey) (string, bool) {
	val := ctx.Value(key)
	if val == nil {
		return "", false
	}
	s, ok := val.(string)
	return s, ok
}
