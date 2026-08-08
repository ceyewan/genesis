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
| trace | `Init` and `Discard` install global OTel state; returned shutdown functions are concurrent and idempotent |
| `mq.MQ` | borrows connectors; `Drain(ctx)` is graceful, `Close()` is bounded immediate shutdown |
| `registry.Registry` | borrows etcd; `Shutdown(ctx)` owns watches, leases, and resolver tasks; returned gRPC connections are caller-owned |
| `idgen.Allocator` | borrows connector; `KeepAlive` may start once after `Allocate`; `Stop` cancels and joins it before releasing ownership |
| `dlock.Locker` | borrows connector; `Close` stops renewal and releases locks; new locks fail with `ErrClosed` |
| `idem.Idempotency`, standalone ratelimit, local cache | own their internal cleanup goroutines and stop them on `Close` |
| distributed cache/ratelimit, `db.DB`, multi-cache | borrowers; their `Close` is a no-op and never closes injected dependencies |

Concurrent trace and metrics shutdown callers wait with their own contexts; cancellation of one waiter does not prevent another waiter from observing completion. Redis Stream Drain stops broker reads without canceling an active handler context, and only force-cancels remaining handlers when the Drain context expires.

## Runtime boundaries

- Auth is a local HS256 access/refresh JWT component. Issuer, audience, expiry, signing algorithm and token type are enforced. Machine identities use standard `Subject`, roles and namespaced `Extra` claims; OAuth2/OIDC, revocation and a machine-identity directory are outside this module.
- MQ owns transport acknowledgement, redelivery, durability, queue-group, backpressure, reconnect and drain behavior. Business retry classification and DLQ payload/topic policy remain application concerns.
- JetStream queue-group and durable identities are scoped by topic because one Genesis stream can contain several topics; the same logical name on different topics creates independent consumers.
- Registry Watch emits a linearizable initial snapshot before changes. Unexpected lease loss is reported through `LeaseFailures`.
- Trace uses W3C Trace Context and Baggage for HTTP, gRPC and MQ helpers. OTLP export is asynchronous even for the legacy `simple` batcher value. Optional `ExportErrors` receives failures without blocking exporter workers.
- `service.name`, `service.version`, `service.instance.id` and `deployment.environment` are the shared resource/log field names across trace, metrics and clog.

## Package stability

All importable non-example packages in this module, including `testkit`, are part of the v1 review surface and are listed in the API inventory. `testkit` is stable test support, not production runtime API. Generated packages under `examples` are examples and are not versioned as reusable application contracts. No current runtime package is marked experimental, split into another module, or removed for v1.

`mq` transport implementations are intentionally package-internal. The pre-v1 exported `mq.Transport` name was removed because its methods depended on unexported option-state types and external packages could not implement it. Applications extend MQ behavior through `Middleware` and the public option constructors; adding third-party broker drivers requires a future explicit public driver contract rather than depending on the internal transport interface.
