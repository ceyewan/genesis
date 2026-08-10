# Genesis v1 compatibility matrix

This matrix records the configured candidate baseline for the Genesis v1
release gate. A combination becomes release evidence only after the final
evidence record contains its successful exact-SHA run and artifact; it is not a
claim that every earlier or later vendor version behaves identically.

## Go toolchain

| Role                 | Version  | Gate                                                  |
| -------------------- | -------- | ----------------------------------------------------- |
| Minimum supported Go | `1.26.0` | Build, unit tests, API compatibility                  |
| Pinned current Go    | `1.26.5` | Full tests, race, lint, examples and release evidence |

Consumers must compile with Go 1.26 or later. A higher local Go version is not
accepted as a substitute for the minimum-version gate.

## Quality toolchain

| Tool             | Version   | Gate                          |
| ---------------- | --------- | ----------------------------- |
| golangci-lint    | `v2.12.2` | Go lint and static analysis   |
| Buf              | `v1.66.1` | Protobuf lint                 |
| actionlint       | `v1.7.7`  | GitHub Actions syntax         |
| GitHub CLI       | `v2.96.0` | Immutable Release attestation |

Genesis local/hosted checks and Resonance local/hosted checks use the same
golangci-lint version; a different local binary is not equivalent evidence.
The tag-defense publisher downloads the exact GitHub CLI archive, checks its
pinned SHA-256 and reported version before the irreversible Release transition,
and uses that binary for the post-publication attestation check.

## Documentation toolchain

| Tool                | Version   | Gate                                  |
| ------------------- | --------- | ------------------------------------- |
| Node.js             | `22.23.2` | Hosted Markdown quality/API-docs jobs |
| markdownlint-cli2   | `0.18.1`  | Repository Markdown structure         |
| Local link checker  | Candidate | Relative paths and Markdown anchors   |

The hosted jobs pin the Node.js patch version and the immutable `setup-node`
action revision. The Markdown command installs only the exact CLI version in
the runner's npm cache; it does not create a repository `node_modules` tree or
lockfile.

## Package support tiers

Support tiers describe verification depth and response priority, not different
SemVer promises. Every package admitted to the generated root-v1 inventory is
covered by the same compatibility policy.

| Tier                    | Packages                                                                                                                    | Release evidence                                                                           |
| ----------------------- | --------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------ |
| Consumer-validated core | `auth`, `cache`, `clog`, `config`, `connector`, `db`, `idgen`, `metrics`, `mq`, `ratelimit`, `registry`, `trace`, `xerrors` | Package tests plus an exact Genesis/Resonance consumer gate                                |
| Extended                | `breaker`, `cache/serializer`, `dlock`, `idem`                                                                              | Package contract, race, and applicable real-backend tests; no direct Resonance import      |

Within mixed packages, Resonance directly validates Redis distributed cache KV,
JetStream MQ, and limiter-core paths. Local/multi/hash/ZSet cache features,
Redis Stream MQ, and framework adapters retain the package's v1 contract but
receive extended-path evidence rather than direct consumer evidence.

`testkit` and all four generated example-proto packages are now under
`internal` paths. They remain fully buildable repository infrastructure, but
are intentionally outside the public module contract and package tiers.
External consumers must own their Testcontainers fixtures and protobuf schemas.

## External backends

| Backend        | Release-test image                   | Exercised packages                                                |
| -------------- | ------------------------------------ | ----------------------------------------------------------------- |
| Redis          | `redis:7.2-alpine`                   | `connector`, `cache`, `dlock`, `idem`, `idgen`, `mq`, `ratelimit` |
| PostgreSQL     | `postgres:17-alpine`                 | `connector`, `db`                                                 |
| MySQL          | `mysql:8.0`                          | `connector`, `db`                                                 |
| etcd           | `quay.io/coreos/etcd:v3.5.12`        | `connector`, `dlock`, `idgen`, `registry`                         |
| NATS JetStream | `nats:2.10-alpine`                   | `connector`, `mq`                                                 |
| Kafka          | `confluentinc/confluent-local:7.5.0` | `connector`                                                       |
| SQLite         | in-process driver from `go.mod`      | `connector`, `db`                                                 |

The ordinary and race release jobs run the real Testcontainers cases. Docker
unavailability is a failure, not a skip. The table records human-readable image
tags; each ordinary/race Testcontainers artifact also captures the resolved
image ID and repository digest so the mutable tag is not mistaken for immutable
evidence.
Backend-specific behavior remains visible at the package boundary; for example,
Etcd lock TTLs are whole seconds, JetStream supports progress acknowledgement,
and Redis Stream does not.

## Client-library baseline

The exact client dependency graph is fixed by `go.mod` and `go.sum`. The RC2
candidate currently uses:

| Client                                        | Version   |
| --------------------------------------------- | --------- |
| `github.com/redis/go-redis/v9`                | `v9.16.0` |
| `go.etcd.io/etcd/client/v3`                   | `v3.6.6`  |
| `github.com/nats-io/nats.go`                  | `v1.47.0` |
| `github.com/twmb/franz-go`                    | `v1.20.6` |
| `gorm.io/gorm`                                | `v1.31.1` |
| GORM MySQL/PostgreSQL/SQLite drivers          | `v1.6.0`  |
| `github.com/testcontainers/testcontainers-go` | `v0.40.0` |

An incompatible upstream major version is not adopted implicitly. It requires
a reviewed dependency PR, the full Genesis gate, API-inventory review for any
public third-party type, and the exact Resonance consumer gate.

## Support interpretation

- The table plus the hosted run's resolved image digests is the reproducible
  release baseline. Other compatible vendor patch versions may work, but are
  not release evidence until added to CI.
- Final RC2 evidence is durable only after the immutable GitHub prerelease
  contains the five pre-tag archives plus the tag-defense evidence archive and
  its exact-six asset set and release attestation have both been verified.
- A package remaining in the root v1 module carries normal SemVer obligations
  regardless of whether Resonance currently imports it.
- Applications own topology, credentials, TLS, persistence, backup and vendor
  upgrade policy. Genesis owns the documented Go API and backend interaction
  semantics it exposes.
- A backend upgrade that changes observable delivery, TTL, transaction or
  ownership behavior must be treated as a contract change, not a routine image
  bump.
