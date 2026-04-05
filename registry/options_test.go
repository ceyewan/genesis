package registry

import (
	"testing"

	"github.com/ceyewan/genesis/clog"
	"github.com/stretchr/testify/require"
)

func TestWithLogger(t *testing.T) {
	t.Parallel()

	t.Run("nil logger", func(t *testing.T) {
		t.Parallel()
		opt := WithLogger(nil)
		o := &options{}
		opt(o)
		require.Nil(t, o.logger)
	})

	t.Run("valid logger", func(t *testing.T) {
		t.Parallel()
		logger := clog.Discard()
		opt := WithLogger(logger)
		o := &options{}
		opt(o)
		require.NotNil(t, o.logger)
		require.Equal(t, logger, o.logger)
		// Since clog.Discard() returns a noopLogger and its WithNamespace returns itself,
		// o.logger will be assigned. In a real logger, it would have the namespace added.
	})
}
