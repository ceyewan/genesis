package db

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ceyewan/genesis/clog"
)

func TestGormLoggerSlowThreshold(t *testing.T) {
	t.Parallel()

	defaultLogger := newGormLogger(clog.Discard(), false, 0).(*gormLogger)
	require.Equal(t, defaultSlowThreshold, defaultLogger.slowThreshold)

	customLogger := newGormLogger(clog.Discard(), false, 25*time.Millisecond).(*gormLogger)
	require.Equal(t, 25*time.Millisecond, customLogger.slowThreshold)
}

func TestNewRejectsNegativeSlowThreshold(t *testing.T) {
	t.Parallel()

	database, err := New(&Config{Driver: "sqlite"}, WithSlowThreshold(-time.Millisecond))
	require.Nil(t, database)
	require.ErrorIs(t, err, ErrInvalidConfig)
}
