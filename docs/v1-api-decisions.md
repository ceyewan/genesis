# Genesis v1 API and lifecycle decisions

This document freezes the cross-package rules used by the `v1.0.0-rc.1` candidate. The exhaustive symbol surface is in [v1-api-inventory.md](./v1-api-inventory.md).

## Constructor contract

Every constructor owns a copy of the supplied configuration before applying defaults. Mutating the caller's struct or its slice fields after construction does not change a live component. Functional options are applied after configuration defaults and validation; they inject collaborators or behavior that is not represented by a config field. A nil logger or meter option becomes its package's discard implementation.

Nil configuration is accepted only for constructors whose zero value is a complete, useful configuration:

| Constructor | Nil config |
| --- | --- |
| `clog.New`, `config.New`, `db.New`, `breaker.New`, `registry.New`, `cache.NewMulti` | accepted; documented defaults are used |
| connector constructors, `auth.New`, `cache.NewDistributed`, `cache.NewLocal`, `dlock.New`, `idem.New`, `idgen.New*`, `mq.New`, `ratelimit.New`, `metrics.New`, `trace.Init` | rejected with a classifiable configuration error |
| `metrics.NewHTTPServerMetrics`, `metrics.NewGRPCServerMetrics` | rejected when the meter or config is nil |

Connector construction never performs network I/O. `Connect` establishes and validates the connection. Components that receive a connector borrow it and never close it. Configuration-bearing metrics helpers and `WithBuckets` also copy slice inputs rather than retaining caller-owned backing arrays.

## Time and TTL contract

Public durations and TTLs use `time.Duration`. Zero means “use the documented default” or “no expiry” only where the field or method explicitly says so. Negative durations are invalid. Etcd leases are second-granularity and reject values below one second; Redis ID allocator leases support millisecond precision.

`idgen.SequencerConfig.MaxValue` is an exhaustion boundary. `Next` and `NextBatch` return `ErrSequenceExhausted` atomically and leave the stored value unchanged; sequences never reset or wrap.

## Error contract

Exported sentinel errors remain usable through `errors.Is`. Wrapped dependency failures preserve their cause. Constructors identify the invalid field or missing dependency. Context cancellation and dependency sentinels are classified with `errors.Is`/`errors.As`, not direct equality.

`idgen.UUID` returns `(string, error)`. Entropy-source failures are never converted to an empty string, panic, or silent fallback.

## Lifecycle and ownership

| Component | Contract |
| --- | --- |
| connectors | own their client/connection; `Close` is repeatable |
| `config.Loader` | owns fsnotify and subscription goroutines; `Close` is concurrent and idempotent and closes Watch channels |
| `clog.Logger` | only the root owns the writer; derived loggers never close the shared writer; root `Close` is idempotent |
| `metrics.Meter` | owns its HTTP server and provider; `Shutdown(ctx)` is concurrent and idempotent |
| trace | `Init` and `InstallLocalProvider` install global OTel state; returned shutdown functions are concurrent and idempotent |
| `mq.MQ` | borrows connectors; `Drain(ctx)` is graceful, `Close()` is bounded immediate shutdown |
| `registry.Registry` | borrows etcd; `Shutdown(ctx)` owns watches, leases, and resolver tasks; returned gRPC connections are caller-owned |
| `idgen.Allocator` | borrows connector; `KeepAlive` may start once after `Allocate`; `Stop` cancels and joins it before releasing ownership |
| `dlock.Locker` | borrows connector; `Close` stops renewal and releases locks; new locks fail with `ErrClosed` |
| `idem.Idempotency`, standalone ratelimit, local cache | own their internal cleanup goroutines and stop them on `Close` |
| distributed cache, `db.DB`, multi-cache | borrowers; their `Close` is a no-op and never closes injected dependencies |
| standalone/distributed ratelimit | `Close` is terminal; distributed mode still borrows and never closes Redis |

Concurrent trace, metrics, and registry shutdown callers wait with their own contexts. The first short-lived caller cannot permanently abort cleanup: one internal five-second cleanup task continues, while each caller may stop waiting independently. Redis Stream Drain stops broker reads without canceling an active handler context; at deadline it cancels the handler context and returns, but Go cannot forcibly terminate a handler that ignores context cancellation.

`dlock.Locker.Lost(key)` is the minimum ownership-loss signal for v1. A normal unlock closes the channel without a value; renewal/session loss sends `ErrOwnershipLost` and then closes it. Failed remote unlock retains local state so the caller may retry with a new context. Genesis does not issue fencing tokens, so irreversible downstream writes still require a version/CAS fence.

`idgen.Allocator.KeepAlive` retains its channel model: an error value means ownership was lost; clean channel closure means caller cancellation or Stop. `Stop() error` joins the keepalive task and reports token/lease release failures instead of swallowing them.

## Runtime boundaries

