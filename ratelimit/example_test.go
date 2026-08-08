package ratelimit_test

import "github.com/ceyewan/genesis/ratelimit"

func Example() {
	limiter := ratelimit.Discard()
	defer limiter.Close()
}
