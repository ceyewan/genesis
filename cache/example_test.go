package cache_test

import (
	"context"
	"time"

	"github.com/ceyewan/genesis/cache"
)

func Example() {
	local, err := cache.NewLocal(&cache.LocalConfig{MaxEntries: 1000})
	if err != nil {
		return
	}
	defer local.Close()
	_ = local.Set(context.Background(), "key", "value", time.Minute)
}
