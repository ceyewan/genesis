package idem

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type untouchedStore struct {
	Store
	reads int
}

func (s *untouchedStore) GetResult(context.Context, string) ([]byte, error) {
	s.reads++
	return nil, ErrResultNotFound
}

func TestInvalidCallbackAndTTLPrecedeStorage(t *testing.T) {
	t.Parallel()
	store := &untouchedStore{}
	component, err := New(&Config{Driver: DriverMemory}, WithStore(store))
	require.NoError(t, err)
	_, err = component.Execute(context.Background(), "key", nil)
	require.ErrorIs(t, err, ErrInvalidConfig)
	executed, err := component.Consume(context.Background(), "key", time.Second, nil)
	require.ErrorIs(t, err, ErrInvalidConfig)
	require.False(t, executed)
	executed, err = component.Consume(context.Background(), "key", -time.Nanosecond, func(context.Context) error { t.Fatal("Invalid input executed callback"); return nil })
	require.ErrorIs(t, err, ErrInvalidConfig)
	require.False(t, executed)
	require.Zero(t, store.reads)
}

func TestConsumeZeroTTLStillUsesDefault(t *testing.T) {
	t.Parallel()
	component, err := New(&Config{Driver: DriverMemory, DefaultTTL: time.Minute})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, component.Close()) })
	first, err := component.Consume(context.Background(), "key", 0, func(context.Context) error { return nil })
	require.NoError(t, err)
	require.True(t, first)
	second, err := component.Consume(context.Background(), "key", 0, func(context.Context) error { t.Fatal("Completed key ran twice"); return nil })
	require.NoError(t, err)
	require.False(t, second)
}
