# v1.0.0-rc.2 release process

RC1 is immutable. RC2 must be a new annotated tag; never move or recreate
`v1.0.0-rc.1`.

## 1. Prepare the exact consumer pair

1. Merge the Genesis RC2 hardening PR to `main` and record the 40-character
   commit SHA.
2. Merge the pre-tag Resonance compatibility work through its protected PR into
   Resonance `main`, and retain that merge SHA as provenance. It is not yet the
   frozen consumer identity while Stage 3 remains in progress.
3. Complete Stage 3 on a descendant that contains that compatibility merge,
   merge the result through protected Resonance `main`, and record this later
   40-character SHA as the final pre-tag consumer identity. Preserve the
   Compose, IM/Agent E2E, failure-recovery, telemetry, and benchmark handoff for
   exactly that SHA. The consumer gate tests this final commit against the
   unpublished Genesis candidate with a temporary module replacement. A
   publication dispatch requires that SHA to equal the observed `origin/main`
   tip and records the observation.
   This pre-publication candidate-freeze prerequisite is distinct from the
   post-tag direct-RC2 Stage 3 repetition in section 4.
4. Complete the repository controls in the next section. Until they are
   verified, keep the repository variable `RELEASE_TAG_PUBLISH_ENABLED` absent
   or set to `false`; both tag and prerelease publish jobs are then
   machine-disabled.
5. From the Genesis `main` workflow, run `workflow_dispatch` with:
   `candidate_tag=v1.0.0-rc.2`, the current Genesis `main` SHA, and the exact
   final Resonance SHA. Dispatches from another ref or a stale
   Genesis/Resonance `main` SHA fail closed. A `publish_tag=false` dispatch is an
   early candidate preflight and may run before the Stage 3 manifest exists. A
   `publish_tag=true` dispatch and every tag-triggered defense run instead call
   `internal/cmd/stage3evidence` against the exact Resonance checkout. That
   validator requires the fixed tracked manifest
   `docs/verification/evidence/genesis-v1.0.0-rc.2-stage3.json`, validates its
   schema and lineage, derives its digest from the committed blob bytes, and
   exports the four Stage 3 fields used by the release evidence and annotation.
   There is no hand-entered digest input. The manifest itself and its successful
   hosted result remain publication prerequisites. Those four fields do not
   carry raw-bundle locators, and the validator does not fetch the bundles.
   Before release-environment approval, the release owner must cite the separate
   Stage 3 handoff record, confirm each durable locator remains readable, and
   verify the reviewed content against its recorded digest.
6. Preserve the normal, race, GoDoc/API, and Resonance consumer artifacts. Each
   artifact's source manifest records the candidate SHA, workflow definition
   identity, run ID, and attempt. The release-gate record binds the four upload
   artifact IDs, names, attempts, and SHA-256 digests to the exact Genesis and
   Resonance commits. The release-evidence upload has its own independent tuple.
   Before the tag is pushed, the protected job uses those exact IDs to query
   Actions metadata, checks run/head/name/digest/size, and downloads and hashes
   all five original ZIP archives. Immediately after the annotated tag is
   pushed, the same job creates or resumes the exact GitHub prerelease draft and
   uploads those ZIPs as Release assets. Actions uploads retain their source
   copies for 90 days, while immutable Release assets are the durable archive.
   The consumer artifact also records the original direct Genesis version and
   the temporary replacement diff; its job must remove that replacement and
   prove `go.mod`/`go.sum` clean before succeeding.

Each upload job exports its actual artifact name and run attempt with its ID and
digest. The release gate and annotated tag consume those outputs directly and
record an independent tuple per artifact, plus the release-evidence artifact's
own tuple. A partial rerun must not reconstruct prior names from the current
`GITHUB_RUN_ATTEMPT`.

## 2. Configure the release authority

The publish job deliberately does not give `GITHUB_TOKEN` write permission. It
mints a short-lived token for a dedicated Genesis Release GitHub App from the
protected `release` environment. Before enabling it:

