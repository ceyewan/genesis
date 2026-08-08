package cache

import (
	"errors"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestNormalizeRedisErrorPreservesWrappedMiss(t *testing.T) {
	t.Parallel()

	err := normalizeRedisError(errors.Join(errors.New("redis get failed"), redis.Nil))
	require.ErrorIs(t, err, ErrMiss)
}
