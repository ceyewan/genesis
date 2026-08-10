# v1.0.0-rc.2 release process

RC1 is immutable. RC2 must be a new annotated tag; never move or recreate
`v1.0.0-rc.1`.

## 1. Prepare the exact consumer pair

1. Merge the Genesis RC2 hardening changes to `main` and record the full
   commit SHA.
2. Merge the Resonance compatibility changes and complete Stage 3 on that
   protected `main` lineage. The six required rows are Compose, IM, Agent,
   recovery, telemetry/alerts, and benchmark.
3. Add the fixed Stage 3 JSON through an evidence-only Resonance PR. The final
   consumer SHA is the merge commit containing that JSON; the manifest's
   `tested_resonance_sha` remains the exact source commit exercised by Stage 3.
4. Resolve every pre-tag item in the release notes and replace the draft marker
   with the single exact line `Status: publication-ready.`.
5. Dispatch the Genesis `main` CI with `candidate_tag=v1.0.0-rc.2`, the current
   Genesis `main` SHA, the current Resonance `main` SHA, and
   `final_release_preflight=true`. The final preflight requires the tracked
   Stage 3 manifest, validates both repositories' exact tips, runs all normal,
   race, API/GoDoc, quality, and consumer gates, and validates the byte-exact
   release notes.
6. Preserve the resulting normal, race, API/GoDoc, consumer, and
   release-evidence artifact tuples. Each tuple consists of its Actions ID,
   name, actual attempt, SHA-256, and downloaded byte size. Do not reconstruct
   an artifact name from a later rerun attempt.

An early dispatch may set `final_release_preflight=false`; it is diagnostic and
does not authorize a tag. Only the successful final preflight is used below.

## 2. Configure the release authority

Genesis is a single-maintainer repository. The release identity is therefore
the authenticated `ceyewan` account used by the local GitHub CLI, rather than a
separate GitHub App. Before publication, verify through `gh api` that:

- `gh auth status` reports the `ceyewan` account and the account has repository
  administration permission;
- the `release` environment requires `ceyewan` approval, permits self-review,
  and accepts only the `main` branch and `v*` tags;
- an active tag ruleset targets `refs/tags/v*`, restricts creation, update, and
  deletion, and grants the `ceyewan` user the only configured bypass;
- repository immutable releases are enabled; and
- `v1.0.0-rc.2` does not already exist as a Git ref or GitHub Release.

No personal token is stored in Actions secrets. The local publisher receives
the current `gh auth token` only through the process environment and never
prints or persists it. `GITHUB_TOKEN` remains read-only in CI. Preserve the
environment, ruleset, immutable-setting, authenticated-user, and repository
permission JSON with the release-owner record.

## 3. Create the annotated tag and prerelease with `gh`

The release owner performs publication from a clean checkout of the successful
final-preflight Genesis SHA:

1. Revalidate the release notes with
   `go run ./internal/cmd/publishprerelease --validate-only`.
2. Read the successful workflow run and artifact metadata with `gh run view`
   and `gh api`. Export the exact five preflight tuples, Stage 3 tuple, Genesis
   and Resonance SHAs, `GITHUB_REPOSITORY=ceyewan/genesis`, and
   `RELEASE_TOKEN="$(gh auth token)"` only for the publisher process.
3. Run `publishprerelease --pre-tag-check <archive-dir>`. It rechecks immutable
   releases, requires no conflicting tag Release, validates Actions lineage and
   metadata, downloads each original ZIP by ID, and verifies its digest and
   size.
4. Recheck that Genesis and Resonance `main` still equal the bound SHAs. Render
   the annotation with `.github/scripts/render-rc2-tag-annotation.sh`, create an
   annotated `v1.0.0-rc.2` tag at the exact Genesis SHA, and push it with the
   authenticated local `git`/`gh` identity. Lightweight or moved tags are not
   accepted.
5. Wait for the tag-triggered CI. It parses and validates the complete
   annotation, reruns the full defense matrix, and uploads the independent
   `tag-defense-evidence-<sha>-<run-id>-<attempt>` artifact.
6. With the same local token, run `publishprerelease --stage-draft
   <archive-dir>` to create or resume the exact five-asset draft. Then export
   the tag-defense run/ID/name/attempt/digest and run `publishprerelease`
   without mode flags. The publisher downloads and hashes the defense ZIP,
   adds it as the sixth asset, publishes the prerelease, and waits until GitHub
   reports `immutable: true` for the exact tag, target SHA, body, flags, and six
   assets.
7. Run the pinned or newer local `gh release verify v1.0.0-rc.2 --format json`
   and preserve the output. Publication is complete only when the REST state
   and attestation verification both succeed.