1. Create and install a dedicated Release App on this repository with only
   `Contents: write`, `Actions: read`, `Administration: read`, and
   `Attestations: read`; record its client ID as the environment variable
   `RELEASE_APP_CLIENT_ID` and its private key as the environment secret
   `RELEASE_APP_PRIVATE_KEY`. The read permissions allow exact Actions archive
   retrieval, immutable-setting verification, and release-attestation
   verification. Do not grant Actions, Administration, or Attestations write.
2. Create an active tag ruleset targeting `refs/tags/v*` that restricts tag
   creation, update, and deletion. Make that dedicated App the only bypass
   actor; do not grant a generic GitHub Actions or repository-role bypass.
3. Create the `release` environment with required reviewers, prevent
   self-review, and restrict deployments to protected `main` and `v*` tag runs.
   The workflow itself permits prerelease publication only for the exact
   `refs/tags/v1.0.0-rc.2` ref.
4. Have an administrator enable GitHub immutable releases for the repository
   and preserve API evidence of the setting. The App can read and enforce this
   preconfigured setting but cannot change it.
5. Verify those controls through the GitHub API and preserve the returned
   ruleset/environment JSON with the release evidence. Only then set the
   repository variable `RELEASE_TAG_PUBLISH_ENABLED=true`.

Rulesets grant bypass to an actor or App, not to an environment. The security
boundary is therefore the combination of a dedicated App as the sole bypass
actor and that App's private key being available only after protected
environment approval. As of the 2026-08-09 candidate record, these remote
controls remained unverified. That dated local record is not final proof; it
must be replaced with current GitHub API evidence before publication, and the
enable variable must remain absent/false until the checklist is completed.

## 3. Create the annotated tag and prerelease

The protected `publish-tag` job creates the annotated tag only after the same
dispatch has passed every release gate. The annotation binds the Genesis SHA
to exactly one `Resonance-SHA:` line, the derived Stage 3 tuple, the preflight
run, and all five artifact ID/name/attempt/digest/size tuples. The tag is
therefore created only after the exact-source gates have succeeded and the
original archive bytes remain available and hash correctly. A single Go
validator first requires the exact release-notes title as the first line,
exactly one `Status: publication-ready.` status line, no other status line, and
no pending-before-tag section. It runs once in the release gate and again in
`publish-tag` immediately before tag creation, so an immutable tag cannot be
published with a body that the post-tag publisher must reject. Because the
dedicated App token is not `GITHUB_TOKEN`, its push also triggers tag CI, which
validates the annotation and reruns the complete gate as defense in depth.

Lightweight tags are rejected. The publish job repeats the
`origin/main == candidate SHA` check after any environment approval delay,
immediately before tag creation. `main` may advance after tagging without
invalidating the immutable release. Manual `git tag` or `git push` publication
is not an approved release path, because it bypasses the pre-publication gate.
After environment approval and before pushing, the job also queries Resonance
`main` and requires exact equality with the bound consumer SHA, recording the
remote tip and check time in the annotation. The tag-triggered defense run
allows normal later `main` advancement only when that bound SHA remains an
ancestor of the then-current tip.

Before the tag push, `publish-tag` also queries the immutable-releases endpoint
with GitHub REST API version `2026-03-10` and downloads and hashes the five
preflight archives. It requires the candidate tag to have no GitHub Release
both before and after that download; a pre-created draft or published Release
therefore fails before the tag is pushed. After the push, without another
environment wait, it creates or resumes one exact draft, uploads only the five
expected archives, reads back their uploaded state/size/SHA-256, and leaves the
release as a prerelease draft. An unexpected release, asset, duplicate, or
conflicting byte sequence is never deleted or overwritten. If a concurrent
tag-triggered job has already published the exact Release while immutability is
propagating, this staging step only polls to the bounded deadline and then
validates the immutable six-asset result; it never uploads or PATCHes a
published Release.

