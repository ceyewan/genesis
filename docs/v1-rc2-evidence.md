# Genesis v1.0.0-rc.2 candidate evidence

Status: draft. This record describes the unpublished RC2 candidate. A field
marked `Pending` is a release blocker or evidence that can exist only after
publication; it is not an expected value.

RC1 remains immutable at
`ec5ad2c31fb4adce2bd42529e3d7fbfe92b23aa7`. Nothing in this candidate moves or
rewrites `v1.0.0-rc.1`.

## Candidate identity

| Field                                  | Value                                      |
| -------------------------------------- | ------------------------------------------ |
| Genesis base                           | `d0816a1be5dd403612db15c8d98e2c860c10a5ff` |
| Genesis working branch                 | `agent/v1-surface-rc2`                     |
| Final Genesis commit                   | Pending protected PR and merge             |
| Resonance base                         | `69f02a11319e2adb58b20d7671647f523c18b8b2` |
| Resonance working branch               | `fix/genesis-rc2-consumer`                 |
| Resonance compatibility merge provenance | Pending protected PR and `main` merge    |
| Pre-tag Resonance consumer commit      | Pending later Stage 3 final `main` merge   |
| Pre-publication Stage 3 handoff        | Pending Compose/E2E/recovery/telemetry/benchmark record |
| Post-tag Resonance RC2 adoption commit | Pending direct-module PR and `main` merge  |
| Proposed tag                           | `v1.0.0-rc.2`                              |
| Published tag object and peeled commit | Pending publication                        |

The local consumer worktree uses an unpublished workspace replacement solely
to test the candidate. Resonance's committed `go.mod` continues to select
`v1.0.0-rc.1`; it must move to the public RC2 module with no `replace`,
`go.work`, or copied source after the tag is available.
The compatibility PR merge is provenance, not the frozen consumer identity
while Stage 3 remains in progress. The pre-tag consumer SHA must be a later
protected Resonance `main` commit containing both that merge and the completed
Stage 3 Compose, IM/Agent E2E, recovery, telemetry, and benchmark handoff. The
later direct-RC2 adoption SHA is a different, post-publication identity and must
not be substituted into the pre-tag annotation retroactively.

## Reviewed contract changes

The candidate contains the source-compatible correctness and lifecycle fixes
described in [the API decisions](./v1-api-decisions.md) and
[the migration guide](./v1-migration.md). Important release-sensitive changes
include:

- atomic idempotency result/lock transitions, a post-lock result recheck, and
  authenticated HTTP/gRPC identity scoping;
- bounded runtime cardinality for breaker, rate limiting, idempotency, and
  distributed-lock ownership-loss diagnostics;
- safe gRPC breaker fallback result propagation;
- config snapshot and file/environment leaf-merge correctness;
- connector, registry, metrics, cache, MQ, dlock, idgen, trace, DB, and xerrors
  race, panic, ownership, deadline, and error-classification fixes;
- optional JetStream progress acknowledgement and instance-local pull batch
  sizing, with matching Resonance heartbeat and shutdown behavior;
- an immutable RC1 API baseline, canonical current inventory, paired
  old-to-expected-new compatibility exceptions, and exact approved removals.

RC1-exported `cache.ErrNotSupported`, `mq.ErrSubscriptionClosed`, and
`ratelimit.ErrRateLimitExceeded` remain as deprecated source-compatibility
names. The candidate does not invent production return paths for them.

## Local verification status

During candidate development, the following gates passed at least once with
real Redis, etcd, PostgreSQL, MySQL, NATS JetStream, and Kafka Testcontainers
where applicable:

```text
go test -count=1 ./...
go test -race -count=1 ./...
go build ./...
make lint
make markdown-check
make exported-comments
make modernize-check
make buf-lint
make examples-check
make example-all
make api-inventory-check
make api-baseline-check
make api-compat-check
make godoc-artifacts
go mod verify
git diff --check
```

Those runs are implementation evidence, not final release evidence: subsequent
hardening changed the working tree. Every command must pass again against the
frozen final commit, and the hosted workflow must preserve the exact-source
artifacts described below.

The Resonance candidate has also passed targeted and full ordinary/race tests
against the local Genesis candidate, plus a `GOWORK=off` compile check against
the still-selected RC1 module. The final hosted consumer gate and the
post-publication no-replacement run remain Pending.

