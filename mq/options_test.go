package mq

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPublishOptions(t *testing.T) {
	t.Parallel()

	t.Run("WithHeaders", func(t *testing.T) {
		opts := defaultPublishOptions()
		headers := Headers{"key1": "val1"}

		opt := WithHeaders(headers)
		opt(&opts)

		require.Equal(t, headers, opts.Headers)

		// Ensure it's a clone, changing original doesn't affect the options
		headers["key1"] = "val2"
		require.Equal(t, "val1", opts.Headers["key1"])
	})

	t.Run("WithHeader", func(t *testing.T) {
		opts := defaultPublishOptions()

		opt1 := WithHeader("key1", "val1")
		opt1(&opts)
		require.Equal(t, "val1", opts.Headers["key1"])

		opt2 := WithHeader("key2", "val2")
		opt2(&opts)
		require.Equal(t, "val1", opts.Headers["key1"])
		require.Equal(t, "val2", opts.Headers["key2"])
	})
}

func TestSubscribeOptions(t *testing.T) {
	t.Parallel()

	t.Run("WithQueueGroup", func(t *testing.T) {
		opts := defaultSubscribeOptions()
		opt := WithQueueGroup("test-group")
		opt(&opts)

		require.Equal(t, "test-group", opts.QueueGroup)
	})

	t.Run("WithManualAck", func(t *testing.T) {
		opts := defaultSubscribeOptions()
		opts.AutoAck = true // Set to true to test if it gets turned off

		opt := WithManualAck()
		opt(&opts)

		require.False(t, opts.AutoAck)
	})

	t.Run("WithAutoAck", func(t *testing.T) {
		opts := defaultSubscribeOptions()
		require.False(t, opts.AutoAck) // Default is false

		opt := WithAutoAck()
		opt(&opts)

		require.True(t, opts.AutoAck)
	})

	t.Run("WithDurable", func(t *testing.T) {
		opts := defaultSubscribeOptions()
		opt := WithDurable("test-durable")
		opt(&opts)

		require.Equal(t, "test-durable", opts.DurableName)
	})

	t.Run("WithBatchSize", func(t *testing.T) {
		opts := defaultSubscribeOptions()
		require.Equal(t, 10, opts.BatchSize) // Default is 10

		opt := WithBatchSize(20)
		opt(&opts)
		require.Equal(t, 20, opts.BatchSize)

		optZero := WithBatchSize(0)
		optZero(&opts)
		require.Equal(t, 20, opts.BatchSize) // Should not change if <= 0

		optNeg := WithBatchSize(-1)
		optNeg(&opts)
		require.Equal(t, 20, opts.BatchSize) // Should not change if <= 0
	})

	t.Run("WithMaxInflight", func(t *testing.T) {
		opts := defaultSubscribeOptions()
		require.Equal(t, 0, opts.MaxInflight) // Default is 0

		opt := WithMaxInflight(100)
		opt(&opts)
		require.Equal(t, 100, opts.MaxInflight)

		optZero := WithMaxInflight(0)
		optZero(&opts)
		require.Equal(t, 100, opts.MaxInflight) // Should not change if <= 0

		optNeg := WithMaxInflight(-1)
		optNeg(&opts)
		require.Equal(t, 100, opts.MaxInflight) // Should not change if <= 0
	})
}
