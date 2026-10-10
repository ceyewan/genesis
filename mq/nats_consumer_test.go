package mq

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

type stubJetStreamConsumer struct {
	jetstream.Consumer
	info jetstream.ConsumerInfo
}

func (c *stubJetStreamConsumer) CachedInfo() *jetstream.ConsumerInfo { return &c.info }

type stubConsumerAPI struct {
	consumer                  jetstream.Consumer
	lookupErrors              []error
	createError               error
	updateError               error
	lookups, creates, updates int
	updated                   jetstream.ConsumerConfig
}

func (a *stubConsumerAPI) Consumer(context.Context, string, string) (jetstream.Consumer, error) {
	a.lookups++
	if len(a.lookupErrors) >= a.lookups {
		return a.consumer, a.lookupErrors[a.lookups-1]
	}
	return a.consumer, nil
}

func (a *stubConsumerAPI) CreateConsumer(context.Context, string, jetstream.ConsumerConfig) (jetstream.Consumer, error) {
	a.creates++
	return a.consumer, a.createError
}

func (a *stubConsumerAPI) UpdateConsumer(_ context.Context, _ string, cfg jetstream.ConsumerConfig) (jetstream.Consumer, error) {
	a.updates++
	a.updated = cfg
	return a.consumer, a.updateError
}

func TestResolveJetStreamConsumer(t *testing.T) {
	t.Parallel()
	desired := jetstream.ConsumerConfig{
		Durable: "worker", FilterSubject: "orders.created", AckPolicy: jetstream.AckExplicitPolicy,
		AckWait: 30 * time.Second, MaxDeliver: 5,
	}
	tests := []struct {
		name                      string
		change                    func(*stubConsumerAPI, *jetstream.ConsumerConfig, *jetstream.ConsumerConfig)
		lookups, creates, updates int
		wantError                 error
	}{
		{name: "equivalent server defaults", lookups: 1},
		{
			name: "allowed update preserves operator fields", lookups: 1, updates: 1,
			change: func(_ *stubConsumerAPI, _, want *jetstream.ConsumerConfig) {
				want.AckWait = time.Minute
				want.MaxDeliver = 10
				want.MaxAckPending = 20
			},
		},
		{
			name: "missing consumer", lookups: 1, creates: 1,
			change: func(api *stubConsumerAPI, _, _ *jetstream.ConsumerConfig) {
				api.lookupErrors = []error{jetstream.ErrConsumerNotFound}
			},
		},
		{
			name: "concurrent creation", lookups: 2, creates: 1,
			change: func(api *stubConsumerAPI, _, _ *jetstream.ConsumerConfig) {
				api.lookupErrors = []error{jetstream.ErrConsumerNotFound, nil}
				api.createError = jetstream.ErrConsumerExists
			},
		},
		{
			name: "ephemeral creation", creates: 1,
			change: func(_ *stubConsumerAPI, _, want *jetstream.ConsumerConfig) {
				want.Durable = ""
			},
		},
		{
			name: "lookup failure is not creation", lookups: 1, wantError: context.DeadlineExceeded,
			change: func(api *stubConsumerAPI, _, _ *jetstream.ConsumerConfig) {
				api.lookupErrors = []error{context.DeadlineExceeded}
			},
		},
		{
			name: "deleted before update is not recreated", lookups: 1, updates: 1, wantError: jetstream.ErrConsumerDoesNotExist,
			change: func(api *stubConsumerAPI, _, want *jetstream.ConsumerConfig) {
				want.MaxAckPending = 20
				api.updateError = jetstream.ErrConsumerDoesNotExist
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			existing := desired
			existing.Name = desired.Durable
			existing.MaxAckPending = 777
			existing.MaxWaiting = 512
			existing.Replicas = 1
			existing.Metadata = map[string]string{"owner": "operator"}
			existing.Description = "managed externally"
			want := desired
			api := &stubConsumerAPI{}
			if tt.change != nil {
				tt.change(api, &existing, &want)
			}
			api.consumer = &stubJetStreamConsumer{info: jetstream.ConsumerInfo{Config: existing}}
			consumer, err := resolveJetStreamConsumer(context.Background(), api, "S-orders", want)
			if tt.wantError != nil {
				require.ErrorIs(t, err, tt.wantError)
			} else {
				require.NoError(t, err)
				require.Same(t, api.consumer, consumer)
			}
			require.Equal(t, tt.lookups, api.lookups)
			require.Equal(t, tt.creates, api.creates)
			require.Equal(t, tt.updates, api.updates)
			if tt.updates > 0 {
				require.Equal(t, existing.Metadata, api.updated.Metadata)
				require.Equal(t, existing.Description, api.updated.Description)
				require.Equal(t, existing.MaxWaiting, api.updated.MaxWaiting)
				require.Equal(t, existing.Replicas, api.updated.Replicas)
				require.Equal(t, want.AckWait, api.updated.AckWait)
			}
		})
	}
}