Genesis local/hosted lint and Resonance local/hosted lint all pin
`golangci-lint` `v2.12.2`; a different local binary is not equivalent release
evidence.

## API and documentation evidence

- `docs/api-baselines/v1.0.0-rc.1.md` is regenerated from the immutable RC1
  commit and checked for drift.
- `docs/v1-api-inventory.md` is generated from the candidate. Its final count
  is 17 packages and 705 declarations after the approved surface removals.
- `docs/v1-api-compat-allowlist.md` names each approved old declaration, while
  `docs/v1-api-compat-expected.md` independently fixes its exact replacement.
- `docs/v1-api-compat-removals.md` records all 34 approved RC1 declarations
  removed without replacement. The checker rejects stale entries, signature
  changes disguised as removals, and overlap with paired replacements.
- The compatibility checker records constant values, variable types, full
  import paths, exported struct shape, and complete promoted method sets.
- GoDoc, executable examples, Markdown style, and local relative-link/anchor
  checks are release gates. External HTTP links are not fetched by the local
  link checker.

The release owner approved the final package boundary: `dlock` remains an
extended root-v1 package; `testkit` and four example-proto packages are
internalized; and the two zero-logic trace HTTP wrappers are removed. Local
implementation and compatibility evidence exist, while protected PR/main and
hosted exact-SHA evidence remain Pending.

## Pre-publication exact-source evidence

The release workflow binds one Genesis candidate SHA and the final pre-tag
Resonance `main` tip. A dispatch consumer gate requires exact equality with the
observed Resonance `origin/main`; `publish-tag` repeats the remote-tip equality
check after environment approval. The tag-triggered defense run permits later
normal Resonance `main` advancement but still requires the bound SHA to remain
an ancestor. The consumer artifact records the observed tip and check time. It
also records `resonance-module-input.txt`, applies the unpublished Genesis
replacement only for the test commands, preserves that exact diff, drops the
replacement, and requires `go.mod`/`go.sum` to return byte-for-byte clean.

The publication path now machine-binds the pre-publication Stage 3 handoff. It
reads the fixed tracked manifest from the exact Resonance checkout, validates
its schema and lineage, computes the digest from committed blob bytes, and
binds the four derived fields into the release evidence and annotation. A
hand-entered digest is not accepted. The fixed manifest is intentionally not in
the current candidate yet, so its protected successful run remains Pending and
publication must stay disabled. The validator does not fetch the raw Stage 3
bundles, and its four outputs carry no locator. Before release-environment
approval, the release owner must cite the separate Stage 3 handoff record,
confirm that every durable raw-bundle locator remains readable, and verify that
the reviewed content matches its recorded digest. Do not claim that those
locators are embedded in the consumer artifact, release-evidence ZIP, tag
annotation, or four derived fields. Populate this table only from that run:

| Evidence                                     | Run / attempt | Artifact ID / name / SHA-256      | Status  |
| -------------------------------------------- | ------------- | --------------------------------- | ------- |
| Minimum Go build/unit/API required job       | Pending       | None by design; required job only | Pending |
| Ordinary Testcontainers                      | Pending       | Pending normal artifact           | Pending |
| Full race Testcontainers                     | Pending       | Pending race artifact             | Pending |
| API, docs, examples, and Markdown            | Pending       | Pending API/GoDoc artifact        | Pending |
| Exact merged-main Resonance pre-tag consumer | Pending       | Pending consumer artifact         | Pending |
| Final-SHA Stage 3 handoff                     | Pending       | Pending durable record + SHA-256  | Pending |
| Raw Stage 3 evidence bundles                  | Pending owner review | Pending durable locators + digests | Pending |
| Release evidence record                      | Pending       | Pending release-evidence artifact | Pending |

The workflow's annotation schema contains the candidate/workflow identities,
Resonance main tip and check time, the four derived Stage 3 fields, preflight
run, and the real ID/name/digest/attempt/size tuple for the release-evidence
artifact and every upstream artifact. Names and attempts come from each upload
job's outputs; they are never reconstructed from the current rerun attempt. The
tag-triggered workflow derives the same Stage 3 fields again and reruns the gate
as defense in depth.

The quality job is a required workflow result rather than a fifth uploaded
artifact. Record every source layer independently; one green dispatch does not
stand in for the protected PR, merged `main`, or tag-triggered rerun:

