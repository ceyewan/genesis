package cache

import "errors"

var (
	// ErrCacheMiss indicates that the requested key does not exist.
	ErrCacheMiss = errors.New("cache: key not found")
)
