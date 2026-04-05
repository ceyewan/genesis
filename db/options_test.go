package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/ceyewan/genesis/testkit"
)

func TestWithLogger(t *testing.T) {
	t.Parallel()

	t.Run("valid logger", func(t *testing.T) {
		logger := testkit.NewLogger()
		opt := WithLogger(logger)

		opts := &options{}
		opt(opts)

		assert.NotNil(t, opts.logger)
	})

	t.Run("nil logger", func(t *testing.T) {
		opt := WithLogger(nil)

		opts := &options{}
		opt(opts)

		assert.Nil(t, opts.logger)
	})
}