After the tag-triggered defense-in-depth `release-gate` succeeds, the
`publish-prerelease` job enters the same protected `release` environment and
mints a new short-lived token from the dedicated Release App. It verifies the
local annotated tag and exact draft identity/assets. The tag run's own
release-evidence artifact uses the unique Actions name
`tag-defense-evidence-<sha>-<run-id>-<actual-attempt>`. The publisher validates
its current run/head/name/ID/digest/size through the Actions API, downloads and
hashes its original ZIP, and adds it as the sixth asset. Its durable Release
asset filename also encodes the Actions ID and digest, so this source tuple
remains auditable after Actions retention expires.

The publisher then rechecks the immutable setting immediately before the
transition and PATCHes the draft into a
non-draft prerelease whose body is byte-for-byte equal to
`docs/v1-rc2-release-notes.md`. It then polls until the API reports
`immutable: true` with the exact asset set. A partial draft can resume by
downloading the annotation-bound Actions artifacts by ID; a published release
is accepted only if its tag, title, body, flags, immutable state, and assets all
match. The API response's `target_commitish` must also equal the candidate SHA,
and the local annotated tag must peel to that same SHA. The workflow never
overwrites a mismatched release or asset.

If a failed-job rerun reuses the same tag-defense output tuple, an already
uploaded sixth asset remains sufficient even after its Actions source expires.
For a full rerun of the same tag workflow run, the run ID stays fixed while the
attempt changes. An older draft asset becomes canonical only after its filename
recovers the old Actions ID/digest and the still-available Actions metadata is
revalidated against the exact run/head/name/digest/size; otherwise the mutable
draft fails closed. Once the Release is immutable, that first accepted asset is
the canonical defense evidence and later attempts verify it without adding or
replacing assets. If a previous PATCH succeeded but immutability is still
propagating, a rerun verifies identity and all six assets and only polls; an
older-attempt defense source is still revalidated while the Release is mutable,
and offline trust begins only after `immutable: true`. The rerun never sends a
second PATCH.

Before the irreversible PATCH, the workflow downloads GitHub CLI `v2.96.0`,
verifies its pinned archive SHA-256, and checks the binary version. After the
PATCH, it uses that exact binary to verify and record the immutable release
attestation. Release publication is not reported green until both the REST
immutable-state check and attestation verification succeed.

### Fail-closed recovery

Release asset processing and immutable-attestation generation can be
eventually consistent. If an upload returns `422` while a same-name asset is
still in a starter state or has no digest, the publisher fails without deleting
or replacing anything. Wait for GitHub asset processing to settle, then rerun
the same tag workflow. If it does not converge, the release owner must inspect
the exact draft identity and asset set through the API and explicitly dispose of
the abandoned draft before a new attempt; the workflow never performs that
destructive recovery automatically.

If the exact non-draft release or its attestation has not propagated before the
bounded poll expires, first inspect the remote tag, target commit, byte-exact
body, flags, immutable setting, and six assets. Rerun only when that remote state
is exact: an already-PATCHed mutable release is polled without a second PATCH,
and an immutable exact release is only reverified. For attestation propagation,
wait and rerun the same tag workflow after the REST state is exact. Any identity,
asset, or attestation mismatch remains a release-owner stop condition, not a
reason to overwrite remote state.

The publisher repeats the same positive readiness validation after the tag
run. It does not remove or rewrite status or Pending text. The release owner
must resolve the listed surface/security/retention decisions, replace the draft
status with the exact publication-ready marker, remove the pre-tag Pending
section, and freeze that byte-exact body in the tagged commit. Tag-qualified
absolute documentation links are required because relative links in a Markdown
source file do not keep their `docs/` context on a GitHub Release page.

## 4. Verify the published module

After the tag-triggered workflow has completed `publish-prerelease`, exact-six
immutable-state verification, and attestation verification, and the module is
visible through the public Go proxy, use a clean module cache and no local
replacement:

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
