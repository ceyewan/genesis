# Migrating from v0.5.0 to v1.0.0

Genesis v1 tightens failure and lifecycle contracts. Most changes are source compatible, but the items below require caller review.

## Required source changes

### UUID errors

```go
// v0.5
id := idgen.UUID()

// v1
id, err := idgen.UUID()
if err != nil {
	return err
}
```

### IDGen TTL values

`SequencerConfig.TTL` and `AllocatorConfig.TTL` now use `time.Duration`, not integer seconds.

```go
TTL: 30 * time.Second
```

Allocator `KeepAlive` can be started once after a successful `Allocate`. Consume its error channel until it closes and treat any value as loss of worker ownership. `Stop` is safe to call concurrently and waits for the keepalive task before releasing the lease.

Sequencer `MaxValue` no longer wraps to the beginning. Handle `errors.Is(err, idgen.ErrSequenceExhausted)` and provision a new business namespace or remove the limit.

### Config loader lifecycle

`config.Loader` now has `Close() error`. Applications that call `Watch` must close the loader during shutdown:

```go
loader, err := config.New(cfg)
if err != nil {
	return err
}
defer loader.Close()
```

### Graceful MQ and registry shutdown

Use `mq.Drain(ctx)` when active handlers should finish. Use `mq.Close()` for bounded immediate shutdown. Use `registry.Shutdown(ctx)` when the application owns a shutdown deadline; `Close()` remains a five-second compatibility wrapper. Close every `*grpc.ClientConn` returned by `registry.GetConnection` yourself.

### Trace and metrics shutdown

Both shutdown paths are now concurrent and idempotent. Trace's legacy `Batcher: "simple"` value remains accepted but is asynchronous; it no longer makes `span.End` wait for OTLP. Supply a buffered `trace.Config.ExportErrors` channel if the application needs exporter-failure alerts.

## Configuration review

- Constructors copy configs before defaults. Do not mutate a config to reconfigure a live component; construct a replacement instead.
- Public TTLs use `time.Duration`. Replace bare numeric values with explicit units.
- Etcd-backed TTLs must be at least one second.
- NATS JetStream now exposes `AckWait`, `MaxDeliver`, retention, storage, max age, max bytes and replicas. Review auto-created stream settings before production use.
- Observability configs support `Version`, `InstanceID` and `Environment`; set the same values for trace, metrics and clog.
- Connector and registry constructors accept `WithMeter` for internal health, reconnect, registration, watch and lease metrics.

## Behavior changes to test

- MQ `Publish` returns only after broker acknowledgement. Manual Ack/Nak is the default; delayed Nak is JetStream-only. Drain waits for active handlers within the supplied context.
- Registry Watch begins with PUT events for the current snapshot and then streams changes without a Get/Watch gap.
- Auth validates access versus refresh token type in addition to signature, issuer, audience and expiry. `GinMiddleware` accepts access tokens only.
- dlock rejects new acquisitions after `Close` with `ErrClosed`.
- Connector construction validates configuration but does not connect; call `Connect` and classify failures with `errors.Is(err, connector.ErrConnection)`.

The full frozen surface and cross-package decisions are recorded in [v1-api-inventory.md](./v1-api-inventory.md) and [v1-api-decisions.md](./v1-api-decisions.md).
