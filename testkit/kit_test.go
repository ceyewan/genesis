package testkit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewKit(t *testing.T) {
	t.Parallel()

	kit := NewKit(t)

	require.NotNil(t, kit)
	require.NotNil(t, kit.Ctx)
	require.NotNil(t, kit.Logger)
	require.NotNil(t, kit.Meter)
}

func TestNewContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := NewContext(t, 50*time.Millisecond)
	defer cancel()

	require.NotNil(t, ctx)
	deadline, ok := ctx.Deadline()
	require.True(t, ok)
	require.WithinDuration(t, time.Now().Add(50*time.Millisecond), deadline, 100*time.Millisecond)
}

func TestNewID(t *testing.T) {
	t.Parallel()

	id1 := NewID()
	id2 := NewID()

	require.Len(t, id1, 8)
	require.Len(t, id2, 8)
	require.NotEqual(t, id1, id2)
}
