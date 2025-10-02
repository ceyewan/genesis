package middleware

import "context"

// Ratelimiter exposes token bucket style limiters.
type Ratelimiter interface {
	Allow(ctx context.Context, key string) bool
	Wait(ctx context.Context, key string) error
}

// RatelimitConfig defines tuner knobs for limiters.
type RatelimitConfig struct {
	Rate        float64 `json:"rate" yaml:"rate"`
	Burst       int     `json:"burst" yaml:"burst"`
	Distributed bool    `json:"distributed" yaml:"distributed"`
}
