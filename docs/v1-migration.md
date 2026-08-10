<!-- markdownlint-disable-file MD013 -->

# Migrating from v0.5.0 to v1.0.0

Genesis v1 tightens failure and lifecycle contracts. Most changes are source compatible, but the items below require caller review.

The currently published preview of this contract is `v1.0.0-rc.1` at
`ec5ad2c31fb4adce2bd42529e3d7fbfe92b23aa7`. The RC is immutable: later test
or documentation commits do not alter that module artifact. Production fixes
require a separately approved and published RC, which consumers must select
explicitly.

## From v1.0.0-rc.1 to the RC2 candidate

RC2 retains every RC1 exported sentinel name. Three unreachable sentinels are
deprecated but intentionally retained so existing source still compiles:
`cache.ErrNotSupported`, `mq.ErrSubscriptionClosed`, and
`ratelimit.ErrRateLimitExceeded`. Do not add new handling branches for them;
current implementations never return them.

RC2 deliberately removes 34 RC1 declarations from the stable public surface:
the 32-item public `testkit` package and two zero-logic trace HTTP wrappers. It
also internalizes four generated example-proto packages that the inventory had
previously excluded despite their importable paths. These removals are recorded
exactly in `v1-api-compat-removals.md`; they are not hidden as ordinary
source-compatible hardening.

The reviewed source-compatibility exception is configuration structure shape:

- `breaker`, `cache`, `clog`, `dlock`, `idem`, `idgen`, `ratelimit`, and `registry` configs gain
  explicit `mapstructure` tags so nested snake_case configuration decodes as
  documented.
- `ratelimit.StandaloneConfig` gains `MaxKeys`, defaulting to 10000. Replace
  positional literals with keyed literals before upgrading:

```go
// Fragile across added fields.
// ratelimit.StandaloneConfig{time.Minute, 5 * time.Minute}

cfg := ratelimit.StandaloneConfig{
	CleanupInterval: time.Minute,
	IdleTimeout:     5 * time.Minute,
	MaxKeys:         10_000,
}
```

Other RC2 additions are source-compatible:

- `mq.ProgressMessage` exposes JetStream `InProgress()` without widening the
  base `Message` interface. Use a type assertion and stop heartbeats before
  `Ack`, `Nak`, or handler return. Redis messages do not implement it.
- Explicit `mq.WithBatchSize(n)` now maps to JetStream's instance-local
  `PullMaxMessages`; `WithMaxInflight(n)` remains the shared durable's
  cluster-wide `MaxAckPending`. Do not set the latter from a single replica's
  worker count.
- `trace.ErrInvalidConfig`, `registry.ErrInvalidConfig`, `dlock.ErrInvalidConfig`,
  `idem.ErrInvalidConfig`,
  `cache.ErrInvalidDestination`, `ratelimit.ErrInvalidConfig`, and
  `ratelimit.ErrKeyLimitExceeded` make previously raw failures classifiable.
- A successful breaker gRPC fallback now copies its replacement result into
  the caller's reply. A replacement with the wrong type returns `Internal`
  rather than silently returning an empty reply.
- `config.Loader` merges file and environment values at individual leaves;
  overriding one nested field no longer hides its file-backed siblings.

The minimum supported toolchain is Go 1.26.0; release CI also tests the pinned
current patch version, Go 1.26.5. The immutable RC1 inventory, current
inventory, and reviewed exceptions are checked by `make api-compat-check` and
must not be replaced with an unreviewed regenerated snapshot.

### Approved RC2 surface removals

`github.com/ceyewan/genesis/testkit` is now `internal/testkit` and cannot be
imported by another module. External projects must maintain their own
Testcontainers fixtures, logger/meter helpers, temporary IDs, and cleanup
policy. Do not rewrite an external import to the internal path; Go correctly
rejects that dependency.

The generated packages formerly available at these paths are also internal:

- `examples/grpc-registry/proto`
- `examples/idem/proto`
- `examples/observability/proto`
- `examples/proto/demo/v1`

They were example implementation assets, not supported schemas. Copy the
relevant `.proto` definition into the consuming module, choose an owned
`go_package`, and regenerate code. RC2 keeps their protobuf package names and
field numbers unchanged inside the examples, avoiding an unnecessary wire
identity change.

Replace the removed trace wrappers with the upstream OpenTelemetry functions:

```go
handler := otelhttp.NewHandler(baseHandler, "operation", options...)
transport := otelhttp.NewTransport(baseTransport, options...)
client := &http.Client{Transport: transport}
```

