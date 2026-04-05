package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ceyewan/genesis/clog"
)

func TestWithLogger(t *testing.T) {
	t.Parallel()

	t.Run("ValidLogger", func(t *testing.T) {
		l := &loader{}
		logger := clog.Discard()
		opt := WithLogger(logger)
		opt(l)
		require.Equal(t, logger, l.logger)
	})

	t.Run("NilLogger", func(t *testing.T) {
		l := &loader{logger: clog.Discard()}
		opt := WithLogger(nil)
		opt(l)
		require.NotNil(t, l.logger)
	})
}
