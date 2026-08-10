package mq

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

type progressJetStreamMsg struct {
	progressCalls int
	progressErr   error
}

func (*progressJetStreamMsg) Metadata() (*jetstream.MsgMetadata, error) { return nil, nil }
func (*progressJetStreamMsg) Data() []byte                              { return nil }
func (*progressJetStreamMsg) Headers() nats.Header                      { return nil }
func (*progressJetStreamMsg) Subject() string                           { return "test.progress" }
func (*progressJetStreamMsg) Reply() string                             { return "reply" }
func (*progressJetStreamMsg) Ack() error                                { return nil }
func (*progressJetStreamMsg) DoubleAck(context.Context) error           { return nil }
func (*progressJetStreamMsg) Nak() error                                { return nil }
func (*progressJetStreamMsg) NakWithDelay(time.Duration) error          { return nil }
func (*progressJetStreamMsg) Term() error                               { return nil }
func (*progressJetStreamMsg) TermWithReason(string) error               { return nil }
func (m *progressJetStreamMsg) InProgress() error {
	m.progressCalls++
	return m.progressErr
}

func TestJetStreamMessageProgressCapability(t *testing.T) {
	wantErr := errors.New("progress failed")
	underlying := &progressJetStreamMsg{progressErr: wantErr}
	message := Message(&jetStreamMessage{msg: underlying})

	progress, ok := message.(ProgressMessage)
	require.True(t, ok)
	require.ErrorIs(t, progress.InProgress(), wantErr)
	require.Equal(t, 1, underlying.progressCalls)
}

func TestJetStreamMessageProgressAfterAckIsNoOp(t *testing.T) {
	underlying := &progressJetStreamMsg{}
	message := &jetStreamMessage{msg: underlying, acked: true}

	require.NoError(t, message.InProgress())
	require.Zero(t, underlying.progressCalls)
}

func TestRedisStreamMessageHasNoProgressCapability(t *testing.T) {
	var message Message = &redisStreamMessage{}
	_, ok := message.(ProgressMessage)
	require.False(t, ok)
}