`trace.Init` still installs the global W3C Trace Context/Baggage propagator and
TracerProvider used by `otelhttp`; only the redundant Genesis wrappers were
removed. The RC2 module no longer lists `otelhttp` as a direct dependency,
although the retained instrumentation stack still brings it into the build
list indirectly.

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

Allocator `KeepAlive` can be started once after a successful `Allocate`. Consume its error channel until it closes: a received error means ownership was lost; a channel that closes without a value means the caller canceled it or `Stop` began. `Stop() error` is safe to call concurrently, waits for the keepalive task, and reports lease-release failures.

Use `idgen.DriverRedis` / `idgen.DriverEtcd` instead of bare driver strings. Allocator's default `MaxID` changed from 1024 to 32 so it can be passed directly to the default multi-datacenter Generator; explicitly set 1024 for `GeneratorModeSingleDC`. `ParseGeneratorID` now returns a fifth `error` result and rejects negative IDs or unknown modes.

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

Environment-only unmarshalling now resolves names against the destination schema. Fields such as
`database.max_open_conns` therefore use `PREFIX_DATABASE_MAX_OPEN_CONNS` without treating the
underscore inside `max_open_conns` as another nesting boundary.

### Gin idempotency middleware type

`idem.Idempotency.GinMiddleware` now returns `gin.HandlerFunc` instead of `any`. Remove pre-v1
type assertions and pass it directly to Gin:

```go
router.Use(component.GinMiddleware())
```

### Graceful MQ and registry shutdown

Use `mq.Drain(ctx)` when active handlers should finish. Use `mq.Close()` for bounded immediate shutdown. Use `registry.Shutdown(ctx)` when the application owns a shutdown deadline; `Close()` remains a five-second compatibility wrapper. Close every `*grpc.ClientConn` returned by `registry.GetConnection` yourself.

### Trace and metrics shutdown

Both shutdown paths are now concurrent and idempotent. Replace `trace.Discard` with `trace.InstallLocalProvider`, and replace `Batcher: "simple"` with `Batcher: trace.BatcherImmediate`. Both batch modes remain asynchronous. Supply a buffered `trace.Config.ExportErrors` channel if the application needs exporter-failure alerts; static OTLP authentication headers can be supplied through `trace.Config.Headers`.

### External interface implementations

Several public interfaces gained lifecycle or delivery methods. Ordinary callers of the constructors do not need adapter changes, but mocks, wrappers and third-party implementations must add the following methods:

| Interface | Added in v1 |
| --- | --- |
| `config.Loader` | `Close() error` |
| `idem.Idempotency` | `Close() error` |
| `mq.MQ` | `Drain(context.Context) error` |
| `mq.Message` | `NakWithDelay(time.Duration) error` |
| `mq.Subscription` | `Drain(context.Context) error` |
| `registry.Registry` | `LeaseFailures() <-chan LeaseFailure`, `Shutdown(context.Context) error` |
| `dlock.Locker` | `Lost(string) <-chan error` |

The unusable pre-v1 `mq.Transport` symbol has been removed. It exposed package-private option-state types, so external packages could not implement it. MQ middleware remains the supported extension point; a future third-party driver API would require a separate public contract.

`xerrors.New`, `Is`, `As`, `Unwrap`, and `Join` are now functions rather than assignable process-global variables. Calls remain source-compatible; code that reassigned these names must use an explicit test seam instead.

`mq.DefaultRetryConfig` is now `mq.DefaultRetryConfig()` so every caller receives an independent value. Redis/JetStream subscriptions start from retained history by default; use `mq.FromLatest()` to preserve the earlier “new messages only” behavior or `mq.FromID(...)` for an explicit backend position.

`cache.WithMeter` was removed because it did not emit metrics. `cache.Distributed.RawClient()` now returns `*redis.Client` instead of `any`. The unused registry watch/connection sentinels and unused ratelimit metric names were also removed rather than frozen.

`breaker.FallbackFunc` now returns `(any, error)` so a successful fallback carries an actual result. Configure `MaxKeys` for the expected low-cardinality key space and handle `ErrKeyLimitExceeded`; use `WithFailureClassifier` for non-gRPC business errors. Breaker metrics are available through `WithMeter`.

Ratelimit `Close` is terminal in both standalone and distributed modes; later calls return `ErrLimiterClosed` (the distributed limiter still does not own Redis). An omitted middleware `ErrorPolicy` keeps the documented fail-open default, while an explicit unknown value now fails closed so configuration typos cannot disable hard protection. Gin reports limiter unavailability as HTTP 503 rather than conflating it with a real HTTP 429 quota rejection. Standalone `Wait` no longer serializes concurrent `Allow` calls on the same key.

## Configuration review

