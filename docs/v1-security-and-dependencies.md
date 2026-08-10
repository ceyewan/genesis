# Genesis v1 security and dependency policy

This policy defines the evidence required for an RC2 or stable-v1 publication.
It does not claim that a vulnerability scan has already run.

## Current RC2 decision

`govulncheck` source mode sends module and package metadata to a vulnerability
database. That external disclosure has not been authorized for this candidate,
so no networked scan result is recorded. RC2 publication remains blocked until
one of the following is captured in the RC2 evidence:

1. an approved, pinned `govulncheck` run with every finding dispositioned;
2. an approved offline or organization-managed scanner with equivalent
   reachability evidence; or
3. explicit release-owner acceptance of publishing without reachable-vulnerability
   scan evidence.

Absence of a scan is never reported as "no vulnerabilities found."

## Dependency integrity

- `go.mod` and `go.sum` are reviewed source files; release evidence includes
  `go mod verify` and a clean `go mod tidy` drift check.
- CI fixes Go and Node.js patch versions, `golangci-lint`, `markdownlint-cli2`,
  Buf, and GitHub Actions revisions.
- RC1 remains immutable. A dependency change requires a new candidate and may
  not be smuggled into a tag-only workflow.
- The public Go proxy `Version`, `Sum`, and `GoModSum`, plus the annotated tag's
  peeled commit, are recorded after a prerelease is published from a clean
  module cache. A fresh-cache direct-VCS `Origin.Hash`, when returned, is a
  supplemental cross-check rather than a required proxy field.

## Upgrade policy

- Dependency updates use dedicated PRs with release notes and upstream security
  or compatibility references.
- Patch and minor updates run the full ordinary, race, lint, API, examples and
  exact Resonance consumer gates. Public third-party types receive explicit API
  review even when Go compilation remains source-compatible.
- Major updates, or updates that change backend delivery, TTL, error, lifecycle
  or wire behavior, require a Genesis contract decision and an RC observation
  cycle.
- Known exploited or critical reachable findings take priority over scheduled
  upgrades. If no safe patch exists, the release owner documents mitigation,
  exposure and an expiry date for the exception.

## Release evidence

The RC2 evidence must name the scanner and exact version or the explicit risk
acceptance, the database snapshot/date when applicable, every finding and its
reachability disposition, and the Genesis/Resonance SHAs tested. Repository
rulesets, protected environment configuration, immutable releases, and the
dedicated Release App's least-privilege authority are separate supply-chain
controls and must also be verified before tag publication.

The App is limited to `Contents: write` and read-only Actions, Administration,
and Attestations permissions. Before the immutable transition, the protected
workflow must preserve the five pre-tag Actions archives and the tag-defense
evidence archive as an exact-six Release asset set. Publication is not green
until the REST API reports `immutable: true` for that exact set and the pinned
GitHub CLI verifies the Release attestation.

The tracked Stage 3 manifest contains content digests, not raw-bundle locators.
Its validator does not fetch those bundles. The release-environment approval
must therefore cite the separate Stage 3 handoff record, confirm each durable
locator is still readable, and confirm that the recorded digest describes the
reviewed content. This external approval record is a publication prerequisite;
it is not silently synthesized into the four machine-derived Stage 3 fields.
