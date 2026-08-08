# Genesis v1.0.0-rc.1 verification evidence

This record describes the local-only candidate verification performed on 2026-08-08. Local commits are authorized. The exact commit containing this record is written to the stage goal after commit creation. No remote push, tag, release, deployment, or hosted-CI run is claimed here.

## Environment

- Go: `go1.26.1 darwin/arm64`
- Docker client/server: `29.4.0` / `29.4.0`
- Remediation baseline: `430e2f8f9cd403a5c9d9a3b581fe287c9853072b`
- Scope: all 18 externally importable packages, plus examples and internal API-inventory tooling

## Full local gates

The following commands completed successfully against the final candidate worktree:

```text
go test -count=1 ./...
go test -race -count=1 ./...
go build ./...
make examples-check
make example-all
make lint
make exported-comments
make modernize-check
make buf-lint
make api-inventory
make api-inventory-check
make godoc-artifacts
git diff --check
```

The ordinary and race suites executed real Docker/Testcontainers dependencies rather than skipping them. They covered Redis, etcd, PostgreSQL, MySQL, NATS JetStream, and Kafka. Connector tests took about 60 seconds in both suites; registry, dlock, DB, MQ, idem, idgen, and ratelimit also ran their container-backed cases. `make example-all` exited successfully without a separately started development stack: local examples ran normally, while external-service examples reported their documented classifiable connection failures or skips. Successful external paths are covered by Testcontainers.

`make godoc-artifacts` generated exactly 18 package artifacts. `make api-inventory-check` passed after regeneration from `internal/cmd/apiinventory`.

## Directed contract evidence

The full ordinary and race suites include regression coverage for the audit findings:

- Config: environment-only `Unmarshal` and transactional failed reload.
- Connector/DB: timeout fields take effect, dependency causes remain discoverable, and nil/closed connector clients return errors instead of panicking.
- Cache/testkit/xerrors: default local TTL, injectable serializers, typed Redis raw client, true discard test meter, isolated SQLite DSNs, and immutable error helper functions.
- Metrics/trace/registry: every concurrent shutdown caller observes its own context while an internal bounded cleanup continues; listener address and listen-error classification are tested; resolver endpoint replacement and Register/Shutdown races are covered.
- Idempotency/dlock: stale owners cannot commit, endpoint/request fingerprints detect conflicts, short TTL renewals work, ownership loss is observable, and failed unlock/release can be retried or reported.
- MQ: retained-history/default and latest start positions, Redis/JetStream persistent consumption, multi-topic durable/group identity isolation, retry/DLQ edge cases, Ack error propagation, reconnect, and Drain behavior.
- Breaker/ratelimit/idgen: meaningful breaker assertions, bounded key cardinality, fallback results, failure classification, non-serializing rate-limit Wait, terminal Close, typed drivers, composable defaults, and rejecting invalid Snowflake parsing.

## API and CI audit

- [v1-api-inventory.md](./v1-api-inventory.md) is generated and drift-checked in CI.
- [v1-api-decisions.md](./v1-api-decisions.md) records constructor, error, ownership, global-state, third-party type, no-op, durability, and lifecycle decisions.
- [v1-migration.md](./v1-migration.md) includes the source-breaking changes found during both audits.
- Every importable package has a compile-checked Go Example and generated `go doc -all` evidence.
- `.github/workflows/ci.yml` now gates PRs, pushes to `main`, and `v*` tags with separate ordinary/race Testcontainers jobs, pinned lint/Buf tooling, build/examples/API/GoDoc checks, artifacts, and a tag-only release gate depending on every job.
- The workflow file was parsed locally and its constituent commands passed locally. This is not represented as a hosted GitHub Actions result because pushing is outside the stage authorization.

## Accepted v1 boundaries

- Gin, gRPC, GORM, OpenTelemetry, slog, JWT, and native connector client types are intentional v1 public contracts.
- Registry resolver registration and trace/metrics providers remain process-global; applications initialize one production instance and tests must serialize global replacement.
- Necessary no-op methods remain only for discard implementations, borrowed-resource `Close`, and synchronous slog `Flush` compatibility.
- Redis broadcast is retained-stream delivery without server-side acknowledgement/redelivery. Persistent at-least-once paths use Redis consumer groups or JetStream durable consumers.
- Dlock exposes ownership loss but does not issue fencing tokens; irreversible downstream writes still require CAS/version fencing.
- Trace supports system TLS and static OTLP headers, but custom certificate-pool construction remains application/deployment configuration outside Genesis v1.

No unresolved P0/P1 API, correctness, migration, or CI-gate risk identified by the stage audits remains. Future feature expansion is not a stage-one blocker.