| Source layer                       | Tested checkout SHA       | Source/head identity                 | Required lineage proof                         | Run / attempt | Required jobs |
| ---------------------------------- | ------------------------- | ------------------------------------ | ---------------------------------------------- | ------------- | ------------- |
| Protected PR                       | Pending synthetic merge   | Pending PR head SHA and source tree  | Head/base parents of tested synthetic merge    | Pending       | Pending       |
| Protected merged `main`            | Pending final Genesis SHA | Pending protected merge commit/tree  | PR head ancestry and protected merge provenance | Pending       | Pending       |
| Exact-SHA pre-tag dispatch         | Pending final Genesis SHA | Same protected `main` commit/tree    | Candidate SHA equals observed `origin/main`    | Pending       | Pending       |
| Annotated-tag defense-in-depth run | Pending final Genesis SHA | Annotated tag object and peeled SHA  | Tag peels to final SHA and binds preflight     | Pending       | Pending       |

The last three rows must name the same final Genesis commit and source tree.
The PR row instead records both the checkout's synthetic merge SHA and the PR
head/tree it evaluates; it must not claim that GitHub's synthetic test commit
is the later protected `main` merge SHA.

## Release authority and security

As of the 2026-08-09 candidate record, automatic publication remains disabled
and the remote controls below are unverified. This dated statement must be
replaced by current GitHub API evidence. Before setting
`RELEASE_TAG_PUBLISH_ENABLED=true`, preserve API JSON proving all of the
following:

- a protected `release` environment with required reviewers, prevention of
  self-review, and deployment restricted to protected `main` plus `v*` tag
  runs; the workflow further limits prerelease publication to the exact RC2
  tag;
- an active `refs/tags/v*` ruleset restricting creation, update, and deletion;
- a dedicated Release GitHub App as the ruleset's sole bypass actor, with only
  `Contents: write`, `Actions: read`, `Administration: read`, and
  `Attestations: read`, and with its private key available only through that
  environment;
- repository immutable releases enabled in advance by an administrator, with
  API evidence preserved; and
- the protected pre-tag run successfully downloading and hashing the four
  upstream artifact ZIPs and release-evidence ZIP, then staging exactly those
  five archives on the prerelease draft; and
- the tag-triggered defense run preserving its own release-evidence ZIP as a
  sixth asset whose durable filename binds its run, actual attempt, Actions
  artifact ID, and SHA-256 before the exact-six draft is published immutable.

| Control                     | Evidence                                      | Status   |
| --------------------------- | --------------------------------------------- | -------- |
| Release environment         | Pending API JSON                              | Pending  |
| Tag ruleset                 | Pending API JSON                              | Pending  |
| Dedicated Release App       | Pending installation/permission evidence      | Pending  |
| Immutable releases          | Pending administrator/API evidence            | Pending  |
| Durable evidence archive    | Pending immutable Release URL and six asset digests | Pending |
| Publication enable variable | Must remain absent/false until the above pass | Disabled |

No networked `govulncheck` result is claimed. The release requires one of the
auditable choices in
[the security and dependency policy](./v1-security-and-dependencies.md): an
approved pinned scan, an approved equivalent channel, or explicit release-owner
risk acceptance.

## Tag publication evidence

These values do not exist until the protected workflow publishes the tag. They
prove immutable publication, not downstream module adoption:

| Field                        | Value   |
| ---------------------------- | ------- |
| Annotated tag object         | Pending |
| Peeled Genesis commit        | Pending |
| Tag annotation evidence hash | Pending |
| GitHub tag workflow          | Pending |
| GitHub prerelease            | Pending |
| Immutable release state      | Pending `immutable: true` and attestation |
| Durable evidence assets      | Pending exact six names and SHA-256 digests |
| Attestation verifier         | Pending pinned `gh v2.96.0` output |

## Published-module and Resonance adoption evidence

The proxy and no-replacement consumer evidence can exist only after tag
publication. It closes RC2 adoption, not the earlier tag-creation act:

| Field                                                | Value   |
| ---------------------------------------------------- | ------- |
| `go list -m -json` `Version`                         | Pending |
| `go list -m -json` `Dir` under clean `GOMODCACHE`    | Pending |
| `go list -m -json` `Sum`                             | Pending |
| `go list -m -json` `GoModSum`                        | Pending |
| Optional direct-VCS `Origin.Hash` cross-check        | Pending if returned |
| Module `Replace` field                               | Pending absent |
| Effective `GOWORK`                                   | Pending `off` |
| Tracked vendor/local Genesis source-copy path check  | Pending clean |
| Resonance RC2 upgrade PR and merge SHA               | Pending |
| Resonance no-replacement module identity             | Pending |

