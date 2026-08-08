# Genesis v1.0.0-rc.1 verification evidence

This record describes the local-only candidate verification performed on 2026-08-08. The exact candidate commit SHA is recorded in the stage goal after the authorized local commits are created. No remote push, tag, release, or deployment is part of this verification.

## Environment

- Go: `go1.26.1 darwin/arm64`
- Docker client/server: `29.4.0` / `29.4.0`
- Baseline before the candidate changes: `1957faddd95f590d831609db39a2b45e2bb21e22`

## Full gates

The following commands completed successfully against the candidate worktree:

```text
go test ./... -count=1
go test -race ./... -count=1
make lint
make modernize-check
make buf-lint
make example-all
git diff --check
```

The ordinary and race suites executed real Docker/Testcontainers dependencies rather than skipping them. They covered Redis, etcd, PostgreSQL, MySQL, NATS JetStream, and Kafka-backed packages. The local example stack supplied Redis, PostgreSQL, NATS, and etcd. The connector example intentionally demonstrated classifiable connection failures for services not in that stack (MySQL and Kafka); their successful connection paths were exercised by the Testcontainers suites. The example command itself completed with status 0. The local example containers were removed after the gate.

## Directed contract evidence

The full race suite includes these focused cases:

- MQ: `TestJetStreamPublishSubscribeIntegration`, `TestJetStreamManualNakWithDelayRedeliveryIntegration`, `TestJetStreamDurableResumeIntegration`, `TestJetStreamMaxInflightBackpressureIntegration`, `TestJetStreamDrainWaitsForHandlerIntegration`, `TestJetStreamReconnectAndResumeIntegration`, and `TestMQ_DrainConcurrent`.
- Registry: `TestRegister`, `TestWatch`, `TestKeepAlive`, `TestLeaseFailureIsExposed`, `TestWatchReconnectAndRecoverIntegration`, `TestClose`, `TestShutdownDeadlineStillClosesLeaseFailuresAfterWorkersExit`, and `TestResolverConcurrentCloseWaitsForWorker`.
- Repeated lifecycle operations: concurrent close/stop coverage in local cache, config loader, Redis and etcd dlock, standalone ratelimit, idem memory store, ID allocator, metrics, trace, MQ, and registry.
- Export failures: `TestUnavailableExporterDoesNotBlockSpanAndReportsFailure`, `TestReportingExporterExposesFailureWithoutBlocking`, and concurrent normal shutdown tests for trace and metrics.
- Connector contracts: `TestConstructorsRejectNilConfig`, `TestConstructorsCopyConfigBeforeApplyingDefaults`, `TestConfigValidationIdentifiesInvalidField`, connection-failure classification, and real connector integration tests.
- ID generation boundaries: deterministic clock rollback tests, `TestSequencer_DoesNotWrap_Integration`, Redis/etcd allocator lease and keepalive subtests, and concurrent `Stop` coverage.

## API and workspace audit

- [v1-api-inventory.md](./v1-api-inventory.md) matches every non-example, externally importable package reported by `go list`, including `cache/serializer` and `testkit`.
- [v1-api-decisions.md](./v1-api-decisions.md) records constructor, config-copy, time, error, lifecycle, ownership, observability, and stability decisions.
- [v1-migration.md](./v1-migration.md) records caller changes from v0.5.0.
- Source and documentation contain no developer-machine absolute path dependency, and the worktree contains no unexpected generated build artifacts.

## Accepted boundaries

- Auth remains a local HS256 access/refresh JWT component; OAuth2/OIDC, revocation, and an identity directory are outside Genesis v1.
- Redis Stream has no native delayed Nak. `Nak` and `NakWithDelay` return `ErrNotSupported`; pending-message reclaim supplies redelivery.
- MQ business DLQ policy remains application-owned.
- No known unresolved code or migration risk remains. Creating the local candidate commits and recording their exact final SHA requires explicit user authorization.
