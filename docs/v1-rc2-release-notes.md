# Genesis v1.0.0-rc.2 release notes

Status: publication-ready.

RC2 is a contract-hardening release. It does not move or replace the immutable
`v1.0.0-rc.1` tag. The release owner has approved the final RC2 package
boundary; publication evidence and remote controls remain pending.

## Highlights

- Idempotency result publication and lock release now form an atomic ownership
  transition in the built-in Memory and Redis stores. The shared execution path
  also rechecks after acquiring a lock for compatibility with third-party
  stores.
- HTTP and gRPC idempotency can bind storage keys and fingerprints to a trusted
  authenticated tenant/principal scope. A configured scope callback fails
  closed when it cannot produce an identity.
- JetStream consumers can issue progress acknowledgements, distinguish a
  subscription-local pull batch from the durable consumer's cluster-wide
  inflight limit, and preserve settlement errors.
- Breaker gRPC fallback now copies a compatible replacement into the caller's
  reply instead of returning an empty success.
- Config loading publishes transactional snapshots and merges environment/file
  values at schema leaves, so one override no longer hides sibling fields.
- Breaker scene state, standalone rate-limiter buckets, idempotency Memory
  entries, and dlock's recent ownership-loss history now have documented bounds
  and classifiable overflow behavior.

## Correctness and lifecycle fixes

- `cache`: classify invalid configuration and destination shapes, prevent a
  concurrent local `Expire` from reviving a deleted key, validate `HGetAll`
  map-key types, and preserve borrowed connector ownership.
- `connector` and `db`: close failed SQLite handles, honor NATS connection
  context/deadlines, clean up late connections, and preserve both component and
  connector error classes.
- `dlock`: reject empty keys and sub-millisecond Redis TTLs, bound recent
  ownership-loss lookup history, preserve acquired loss channels, and prevent
  concurrent same-key Etcd acquisitions through one Locker from sharing and
  deleting the same lease-backed mutex key.
- `metrics`, `registry`, and `trace`: turn invalid configuration into
  classifiable errors, remove panic/deadlock-prone boundaries, make shutdown
  and registration races deterministic, and stabilize resolver output.
- `mq`: validate topics and handlers, correct acknowledgement/error metrics,
  saturate retry backoff safely, and expose optional progress capability.
- `ratelimit`: linearize standalone key lifecycle, add a hard key bound,
  reject non-finite limits, report internal errors, and distinguish backend
  unavailability from a real quota rejection.
- `idgen` and `xerrors`: tighten configuration decoding, close lifecycle and
  collision-domain gaps, and make nil aggregate errors and returned slices
  safe.

## API and compatibility

RC2 retains every RC1-exported sentinel name. The formerly unreachable
`cache.ErrNotSupported`, `mq.ErrSubscriptionClosed`, and
`ratelimit.ErrRateLimitExceeded` are deprecated compatibility-only names and
remain unreturned by production code.

Additive APIs include classifiable configuration/capacity errors, JetStream
`ProgressMessage`, authenticated idempotency scope callbacks, and bounded
idempotency options. The existing `WithBatchSize` option now has concrete
JetStream instance-local `PullMaxMessages` behavior; it is not a newly exported
name. Several public config structs gain exact `mapstructure` tags;
`ratelimit.StandaloneConfig` gains `MaxKeys`. Callers should use keyed struct
literals.

The approved RC2 boundary keeps `dlock` as an extended root-v1 package and
removes 34 RC1 declarations without replacement: public `testkit` moves to
`internal/testkit`, while `trace.HTTPHandler` and `trace.HTTPTransport` are
replaced by direct upstream `otelhttp` calls. Four generated example-proto
packages also move below their owning example's `internal` tree. The resulting
governed inventory contains 17 packages and 705 declarations. `otelhttp`
remains in the module graph only as an indirect dependency of the retained
instrumentation stack; Genesis no longer imports or exposes it directly.

The immutable RC1 declaration baseline and current inventory are now checked
with exact old-to-expected-new pairs plus a separate exact approved-removal
file. The checker includes constant values, variable types, full import paths,
exported struct shape, and promoted method sets rather than treating a
regenerated snapshot as compatibility approval.
Genesis local/hosted checks and Resonance local/hosted checks now use the same
`golangci-lint` `v2.12.2` toolchain.

## Consumer action

- Multi-tenant HTTP/gRPC users must supply the trusted identity-scope callback
  appropriate to their authenticated context. Do not treat a raw user header or
  metadata value as proof of identity.
- Review idempotency key, request, result, and in-memory entry limits. An
  oversized successful result is returned to its caller but deliberately not
  cached, so a later request may execute again.
- Treat `dlock.Lost(key)` as an ownership signal, and use downstream CAS or
  version fencing for irreversible writes. Genesis dlock does not issue
  fencing tokens.
- For JetStream, use `WithBatchSize` for per-subscription buffering and reserve
  `WithMaxInflight` for the shared durable consumer's cluster-wide
  `MaxAckPending` policy.
- Update config literals to keyed form and handle the new classifiable errors
  with `errors.Is`.
- Replace `trace.HTTPHandler`/`trace.HTTPTransport` with
  `otelhttp.NewHandler`/`otelhttp.NewTransport`.
- External users of the former `testkit` or example generated proto paths must
  own their fixtures or schemas; importing Genesis internal paths is not a
  supported migration.

See [the migration guide](https://github.com/ceyewan/genesis/blob/v1.0.0-rc.2/docs/v1-migration.md)
for the complete caller checklist and
[the compatibility matrix](https://github.com/ceyewan/genesis/blob/v1.0.0-rc.2/docs/v1-compatibility.md)
for the tested toolchain and backends. These tag-qualified links keep the same
meaning when this file is published byte-for-byte as the GitHub Release body.

## Pending after tag publication

- Capture public Go proxy module checksums and verify the annotated tag object
  and peeled commit. A fresh-cache direct VCS `Origin.Hash`, when available, is
  supplemental cross-check evidence rather than a required proxy field.
- Upgrade Resonance through a protected PR with no local replacement, then run
  all nine stage-two hosted checks and the stage-three production-like Compose,
  IM/Agent E2E, recovery, telemetry, and benchmark matrix.

## Pending before stable-v1 promotion

- Approve and complete the proposed seven-consecutive-day RC2 observation
  window before promoting the same commit to stable v1; production/API/
  dependency changes or a P0/P1 defect require a new RC and restart the window.

The authoritative status and evidence fields are in
[the RC2 candidate evidence](https://github.com/ceyewan/genesis/blob/v1.0.0-rc.2/docs/v1-rc2-evidence.md).
None of the Pending items is claimed complete by this draft.