The publisher never overwrites a mismatched Release or asset. A partial exact
draft may be resumed; an identity or digest mismatch is a hard stop. If GitHub
asset, immutable-state, or attestation propagation times out, inspect the exact
remote state and rerun only after it matches. Never delete or recreate the RC2
tag to recover from a Release-side delay.

## 4. Verify the published module

After the local `gh` publisher has completed exact-six immutable-state and
attestation verification, and the module is visible through the public Go
proxy, use a clean module cache and no local replacement:

```bash
GOMODCACHE="$(mktemp -d)" GOPROXY=https://proxy.golang.org \
  go mod download -json github.com/ceyewan/genesis@v1.0.0-rc.2
```

Record the public proxy response's `Version`, `Dir`, `Sum`, and `GoModSum`, and
separately record the annotated tag object ID and peeled commit. The proxy
response may omit `Origin`; it is not a required proxy identity field. An
optional fresh-cache `GOPROXY=direct go mod download -json` run may record
`Origin.Hash` as a VCS cross-check when Go returns it, and that hash must equal
the peeled commit. Then open a separate Resonance adoption PR that selects
`github.com/ceyewan/genesis v1.0.0-rc.2` directly, contains no Genesis
`replace`, runs with `GOWORK=off`, and records those same module identity
fields. This post-tag adoption SHA is distinct from the already merged pre-tag
consumer SHA.

In each clean adoption checkout, preserve
`go list -m -json github.com/ceyewan/genesis`; require exact RC2 `Version`,
non-empty `Sum`/`GoModSum`, no `Replace`, and a canonical `Dir` below that run's
clean `GOMODCACHE`. Require clean `git status`, then reject tracked paths from
`git ls-files` matching
`(^|/)(genesis|genesis-copy|vendor/github\.com/ceyewan/genesis)(/|$)`.
Together these checks exclude the defined vendor/local-copy paths and prove the
compiler used the downloaded RC2 module rather than sibling source.

Before running the production-like Stage 3 matrix, create one immutable input
identity manifest containing the exact merged Resonance source SHA and release-
set identity, Genesis `Version`/`Sum`/`GoModSum`, and fully qualified
`name@sha256:...` digests for the application, Pilot control, and Pilot runtime
images. Every Stage 3 command log and uploaded artifact must embed that manifest
or its SHA-256 digest; a row produced against a different input identity is not
part of the accepted matrix.

The adoption PR and its merged `main` commit must fill all nine
[stage-two hosted rows](./v1-rc2-evidence.md#published-module-and-resonance-adoption-evidence)
and the complete stage-three production-like matrix in the same evidence
section. Direct RC2 module identity, no replacement/workspace/source-copy
override, and the recorded `Dir`/`Sum`/`GoModSum` are prerequisites for those
results. A direct VCS `Origin.Hash` is supplemental when available, not a proxy
or adoption hard requirement.

## 5. Evidence that cannot be completed before publication

The candidate PR can contain local and hosted preflight evidence, but proxy
checksums and a no-replace consumer run exist only after the tag is public.
Append those results to the RC2 evidence record; do not fill them with expected
values in advance.

`govulncheck` is also a separate release decision because it sends module and
package metadata to an external vulnerability service. Run it only through the
approved security channel and record the pinned tool version and disposition of
every finding.

## 6. Proposed RC2 observation before stable v1

This policy remains subject to explicit release-owner acceptance. The proposed
stable-v1 observation window starts only after Resonance has merged a
no-replacement upgrade to the published RC2 and both repositories' required
main-branch gates are green. The proposed minimum is seven consecutive calendar
days and must include the production-like full-stack, failure-recovery,
telemetry, and representative IM/Agent paths defined by the Resonance release
acceptance plan.

The window restarts from a new RC when Genesis production code, public API,
dependencies, backend interaction semantics, or a required Resonance workaround
changes. A confirmed P0/P1 contract, correctness, security, ownership, or
delivery defect also resets it. Evidence-only corrections that do not change
the module artifact do not reset an otherwise valid window.

Stable `v1.0.0` may point at the observed RC2 commit only when:

- no unresolved P0/P1 defect or unexplained flaky required check remains;
- Genesis hosted release gates and the exact Resonance consumer gate remain
  green for the same commits;
- the no-replacement Resonance full-stack run and failure paths have passed;
- the vulnerability/dependency decision and every finding disposition are
  recorded; and
- the release owner accepts the final API inventory, paired replacements,
  exact approved removals,
  backend matrix, and remaining documented limitations.

Any production-code change after that decision requires another RC and a new
window; stable v1 is not rebuilt from merely similar source.
