package mq

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"

	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/internal/testkit"
)

func TestJetStreamDurableRestartRecoveryIntegration(t *testing.T) {
	t.Parallel()
	container, cfg := testkit.NewNATSContainer(t)
	cfg.MaxReconnects = -1
	ctx, cancel := testkit.NewContext(t, 45*time.Second)
	defer cancel()
	conn, err := connector.NewNATS(cfg, connector.WithLogger(testkit.NewLogger()))
	require.NoError(t, err)
	require.Eventually(t, func() bool { return conn.Connect(ctx) == nil }, 10*time.Second, 100*time.Millisecond)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	js, err := jetstream.New(conn.GetClient())
	require.NoError(t, err)
	q, err := New(&Config{Driver: DriverNATSJetStream, JetStream: &JetStreamConfig{AutoCreateStream: true}},
		WithNATSConnector(conn), WithLogger(testkit.NewLogger()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, q.Close()) })

	subject := uniqueSubject()
	stream := "S-" + subject[:len(subject)-len(".event")]
	durable := "recovery"
	name := durableConsumerName(durable, subject)
	var mu sync.Mutex
	seen := make(map[string]int)
	handler := func(msg Message) error {
		if err := msg.Ack(); err != nil {
			return err
		}
		mu.Lock()
		seen[string(msg.Data())]++
		mu.Unlock()
		return nil
	}
	subscribe := func() Subscription {
		t.Helper()
		sub, subscribeErr := q.Subscribe(ctx, subject, handler, WithDurable(durable))
		require.NoError(t, subscribeErr)
		return sub
	}
	info := func() *jetstream.ConsumerInfo {
		t.Helper()
		consumer, lookupErr := js.Consumer(ctx, stream, name)
		require.NoError(t, lookupErr)
		return consumer.CachedInfo()
	}
	publish := func(first, last int) {
		t.Helper()
		for i := first; i <= last; i++ {
			require.NoError(t, q.Publish(ctx, subject, fmt.Appendf(nil, "message-%d", i)))
		}
	}
	waitAck := func(count uint64) {
		t.Helper()
		require.Eventually(t, func() bool {
			consumer, lookupErr := js.Consumer(ctx, stream, name)
			return lookupErr == nil && consumer.CachedInfo().AckFloor.Stream >= count
		}, 10*time.Second, 50*time.Millisecond)
	}
	stop := func(sub Subscription) {
		t.Helper()
		require.NoError(t, sub.Drain(ctx))
	}

	first := subscribe()
	// Compare with the actual initial file metadata, not host time: Docker's
	// VM clock can differ, and NATS 2.10 timestamps memory and disk separately.
	metadata, err := container.CopyFileFromContainer(ctx,
		"/tmp/nats/jetstream/$G/streams/"+stream+"/obs/"+name+"/meta.inf")
	require.NoError(t, err)
	var persisted struct{ Created time.Time }
	require.NoError(t, json.NewDecoder(metadata).Decode(&persisted))
	require.NoError(t, metadata.Close())
	require.False(t, persisted.Created.IsZero())
	publish(1, 3)
	waitAck(3)
	baseline := info()
	stop(first)
	// Retained messages published while the application subscription is down
	// must survive both subscription recreation and the broker restart.
	publish(4, 6)
	second := subscribe()
	waitAck(6)
	afterSubscription := info()
	require.Equal(t, baseline.Created, afterSubscription.Created)
	require.Equal(t, baseline.Config, afterSubscription.Config)
	stop(second)
	publish(7, 9)

	restartBroker := func() {
		t.Helper()
		require.NoError(t, q.Close())
		require.NoError(t, conn.Close())
		grace := 5 * time.Second
		require.NoError(t, container.Stop(ctx, &grace))
		require.NoError(t, container.Start(ctx))
		port, err := container.MappedPort(ctx, "4222")
		require.NoError(t, err)
		// Docker may assign a new ephemeral host port after restart. Reconnect
		// using the new endpoint; consumer state must remain broker-owned.
		endpoint, err := url.Parse(cfg.URL)
		require.NoError(t, err)
		endpoint.Host = net.JoinHostPort(endpoint.Hostname(), port.Port())
		cfg.URL = endpoint.String()
		conn, err = connector.NewNATS(cfg, connector.WithLogger(testkit.NewLogger()))
		require.NoError(t, err)
		require.Eventually(t, func() bool { return conn.Connect(ctx) == nil }, 10*time.Second, 100*time.Millisecond)
		js, err = jetstream.New(conn.GetClient())
		require.NoError(t, err)
		q, err = New(&Config{Driver: DriverNATSJetStream, JetStream: &JetStreamConfig{AutoCreateStream: true}},
			WithNATSConnector(conn), WithLogger(testkit.NewLogger()))
		require.NoError(t, err)
		require.Eventually(t, func() bool {
			_, lookupErr := js.Consumer(ctx, stream, name)
			return lookupErr == nil
		}, 10*time.Second, 100*time.Millisecond)
	}
	restartBroker()
	afterBroker := info()
	// First recovery must use the original persisted timestamp, never a
	// timestamp from an unnecessary update or from the broker restart.
	require.Equal(t, persisted.Created, afterBroker.Created)
	require.Equal(t, baseline.Config, afterBroker.Config)
	require.Equal(t, name, afterBroker.Name)
	require.GreaterOrEqual(t, afterBroker.Delivered.Consumer, afterSubscription.Delivered.Consumer)
	require.GreaterOrEqual(t, afterBroker.Delivered.Stream, afterSubscription.Delivered.Stream)
	require.GreaterOrEqual(t, afterBroker.AckFloor.Consumer, afterSubscription.AckFloor.Consumer)
	require.GreaterOrEqual(t, afterBroker.AckFloor.Stream, afterSubscription.AckFloor.Stream)

	third := subscribe()
	publish(10, 12)
	waitAck(12)
	stop(third)
	final := info()
	require.Equal(t, afterBroker.Created, final.Created)
	require.Equal(t, baseline.Config, final.Config)
	require.Zero(t, final.NumAckPending)
	// After the first file-store recovery, further no-op startups and broker
	// restarts must preserve the persisted creation timestamp exactly.
	restartBroker()
	recoveredAgain := info()
	require.Equal(t, afterBroker.Created, recoveredAgain.Created)
	require.Equal(t, final.Config, recoveredAgain.Config)
	require.Equal(t, final.AckFloor.Stream, recoveredAgain.AckFloor.Stream)
	require.Equal(t, final.AckFloor.Consumer, recoveredAgain.AckFloor.Consumer)
	require.Equal(t, final.Delivered.Stream, recoveredAgain.Delivered.Stream)
	require.Equal(t, final.Delivered.Consumer, recoveredAgain.Delivered.Consumer)
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, seen, 12)
	for i := 1; i <= 12; i++ {
		require.Equal(t, 1, seen[fmt.Sprintf("message-%d", i)], "missing or duplicate message %d", i)
	}
}

