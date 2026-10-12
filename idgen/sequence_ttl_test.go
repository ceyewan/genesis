package idgen

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSequencerSubmillisecondTTLIsRejected(t *testing.T) {
	t.Parallel()
	_, err := NewSequencer(&SequencerConfig{TTL: time.Nanosecond})
	require.ErrorIs(t, err, ErrInvalidInput)
	for _, ttl := range []time.Duration{0, time.Millisecond, 1500 * time.Microsecond} {
		cfg := &SequencerConfig{TTL: ttl}
		cfg.setDefaults()
		require.NoError(t, cfg.validate())
	}
}
