# v1.0.0-rc.1 contract hardening

This record maps the published `v1.0.0-rc.1` contract to its active
Resonance consumer and to the tests that protect the observable behavior. It
does not change the API inventory or define new behavior.

## Immutable baseline

- Module: `github.com/ceyewan/genesis@v1.0.0-rc.1`
- Tag commit: `ec5ad2c31fb4adce2bd42529e3d7fbfe92b23aa7`
- Module sum: `h1:X3VK5VpPxIrgyzQsPPPSHQHaiNvMhhT/wcGCWkuFS8U=`
- `go.mod` sum: `h1:VUPsG33Toz8lKJk2tEkgeWd7SFMIDjYtwvzYOuQmRU4=`
- Hardening branch baseline: the same tag commit; `main` had no commits ahead
  of the release when the branch was created.

The tag, release artifact, module files, dependency versions, and
[API inventory](./v1-api-inventory.md) remain unchanged.

## Consumer-driven risk inventory

A read-only scan of Resonance Go sources found the following Genesis imports.
Counts are import occurrences, including tests, and are a prioritization aid
rather than a coverage target.

| Package     | Imports | Consumer-sensitive surface                                              | Result                                                                     |
| ----------- | ------: | ----------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| `clog`      |      81 | construction, trace-context fields, derived loggers, root close         | Existing coverage; documentation clarified                                 |
| `mq`        |      26 | JetStream publish/subscribe, manual ack, headers, queue groups, drain   | Existing container/race coverage                                           |
| `auth`      |      17 | constructor errors, JWT access-token validation, logger injection       | Existing coverage; external nil-config test added                          |
| `db`        |      16 | PostgreSQL injection, transaction, borrower `Close`                     | External ownership test added                                              |
| `connector` |      15 | Redis/PostgreSQL/NATS/etcd construction, `Connect`, owner `Close`       | Existing container coverage; external classification/ownership tests added |
| `idgen`     |      14 | allocator, sequencer, generator, explicit duration units                | Existing Redis/etcd and exhaustion coverage                                |
| `registry`  |      12 | etcd injection, register/watch, caller-owned gRPC connections, shutdown | Existing container/race coverage; documentation clarified                  |
| `config`    |       8 | environment-only load, nested underscore keys, repeated close           | External environment/duration test added                                   |
| `metrics`   |       7 | provider construction, buckets, process-global shutdown                 | Existing lifecycle coverage; external nil-config test added                |
| `trace`     |       4 | OTLP/local provider, propagation, global shutdown                       | External propagation test added                                            |
| `ratelimit` |       4 | standalone/distributed construction and terminal close                  | External concurrent-close test added                                       |
| `cache`     |       1 | Redis distributed cache and borrower close                              | Existing Redis coverage; defect documented below                           |
| `xerrors`   |       1 | wrapped error classification                                            | Existing coverage; external `errors.Is` tests added                        |

Low-risk packages not used by Resonance (`breaker`, `dlock`, `idem`, and
`testkit` as a direct import) retain their existing contract and container
coverage. No low-value tests were added solely to increase a coverage number.

## Contract sources and added evidence

The external tests in `connector/rc1_consumer_contract_test.go` derive from
[v1-api-decisions.md](./v1-api-decisions.md):

- nil configurations are rejected at consumer-facing constructors;
- nested environment keys and `time.Duration` units survive config loading;
- a DB borrower does not close its caller-owned connector;
- standalone rate-limit close is concurrent, idempotent, and terminal;
- exported error sentinels remain discoverable through `errors.Is`;
- trace carrier injection and extraction preserve trace association.

The tests use only exported APIs and run in a separate package. Removing any
of these observable behaviors makes the corresponding test fail.

## Testcontainers and CI audit

The ordinary and race jobs both run `go test -count=1 ./...` without skip
flags. `testkit.RequireDocker` fails clearly when Docker is unavailable rather
than reporting a false pass. Redis, PostgreSQL, MySQL, NATS, etcd, and Kafka
helpers use dynamically mapped ports and `t.Cleanup` termination. PostgreSQL
uses its module readiness strategies; MySQL has a two-minute ready-log bound;
NATS waits for its ready log and retries client connection for at most 15
seconds. Test keys, topics, streams, databases, and SQLite DSNs are isolated by
the existing helpers and tests.

No CI change was needed: the workflow already has separate ordinary and race
container jobs, bounded job timeouts, and unchanged release-gate semantics.

## Defect classification

### RC1-BACKLOG-001: cache nil-config errors were not classifiable

Status: resolved on `main` for the planned `v1.0.0-rc.2`; the immutable
`v1.0.0-rc.1` artifact is unchanged.

- Package: `cache`
- RC1 behavior: `NewLocal(nil)` and `NewDistributed(nil)` return non-nil
  errors, but the package exposes no configuration sentinel that matches those
  errors through `errors.Is`.
- Contract basis: the constructor table in `v1-api-decisions.md` says rejected
  nil configurations produce a classifiable configuration error.
- Resonance impact: not blocking. Resonance supplies a non-nil
  `DistributedConfig` and does not depend on nil-config classification.
- Resolution: `cache.ErrInvalidConfig` classifies nil configurations from both
  constructors through `errors.Is`. The consumer-contract test now protects
  this behavior for the next release candidate.

No stage-two-blocking Genesis production defect was found.

## RC maintenance policy

Tests, examples, and documentation merged after `v1.0.0-rc.1` do not change
the immutable module zip and do not create a new version. A production fix,
public API change, dependency update, or observable semantic change requires a
separate RC decision and the complete release gate; consumers must then opt in
to that explicitly published version. Never move or overwrite the `rc.1` tag.