func TestJetStreamDurableConfigUpdateIntegration(t *testing.T) {
	t.Parallel()
	conn := testkit.NewNATSContainerConnector(t)
	ctx, cancel := testkit.NewContext(t, 15*time.Second)
	defer cancel()
	transport, err := newNATSJetStreamTransport(conn, &JetStreamConfig{AutoCreateStream: true}, testkit.NewLogger())
	require.NoError(t, err)
	subject := uniqueSubject()
	require.NoError(t, transport.ensureStream(ctx, subject))
	stream := transport.getStreamName(subject)
	cfg := jetstream.ConsumerConfig{
		Durable: "worker", FilterSubject: subject,
		AckPolicy: jetstream.AckExplicitPolicy, AckWait: 30 * time.Second, MaxDeliver: 5,
		Description: "operator-owned", Metadata: map[string]string{"owner": "operations"}, MaxWaiting: 25,
	}
	consumer, err := transport.js.CreateConsumer(ctx, stream, cfg)
	require.NoError(t, err)
	created := consumer.CachedInfo().Created
	desired := cfg
	desired.Description = ""
	desired.Metadata = nil
	desired.MaxWaiting = 0
	desired.AckWait = time.Minute
	desired.MaxDeliver = 10
	desired.MaxAckPending = 20
	updated, err := resolveJetStreamConsumer(ctx, transport.js, stream, desired)
	require.NoError(t, err)
	info := updated.CachedInfo()
	require.Equal(t, created, info.Created)
	require.Equal(t, cfg.Description, info.Config.Description)
	require.Equal(t, cfg.Metadata, info.Config.Metadata)
	require.Equal(t, cfg.MaxWaiting, info.Config.MaxWaiting)
	require.Equal(t, desired.AckWait, info.Config.AckWait)
	require.Equal(t, desired.MaxDeliver, info.Config.MaxDeliver)
	require.Equal(t, desired.MaxAckPending, info.Config.MaxAckPending)

	desired.DeliverPolicy = jetstream.DeliverNewPolicy
	_, err = resolveJetStreamConsumer(ctx, transport.js, stream, desired)
	require.ErrorIs(t, err, ErrInvalidConfig)
	unchanged, err := transport.js.Consumer(ctx, stream, cfg.Durable)
	require.NoError(t, err)
	require.Equal(t, info.Config, unchanged.CachedInfo().Config)
}
