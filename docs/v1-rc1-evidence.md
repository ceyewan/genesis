# Genesis v1.0.0-rc.1 verification evidence

This record describes the local and GitHub-hosted verification that culminated
in the published `v1.0.0-rc.1` prerelease on 2026-08-09. The final protected
merge was PR [#61](https://github.com/ceyewan/genesis/pull/61), and the annotated
tag resolves to `ec5ad2c31fb4adce2bd42529e3d7fbfe92b23aa7`.

## Environment

- Go: `go1.26.1 darwin/arm64`
- Docker client/server: `29.4.0` / `29.4.0`
- Original remediation baseline: `48acd69eea91764bc8452ea3f55dfe7a2a027607`
- Final pre-RC audit baseline: `c80babaf35f71d656b85e28b8ae02a54e06ce7ce`
- Published RC baseline: `ec5ad2c31fb4adce2bd42529e3d7fbfe92b23aa7`
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

An independent rerun against `c80baba` exposed a concurrent local race-suite readiness failure: MySQL exceeded the module's default 60-second startup wait and three JetStream tests consumed their five-second business contexts while starting NATS containers. The first remediated `main` run later exposed a second NATS readiness edge: the module's listening-port check completed before the server accepted a stable client connection, producing a transient `EOF`. The remediation represented by this record starts MQ operation contexts only after container/connector initialization, gives MySQL an explicit two-minute startup wait, waits for NATS's `Server is ready` log, and retries the testkit connection for a bounded 15 seconds. The exact non-serialized commands `go test -count=1 ./...` and `go test -race -count=1 ./...` must pass with all containers executing.

`make godoc-artifacts` generated exactly 18 package artifacts. `make api-inventory-check` passed after regeneration from `internal/cmd/apiinventory`.

## Directed contract evidence

The full ordinary and race suites include regression coverage for the audit findings:

- Config: environment-only `Unmarshal`, nested fields containing underscores, and transactional failed reload.
- Connector/DB: timeout fields take effect, dependency causes remain discoverable, negative durations are rejected, and nil/closed connector clients return errors instead of panicking.
- Cache/testkit/xerrors: default local TTL, injectable serializers, typed Redis raw client, true discard test meter, isolated SQLite DSNs, and immutable error helper functions.
- Metrics/trace/registry: every concurrent shutdown caller observes its own context while an internal bounded cleanup continues; a metrics listen failure leaves the prior global provider untouched; listener address/error classification, resolver endpoint replacement, and Register/Shutdown races are covered.
- Idempotency/dlock: stale owners cannot commit, endpoint/request fingerprints detect conflicts, short TTL renewals work, ownership loss is observable, and failed unlock/release can be retried or reported.
- MQ: retained-history/default and latest start positions, Redis/JetStream persistent consumption, multi-topic durable/group identity isolation, retry/DLQ edge cases, Ack error propagation, reconnect, and Drain behavior.
- Cache/dlock/idgen/ratelimit/trace reject negative public duration configuration instead of silently applying defaults. Cache methods reject negative operation TTLs.
- Cache/idem/idgen/mq/ratelimit constructors reject unconnected borrowed connectors with errors classifiable as `connector.ErrClientNil`, without panicking.
- Breaker/ratelimit/idgen: meaningful breaker assertions, bounded key cardinality, fallback results, failure classification, non-serializing rate-limit Wait, terminal Close, typed drivers, composable defaults, and rejecting invalid Snowflake parsing.
- Idgen sequencer: zero `Step` defaults to one, while a negative `Step` reaches validation and returns `ErrInvalidInput` with `step_must_be_positive`.

## API and CI audit

- [v1-api-inventory.md](./v1-api-inventory.md) is generated and drift-checked in CI.
- [v1-api-decisions.md](./v1-api-decisions.md) records constructor, error, ownership, global-state, third-party type, no-op, durability, and lifecycle decisions.
- [v1-migration.md](./v1-migration.md) includes the source-breaking changes found during both audits.
- Every importable package has a compile-checked Go Example and generated `go doc -all` evidence.
- `.github/workflows/ci.yml` gates PRs, pushes to `main`, and SemVer tags with separate ordinary/race Testcontainers jobs, exact Go patch and runner versions, commit-pinned Actions, job timeouts, actionlint/module drift checks, build/examples/API/GoDoc checks, exact artifact-count validation, and a release gate depending on every job.
- A manual pre-tag workflow dispatch takes an exact candidate SHA and proposed SemVer tag, runs the same jobs against that SHA, and must succeed before the tag is created. The tag path revalidates the same identity after creation.
- Hosted run [31288087305](https://github.com/ceyewan/genesis/actions/runs/31288087305) passed all four required jobs against candidate `76cb23ca6d31fcd6bc4a1d1e59e38de095e2da3e`: ordinary Testcontainers, race Testcontainers, static/API/GoDoc, and build/examples. The release gate was correctly skipped because this was a pull-request run.
- PR #58's final run [31288491663](https://github.com/ceyewan/genesis/actions/runs/31288491663) passed all four required jobs against `8707620dab42a715a6eb6099f55ae27cecdaa2a1`; the protected merge produced `c80babaf35f71d656b85e28b8ae02a54e06ce7ce`.
- Final `main` push run [31288717036](https://github.com/ceyewan/genesis/actions/runs/31288717036) passed all four required jobs against exact merge SHA `c80baba`, including ordinary and race Testcontainers artifacts.
- Exact-SHA pre-tag run [31288875228](https://github.com/ceyewan/genesis/actions/runs/31288875228) passed all core jobs and the release identity gate for proposed tag `v1.0.0-rc.1` at `c80baba`.
- The first post-remediation `main` run [31290566522](https://github.com/ceyewan/genesis/actions/runs/31290566522) passed ordinary Testcontainers and all non-race gates but caught a transient NATS `EOF` in `TestJetStreamPublishSubscribeIntegration`; the Release gate did not run and publication stopped. Its uploaded race JSON is the evidence for the additional NATS ready-log wait and bounded connection retry in this record.
- `main` branch protection requires those four checks on an up-to-date branch, applies to administrators, requires pull requests and resolved conversations, and disallows force pushes and deletion.
- Controlled negative PR [#59](https://github.com/ceyewan/genesis/pull/59) introduced generated API-inventory drift. Hosted run [31288303160](https://github.com/ceyewan/genesis/actions/runs/31288303160) failed specifically at `Check exported API inventory`, and GitHub reported the PR merge state as `BLOCKED`. The PR was then closed and its remote branch deleted.
- The final remediation passed PR #61, protected merge, exact-main, pre-tag,
  and tag verification. The final `main` run was
  [31290988629](https://github.com/ceyewan/genesis/actions/runs/31290988629),
  the exact-SHA pre-tag run was
  [31291156982](https://github.com/ceyewan/genesis/actions/runs/31291156982),
  and the annotated-tag run was
  [31291334025](https://github.com/ceyewan/genesis/actions/runs/31291334025).

## Accepted v1 boundaries

- Gin, gRPC, GORM, OpenTelemetry, slog, JWT, and native connector client types are intentional v1 public contracts.
- Registry resolver registration and trace/metrics providers remain process-global; applications initialize one production instance and tests must serialize global replacement.
- Necessary no-op methods remain only for discard implementations, borrowed-resource `Close`, and synchronous slog `Flush` compatibility.
- Redis broadcast is retained-stream delivery without server-side acknowledgement/redelivery. Persistent at-least-once paths use Redis consumer groups or JetStream durable consumers.
- Dlock exposes ownership loss but does not issue fencing tokens; irreversible downstream writes still require CAS/version fencing.
- Trace supports system TLS and static OTLP headers, but custom certificate-pool construction remains application/deployment configuration outside Genesis v1.

No unresolved local P0/P1 API, correctness, migration, test-stability, or
repository-workflow defect identified by the release audit remained at
publication. Required-check enforcement and both positive and negative hosted
behavior were proven. Stage one and CI-G1 are complete; the tag and GitHub
prerelease are published. Post-release maintenance evidence is recorded in
[v1-rc1-contract-hardening.md](./v1-rc1-contract-hardening.md) and does not move
or replace the published tag.
