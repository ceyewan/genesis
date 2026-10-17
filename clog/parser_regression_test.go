package clog

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestColorParserPreservesEscapesAndEquals(t *testing.T) {
	t.Parallel()
	h := &coloredTextHandler{}
	for _, line := range []string{
		`token=aGVsbG8== expr="a == b"`,
		`msg="quoted \"hello world\"" token=aGVsbG8==`,
		`msg="slash \\" next="a b"`,
		`msg="%!(EXTRA value)" key=value`,
	} {
		pairs := h.parseKeyValuePairs(line)
		require.Len(t, pairs, 2)
		require.Equal(t, line, pairs[0]+" "+pairs[1])
	}
}

type closeBarrierHandler struct {
	slog.Handler
	entered chan struct{}
	release chan struct{}
	closed  chan struct{}
}

func (h *closeBarrierHandler) Handle(context.Context, slog.Record) error {
	close(h.entered)
	<-h.release
	return nil
}

func (h *closeBarrierHandler) Close() error { close(h.closed); return nil }

func TestCloseWaitsForInFlightLog(t *testing.T) {
	t.Parallel()
	h := &closeBarrierHandler{Handler: slog.NewTextHandler(io.Discard, nil), entered: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{})}
	log := &loggerImpl{shared: &loggerSharedState{handler: h}, options: &options{}, ownsResources: true}
	var wg sync.WaitGroup
	wg.Go(func() { log.Info("In flight") })
	<-h.entered
	wg.Go(func() { require.NoError(t, log.Close()) })
	select {
	case <-h.closed:
		t.Fatal("Handler closed before the log finished")
	case <-time.After(20 * time.Millisecond):
	}
	close(h.release)
	wg.Wait()
	select {
	case <-h.closed:
	default:
		t.Fatal("Handler was not closed")
	}
}
