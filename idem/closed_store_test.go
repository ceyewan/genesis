package idem

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMemoryStoreRejectsOperationsAfterClose(t *testing.T) {
	t.Parallel()
	s := newMemoryStore("closed:").(*memoryStore)
	token, locked, err := s.Lock(t.Context(), "key", time.Minute)
	require.NoError(t, err)
	require.True(t, locked)
	require.NoError(t, s.Close())
	require.NoError(t, s.Close())
	_, _, err = s.Lock(t.Context(), "other", time.Minute)
	require.ErrorIs(t, err, ErrClosed)
	_, err = s.GetResult(t.Context(), "key")
	require.ErrorIs(t, err, ErrClosed)
	require.ErrorIs(t, s.SetResult(t.Context(), "key", []byte("result"), time.Minute, token), ErrClosed)
	require.ErrorIs(t, s.Refresh(t.Context(), "key", token, time.Minute), ErrClosed)
	require.ErrorIs(t, s.Unlock(t.Context(), "key", token), ErrClosed)
	require.ErrorIs(t, s.DeleteResult(t.Context(), "key"), ErrClosed)
}