- Constructors copy configs before defaults. Do not mutate a config to reconfigure a live component; construct a replacement instead.
- Public TTLs use `time.Duration`. Replace bare numeric values with explicit units.
- Public duration values are never silently defaulted when negative. Zero keeps each field's documented default/no-expiry meaning; negative values return a configuration or `ErrInvalidTTL` error.
- Etcd-backed TTLs must be at least one second.
- NATS JetStream now exposes `AckWait`, `MaxDeliver`, retention, storage, max age, max bytes and replicas. Review auto-created stream settings before production use.
- JetStream queue-group and durable consumer identities are now scoped by topic. Reusing one logical name across topics no longer rewrites another subscription's filter, but the physical JetStream durable name differs from pre-v1 candidates.
- Observability configs support `Version`, `InstanceID` and `Environment`; set the same values for trace, metrics and clog.
- Metrics HTTP exposure supports `ListenAddress` and `ServerErrors`; listen failures classify as `metrics.ErrListen`. Development defaults bind loopback, while production defaults bind all interfaces.
- Connector and registry constructors accept `WithMeter` for internal health, reconnect, registration, watch and lease metrics.
- The auth default token source is now only `Authorization: Bearer`. Query and cookie sources require an explicit `TokenLookup` because they can leak through URLs, logs, and browser history.
- Switching cache serialization between JSON and MessagePack requires invalidating existing entries or changing the cache key prefix. Use `cache.WithSerializer` to inject a custom implementation.
- HTTP/gRPC idempotency keys are endpoint-scoped and request-fingerprinted. Handle `idem.ErrKeyConflict` (HTTP middleware maps it to 409; gRPC maps it to AlreadyExists).
- Multi-tenant idempotency entry points must derive a stable tenant/principal scope from authenticated context with `idem.WithHTTPIdentityScopeFunc` or `idem.WithGRPCIdentityScopeFunc`. Empty or failed scope extraction returns HTTP 401 / gRPC `Unauthenticated`; untrusted request headers or metadata are not identity proof.
- Idempotency raw keys now default to a 256-byte limit (`idem.WithMaxKeyBytes`), cached results and captured HTTP responses to 1 MiB (`idem.WithMaxResultBytes`), keyed HTTP request bodies to 1 MiB (`idem.WithHTTPMaxRequestBytes`), and the built-in Memory store to 10000 logical keys (`idem.WithMemoryMaxEntries`). Handle `idem.ErrKeyTooLong` and `idem.ErrStoreCapacity`; HTTP maps them to 400/503, gRPC to `InvalidArgument`/`ResourceExhausted`, and HTTP request-body overflow to 413.
- An oversized successful result is returned but not cached. It keeps the execution-time lock but deliberately permits later or already-waiting same-key calls to execute again after unlock; keep reusable success results within the configured bound when replay is unacceptable.
- Classify every `idem.New` configuration failure with `idem.ErrInvalidConfig`; nil config also preserves `idem.ErrConfigNil`. Missing or unconnected Redis dependencies match both `idem.ErrConnectorNil` and `connector.ErrClientNil`.
- Cache unsupported-driver and invalid `max_entries` failures now match `cache.ErrInvalidConfig`; MQ missing-connector failures match `connector.ErrClientNil`; metrics HTTP/gRPC helper nil meter or config failures match `metrics.ErrInvalidConfig`.
- Registry `LeaseFailures` is buffered and non-blocking; configure `LeaseFailureBuffer` and consume promptly. `GetService` returns a non-nil empty slice when no instance exists.

## Behavior changes to test

- MQ `Publish` returns only after broker acknowledgement. Manual Ack/Nak is the default; delayed Nak is JetStream-only. Drain waits for active handlers within the supplied context.
- Registry Watch begins with PUT events for the current snapshot and then streams changes without a Get/Watch gap.
- Auth validates access versus refresh token type in addition to signature, issuer, audience and expiry. `GinMiddleware` accepts access tokens only.
- dlock rejects empty keys with `ErrInvalidKey`, Redis TTL values below 1ms with
  `ErrInvalidTTL`, and new acquisitions after `Close` with `ErrClosed`. Monitor
  `Lost(key)` from the start of the critical section and use downstream
  fencing/CAS for irreversible writes.
- Connector construction validates configuration but does not connect; call `Connect` before injecting a connector into cache, dlock, idem, idgen, mq, ratelimit, registry, or DB. Constructors reject an unconnected connector with an error classifiable as `connector.ErrClientNil`; classify connection failures with `errors.Is(err, connector.ErrConnection)`.

The full frozen surface and cross-package decisions are recorded in [v1-api-inventory.md](./v1-api-inventory.md) and [v1-api-decisions.md](./v1-api-decisions.md).
