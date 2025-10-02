package middleware

import "context"

// Breaker guards downstream calls and provides circuit breaker semantics.
type Breaker interface {
	Execute(ctx context.Context, key string, fn func(context.Context) error) error
	State(key string) State
}

// State captures circuit breaker states.
type State int

const (
	// StateClosed indicates normal operation.
	StateClosed State = iota
	// StateOpen indicates the breaker is rejecting calls.
	StateOpen
	// StateHalfOpen indicates trial calls are permitted.
	StateHalfOpen
)

// BreakerConfig controls thresholds for state transitions.
type BreakerConfig struct {
	FailureRatio float64 `json:"failure_ratio" yaml:"failure_ratio"`
	Window       int     `json:"window" yaml:"window"`
	CoolDown     string  `json:"cool_down" yaml:"cool_down"`
}