func TestReconcileJetStreamConsumerRejectsUnsafeChanges(t *testing.T) {
	t.Parallel()
	desired := jetstream.ConsumerConfig{
		Durable: "worker", FilterSubject: "orders.created", AckPolicy: jetstream.AckExplicitPolicy,
		AckWait: 30 * time.Second, MaxDeliver: 5,
	}
	tests := []struct {
		name   string
		change func(*jetstream.ConsumerConfig)
	}{
		{"identity", func(c *jetstream.ConsumerConfig) { c.Durable = "other" }},
		{"filter", func(c *jetstream.ConsumerConfig) { c.FilterSubject = "orders.other" }},
		{"multiple filters", func(c *jetstream.ConsumerConfig) { c.FilterSubjects = []string{"orders.created", "orders.other"} }},
		{"push", func(c *jetstream.ConsumerConfig) { c.DeliverSubject = "push" }},
		{"ack policy", func(c *jetstream.ConsumerConfig) { c.AckPolicy = jetstream.AckNonePolicy }},
		{"start policy", func(c *jetstream.ConsumerConfig) { c.DeliverPolicy = jetstream.DeliverNewPolicy }},
		{"start sequence", func(c *jetstream.ConsumerConfig) { c.OptStartSeq = 10 }},
		{"headers only", func(c *jetstream.ConsumerConfig) { c.HeadersOnly = true }},
		{"backoff conflict", func(c *jetstream.ConsumerConfig) { c.BackOff = []time.Duration{time.Second}; c.AckWait = time.Second }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			existing := desired
			tt.change(&existing)
			api := &stubConsumerAPI{consumer: &stubJetStreamConsumer{info: jetstream.ConsumerInfo{Config: existing}}}
			_, err := resolveJetStreamConsumer(context.Background(), api, "S-orders", desired)
			require.ErrorIs(t, err, ErrInvalidConfig)
			require.Zero(t, api.creates)
			require.Zero(t, api.updates)
		})
	}
}

func TestReconcileJetStreamConsumerNormalizedDefaults(t *testing.T) {
	t.Parallel()
	desired := jetstream.ConsumerConfig{Durable: "worker", FilterSubject: "orders", AckPolicy: jetstream.AckExplicitPolicy}
	existing := desired
	existing.FilterSubject = ""
	existing.FilterSubjects = []string{"orders"}
	existing.AckWait = 30 * time.Second
	existing.MaxDeliver = -1
	existing.MaxAckPending = 42
	updated, changed, err := reconcileJetStreamConsumer(existing, desired)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, existing, updated)
	// A compatible operator backoff is preserved, not cleared.
	existing.BackOff = []time.Duration{30 * time.Second}
	_, changed, err = reconcileJetStreamConsumer(existing, desired)
	require.NoError(t, err)
	require.False(t, changed)
}