- Auth is a local HS256 access/refresh JWT component. Issuer, audience, expiry, signing algorithm and token type are enforced. Machine identities use standard `Subject`, roles and namespaced `Extra` claims; OAuth2/OIDC, revocation and a machine-identity directory are outside this module.
- MQ owns transport acknowledgement, redelivery, durability, queue-group, backpressure, reconnect and drain behavior. Business retry classification and DLQ payload/topic policy remain application concerns.
- Redis Stream consumer groups and JetStream durable consumers provide the persistent at-least-once path. Redis broadcast has no server-side acknowledgement/re-delivery guarantee. New subscriptions start from retained history by default; `FromLatest` and `FromID` make another initial position explicit.
- JetStream queue-group and durable identities are scoped by topic because one Genesis stream can contain several topics; the same logical name on different topics creates independent consumers.
- Registry Watch emits a linearizable initial snapshot before changes. Unexpected lease loss is reported through `LeaseFailures`.
- Registry lease-failure reporting is buffered and non-blocking; a full buffer drops the newest notification with an error log so lifecycle workers cannot leak behind an unconsumed channel. `GetService` returns an empty non-nil slice for a missing service.
- Trace uses W3C Trace Context and Baggage for HTTP, gRPC and MQ helpers. OTLP export is asynchronous for both `BatcherBatch` and `BatcherImmediate`; optional headers support authenticated OTLP endpoints. `InstallLocalProvider` deliberately names its global side effect. Optional `ExportErrors` receives failures without blocking exporter workers.
- `service.name`, `service.version`, `service.instance.id` and `deployment.environment` are the shared resource/log field names across trace, metrics and clog.

## Package stability

All importable non-example packages in this module, including `testkit`, are part of the v1 review surface and are listed in the API inventory. `testkit` is stable test support, not production runtime API. Generated packages under `examples` are examples and are not versioned as reusable application contracts. No current runtime package is marked experimental, split into another module, or removed for v1.

`mq` transport implementations are intentionally package-internal. The pre-v1 exported `mq.Transport` name was removed because its methods depended on unexported option-state types and external packages could not implement it. Applications extend MQ behavior through `Middleware` and the public option constructors; adding third-party broker drivers requires a future explicit public driver contract rather than depending on the internal transport interface.

## Third-party and global-state contract

Genesis v1 intentionally binds the following third-party public types: `slog.Attr`, Gin handlers/contexts, gRPC interceptors/connections/resolvers, GORM databases, OpenTelemetry providers/instruments, JWT claims, and raw Redis/etcd/NATS/Kafka clients. These are deliberate adapter and escape-hatch contracts; upgrading an incompatible upstream major version may therefore require a Genesis major version. `cache.Distributed.RawClient` returns `*redis.Client`, not `any`, because the only v1 distributed cache driver is Redis.

Registry's resolver registration and the OpenTelemetry trace/metrics providers remain process-global by design. Applications initialize one production instance during bootstrap. Tests that replace global state must not run such initialization in parallel and must call Shutdown. A shutdown only resets the global provider if it still points at the instance being closed, so closing an older instance cannot erase a newer provider.

No-op methods are retained only where they encode a stable ownership/capability rule: borrower `Close` methods never close injected connectors, discard implementations satisfy the same interfaces, and `clog.Flush` is a no-op for synchronous slog handlers but remains an extension point for buffered handlers. Unreachable sentinels and options with no implementation are removed before v1 rather than frozen as placeholders.

Breaker keys are bounded by `Config.MaxKeys` (default 1024); exceeding it returns `ErrKeyLimitExceeded` rather than growing memory without limit. `WithFallback` returns a real replacement `(any, error)`, `WithFailureClassifier` controls the generic error failure boundary, and `WithMeter` emits execution, rejection, and state metrics. Key labels are therefore restricted to the same configured low-cardinality set.

Allocator and Sequencer drivers use the typed `idgen.DriverType` constants. Allocator's default `MaxID` is 32 so it composes with the default multi-datacenter generator layout; users of single-datacenter mode may explicitly raise it to 1024. `ParseGeneratorID` rejects negative IDs and unknown modes instead of silently choosing multi-datacenter parsing.

Metrics HTTP exposure has an explicit `ListenAddress`, classifiable `ErrInvalidConfig`/`ErrListen` errors, and an optional non-blocking `ServerErrors` channel. Development defaults bind loopback; production defaults bind all interfaces, and applications should override this according to their network boundary.

## Configuration, encoding, and idempotency

`config.Loader.Load` builds and validates a new snapshot before publishing it. Environment-only keys are materialized for both root underscore tags and dotted nested structs so `Get` and `Unmarshal` observe the same values.

Cache JSON/MessagePack changes are not wire-compatible. Deployments must invalidate old entries or change `KeyPrefix`; custom serializers are injected with `cache.WithSerializer`. HTTP and gRPC idempotency keys are scoped by endpoint/full method and stored with a deterministic request fingerprint. Reusing a key for a different request returns `idem.ErrKeyConflict`; result commit and lock deletion are atomic and require the current ownership token.