The upgraded Resonance commit must repeat the stage-two and stage-three
acceptance evidence rather than relying on the temporary local workspace:

The adoption PR must select `github.com/ceyewan/genesis v1.0.0-rc.2` directly,
contain no Genesis `replace`, and run with `GOWORK=off`. In a clean checkout it
must preserve `go list -m -json github.com/ceyewan/genesis`, require its `Dir`
to resolve below that run's clean `GOMODCACHE`, and record `Version`, `Dir`,
`Sum`, and `GoModSum`. Public proxy JSON is not required to contain `Origin`.
A separate fresh-cache `GOPROXY=direct` query may record `Origin.Hash` when Go
returns it, but that is supplemental and must match the annotated tag's peeled
commit.

The same gate must require a clean `git status` and reject any tracked path from
`git ls-files` matching
`(^|/)(genesis|genesis-copy|vendor/github\.com/ceyewan/genesis)(/|$)`.
This is the machine-defined no-vendor/no-local-source-copy boundary; the module
cache `Dir` check separately proves which source the compiler consumed. The
protected PR and merged-`main` runs must then fill all nine stage-two rows and
the complete stage-three matrix below.

| Resonance stage-two hosted check             | PR run  | Merged-main run | Status  |
| -------------------------------------------- | ------- | --------------- | ------- |
| Generated Code Check                         | Pending | Pending         | Pending |
| Go Lint                                      | Pending | Pending         | Pending |
| Go Test: race, Genesis identity, IM/Agent trace continuity | Pending | Pending         | Pending |
| Go Security                                  | Pending | Pending         | Pending |
| Proto Lint                                   | Pending | Pending         | Pending |
| Docs and Format                              | Pending | Pending         | Pending |
| Web                                          | Pending | Pending         | Pending |
| Pilot Bridge and locked Pi contract          | Pending | Pending         | Pending |
| Pilot Image build and isolation              | Pending | Pending         | Pending |

Every Stage 3 row below must bind the same immutable input manifest; no result
may be copied between manifests or inferred from a mutable tag:

| Stage 3 immutable input                     | Exact identity / evidence |
| ------------------------------------------- | ------------------------- |
| Resonance release set and merged source SHA | Pending exact ID and SHA  |
| Genesis module identity                     | Pending `v1.0.0-rc.2`, `Sum`, and `GoModSum` |
| Application image                          | Pending exact `name@sha256:...` |
| Pilot control image                        | Pending exact `name@sha256:...` |
| Pilot runtime image                        | Pending exact `name@sha256:...` |
| Input manifest                             | Pending artifact location and SHA-256 |

| Resonance stage-three production-like evidence            | Command / artifact | Input manifest SHA-256 | Status  |
| --------------------------------------------------------- | ------------------ | --------------------- | ------- |
| Full Compose health and service discovery                 | Pending            | Pending               | Pending |
| IM send/edit/recall/read/offline E2E                      | Pending            | Pending               | Pending |
| Agent/Pilot run, stream, tool/approval, commit/status E2E | Pending            | Pending               | Pending |
| Restart, dependency failure, and recovery paths           | Pending            | Pending               | Pending |
| Metrics, logs, traces, correlation, and alerting          | Pending            | Pending               | Pending |
| Representative concurrency benchmark and capacity record  | Pending            | Pending               | Pending |

## Stable-v1 observation evidence

| Field                                         | Value                                                       |
| --------------------------------------------- | ----------------------------------------------------------- |
| Observation-policy approval                   | Pending release-owner acceptance of the proposed seven days |
| Observation start and end                     | Pending after no-replacement merge                          |
| Proposed seven consecutive days without reset | Pending                                                     |
| Workloads and incidents                       | Pending                                                     |
| Release-owner exit decision                   | Pending                                                     |

RC2 publication, module adoption, and stable promotion are separate milestones.
Stable-v1 promotion follows the reset and exit criteria in
[the release process](./v1-rc2-release-process.md#6-proposed-rc2-observation-before-stable-v1).
