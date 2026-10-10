package mq

import (
	"context"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/ceyewan/genesis/xerrors"
)

// jetStreamConsumerAPI contains only the operations needed during startup.
type jetStreamConsumerAPI interface {
	Consumer(context.Context, string, string) (jetstream.Consumer, error)
	CreateConsumer(context.Context, string, jetstream.ConsumerConfig) (jetstream.Consumer, error)
	UpdateConsumer(context.Context, string, jetstream.ConsumerConfig) (jetstream.Consumer, error)
}

func resolveJetStreamConsumer(ctx context.Context, api jetStreamConsumerAPI, stream string, desired jetstream.ConsumerConfig) (jetstream.Consumer, error) {
	if desired.Durable == "" {
		return api.CreateConsumer(ctx, stream, desired)
	}

	consumer, err := api.Consumer(ctx, stream, desired.Durable)
	if xerrors.Is(err, jetstream.ErrConsumerNotFound) {
		consumer, err = api.CreateConsumer(ctx, stream, desired)
		if !xerrors.Is(err, jetstream.ErrConsumerExists) {
			return consumer, err
		}
		// Another instance created this durable. Read and validate its effective
		// configuration rather than falling back to an unconditional update.
		consumer, err = api.Consumer(ctx, stream, desired.Durable)
	}
	if err != nil {
		return nil, err
	}

	updated, changed, err := reconcileJetStreamConsumer(consumer.CachedInfo().Config, desired)
	if err != nil {
		return nil, err
	}
	if !changed {
		return consumer, nil
	}
	// Update-only must not recreate a consumer deleted after the lookup.
	return api.UpdateConsumer(ctx, stream, updated)
}

// reconcileJetStreamConsumer compares Genesis-owned fields only. Starting from
// the effective configuration preserves operator settings and server defaults.
func reconcileJetStreamConsumer(existing, desired jetstream.ConsumerConfig) (jetstream.ConsumerConfig, bool, error) {
	filterMatches := existing.FilterSubject == desired.FilterSubject && len(existing.FilterSubjects) == 0
	if existing.FilterSubject == "" && len(existing.FilterSubjects) == 1 {
		filterMatches = existing.FilterSubjects[0] == desired.FilterSubject
	}
	if existing.Durable != desired.Durable ||
		(existing.Name != "" && existing.Name != desired.Durable) ||
		!filterMatches || existing.DeliverSubject != "" || existing.DeliverGroup != "" ||
		existing.AckPolicy != desired.AckPolicy ||
		existing.DeliverPolicy != desired.DeliverPolicy || existing.OptStartSeq != desired.OptStartSeq ||
		existing.OptStartTime != nil || existing.ReplayPolicy != desired.ReplayPolicy ||
		existing.HeadersOnly {
		return jetstream.ConsumerConfig{}, false, xerrors.Wrapf(ErrInvalidConfig,
			"durable %s has incompatible identity, filter, delivery or acknowledgement settings", desired.Durable)
	}

	ackWait := desired.AckWait
	if ackWait == 0 {
		ackWait = 30 * time.Second
	}
	maxDeliver := desired.MaxDeliver
	if maxDeliver == 0 {
		maxDeliver = -1
	}
	// BackOff overrides AckWait; silently rewriting it would change an
	// operator-managed retry schedule or let the configuration oscillate.
	if len(existing.BackOff) > 0 && (existing.AckWait != ackWait || existing.MaxDeliver != maxDeliver) {
		return jetstream.ConsumerConfig{}, false, xerrors.Wrapf(ErrInvalidConfig,
			"durable %s has a conflicting operator-managed backoff", desired.Durable)
	}

	updated := existing
	updated.AckWait = ackWait
	updated.MaxDeliver = maxDeliver
	// An omitted inflight limit inherits the effective server/stream setting,
	// not a guessed default that could overwrite operator configuration.
	if desired.MaxAckPending > 0 {
		updated.MaxAckPending = desired.MaxAckPending
	}
	changed := updated.AckWait != existing.AckWait || updated.MaxDeliver != existing.MaxDeliver ||
		updated.MaxAckPending != existing.MaxAckPending
	return updated, changed, nil
}
