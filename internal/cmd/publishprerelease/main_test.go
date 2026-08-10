package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testSHA       = "0123456789abcdef0123456789abcdef01234567"
	testOtherSHA  = "89abcdef0123456789abcdef0123456789abcdef"
	testStage3SHA = "fedcba9876543210fedcba9876543210fedcba98"
)

func TestPrepareStageAndPublishImmutableRelease(t *testing.T) {
	spec := testReleaseSpec()
	preflightEvidence, archives := testReleaseEvidence(spec, false)
	finalEvidence := addTagDefenseEvidence(spec, preflightEvidence, archives)
	fixture := newGitHubFixture(t, spec, finalEvidence, archives)
	p := fixture.publisher(t)
	p.pollInterval = 0

	archiveDir := t.TempDir()
	prepared, err := p.prepareArchives(context.Background(), spec, preflightEvidence, archiveDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range artifactKinds {
		if prepared.Artifacts[kind].Size != int64(len(archives[kind])) {
			t.Fatalf("%s size = %d", kind, prepared.Artifacts[kind].Size)
		}
	}

	draft, err := p.stageDraft(context.Background(), spec, prepared, archiveDir)
	if err != nil {
		t.Fatal(err)
	}
	if !draft.Draft || draft.Immutable {
		t.Fatalf("staged release = %+v", draft)
	}
	if fixture.uploads != len(artifactKinds) {
		t.Fatalf("uploads = %d, want %d", fixture.uploads, len(artifactKinds))
	}

	preparedFinal := mergePreparedPreflightEvidence(finalEvidence, prepared)
	result, err := p.publish(context.Background(), spec, preparedFinal)
	if err != nil {
		t.Fatal(err)
	}
	if result.release.Draft || !result.release.Prerelease || !result.release.Immutable {
		t.Fatalf("published release = %+v", result.release)
	}
	if fixture.patches != 1 {
		t.Fatalf("patches = %d, want 1", fixture.patches)
	}

	// A later rerun verifies the immutable release and exact assets without
	// mutating it or needing the expiring Actions archives again.
	fixture.artifactsExpired = true
	if _, err := p.publish(context.Background(), spec, preparedFinal); err != nil {
		t.Fatal(err)
	}
	if fixture.uploads != len(finalEvidence.artifactKinds()) || fixture.patches != 1 {
		t.Fatalf("idempotent rerun mutated release: uploads=%d patches=%d", fixture.uploads, fixture.patches)
	}
}

func TestPublishAddsOnlyMissingTagDefenseWhenPreflightActionsArtifactsExpired(t *testing.T) {
	spec := testReleaseSpec()
	preflightEvidence, archives := testReleaseEvidence(spec, true)
	finalEvidence := addTagDefenseEvidence(spec, preflightEvidence, archives)
	fixture := newGitHubFixture(t, spec, finalEvidence, archives)
	fixture.release = fixture.matchingRelease(true, false)
	fixture.seedAssets(preflightEvidence, archives)
	for _, kind := range artifactKinds {
		fixture.expiredArtifacts[preflightEvidence.Artifacts[kind].ID] = true
	}

	result, err := fixture.publisher(t).publish(context.Background(), spec, finalEvidence)
	if err != nil {
		t.Fatal(err)
	}
	if !result.release.Immutable || fixture.uploads != 1 {
		t.Fatalf("result = %+v, uploads = %d", result.release, fixture.uploads)
	}
}

func TestPublishResumesAfterTagDefenseUploadWhenActionsExpired(t *testing.T) {
	spec := testReleaseSpec()
	preflightEvidence, archives := testReleaseEvidence(spec, true)
	finalEvidence := addTagDefenseEvidence(spec, preflightEvidence, archives)
	fixture := newGitHubFixture(t, spec, finalEvidence, archives)
	fixture.release = fixture.matchingRelease(true, false)
	fixture.seedAssets(finalEvidence, archives)
	fixture.artifactsExpired = true

	result, err := fixture.publisher(t).publish(context.Background(), spec, finalEvidence)
	if err != nil {
		t.Fatal(err)
	}
	if !result.release.Immutable || fixture.uploads != 0 || fixture.patches != 1 {
		t.Fatalf("result = %+v, uploads=%d patches=%d", result.release, fixture.uploads, fixture.patches)
	}
}

func TestPublishReRunAllKeepsFirstDurableTagDefenseAttempt(t *testing.T) {
	spec := testReleaseSpec()
	preflightEvidence, archives := testReleaseEvidence(spec, true)
	firstEvidence := addTagDefenseEvidence(spec, preflightEvidence, archives)
	fixture := newGitHubFixture(t, spec, firstEvidence, archives)
	fixture.release = fixture.matchingRelease(true, false)
	fixture.seedAssets(firstEvidence, archives)

	rerunEvidence := nextTagDefenseAttempt(spec, firstEvidence)
	result, err := fixture.publisher(t).publish(context.Background(), spec, rerunEvidence)
	if err != nil {
		t.Fatal(err)
	}
	if !result.release.Immutable || fixture.uploads != 0 || fixture.patches != 1 {
		t.Fatalf("result = %+v, uploads=%d patches=%d", result.release, fixture.uploads, fixture.patches)
	}
	for _, asset := range fixture.assets {
		if asset.Name == rerunEvidence.Artifacts[artifactTagDefense].assetName() {
			t.Fatal("rerun replaced the first durable tag-defense attempt")
		}
	}

	// A later full rerun validates the already immutable release using the
	// first durable attempt and does not need either Actions archive.
	fixture.artifactsExpired = true
	if _, err := fixture.publisher(t).publish(context.Background(), spec, rerunEvidence); err != nil {
		t.Fatal(err)
	}
	if fixture.uploads != 0 || fixture.patches != 1 {
		t.Fatalf("immutable rerun mutated release: uploads=%d patches=%d", fixture.uploads, fixture.patches)
	}
}

func TestPublishRejectsUnboundOlderDraftTagDefenseAsset(t *testing.T) {
	tests := map[string]func(*githubFixture, releaseEvidence){
		"legacy name without source tuple": func(fixture *githubFixture, evidence releaseEvidence) {
			mutateDefenseAsset(fixture, func(asset githubAsset) githubAsset {
				asset.Name = evidence.Artifacts[artifactTagDefense].Name + ".zip"
				return asset
			})
		},
		"filename digest mismatch": func(fixture *githubFixture, evidence releaseEvidence) {
			mutateDefenseAsset(fixture, func(asset githubAsset) githubAsset {
				asset.Name = strings.Replace(asset.Name, evidence.Artifacts[artifactTagDefense].Digest, strings.Repeat("c", 64), 1)
				return asset
			})
		},
		"unknown Actions source ID": func(fixture *githubFixture, _ releaseEvidence) {
			mutateDefenseAsset(fixture, func(asset githubAsset) githubAsset {
				asset.Name = strings.Replace(asset.Name, "-artifact-106-", "-artifact-999-", 1)
				return asset
			})
		},
		"expired Actions source": func(fixture *githubFixture, evidence releaseEvidence) {
			fixture.expiredArtifacts[evidence.Artifacts[artifactTagDefense].ID] = true
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			spec := testReleaseSpec()
			preflightEvidence, archives := testReleaseEvidence(spec, true)
			firstEvidence := addTagDefenseEvidence(spec, preflightEvidence, archives)
			fixture := newGitHubFixture(t, spec, firstEvidence, archives)
			fixture.release = fixture.matchingRelease(true, false)
			fixture.seedAssets(firstEvidence, archives)
			mutate(fixture, firstEvidence)

			_, err := fixture.publisher(t).publish(context.Background(), spec, nextTagDefenseAttempt(spec, firstEvidence))
			if err == nil {
				t.Fatal("publisher accepted an unbound older tag-defense draft asset")
			}
			if fixture.patches != 0 {
				t.Fatalf("publisher patched release after invalid defense asset: %d", fixture.patches)
			}
		})
	}
}

func TestPrepareArchivesRejectsDownloadedBytesThatDoNotMatchDigest(t *testing.T) {
	spec := testReleaseSpec()
	evidence, archives := testReleaseEvidence(spec, false)
	fixture := newGitHubFixture(t, spec, evidence, archives)
	fixture.downloadOverrides[evidence.Artifacts[artifactRace].ID] = []byte("tampered archive")

	_, err := fixture.publisher(t).prepareArchives(context.Background(), spec, evidence, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "downloaded archive") {
		t.Fatalf("error = %v, want downloaded archive mismatch", err)
	}
}

func TestPrepareArchivesRejectsExistingReleaseBeforeDownload(t *testing.T) {
	for _, draft := range []bool{true, false} {
		t.Run(fmt.Sprintf("draft=%t", draft), func(t *testing.T) {
			spec := testReleaseSpec()
			evidence, archives := testReleaseEvidence(spec, false)
			fixture := newGitHubFixture(t, spec, evidence, archives)
			fixture.release = fixture.matchingRelease(draft, !draft)

			_, err := fixture.publisher(t).prepareArchives(context.Background(), spec, evidence, t.TempDir())
			if err == nil || !strings.Contains(err.Error(), "already exists before downloading") {
				t.Fatalf("error = %v, want pre-download release rejection", err)
			}
			if fixture.artifactDownloads != 0 {
				t.Fatalf("downloaded %d artifacts after existing release was found", fixture.artifactDownloads)
			}
		})
	}
}

func TestPrepareArchivesRejectsReleaseCreatedDuringDownload(t *testing.T) {
	spec := testReleaseSpec()
	evidence, archives := testReleaseEvidence(spec, false)
	fixture := newGitHubFixture(t, spec, evidence, archives)
	fixture.createReleaseAfterDownloads = len(artifactKinds)

	_, err := fixture.publisher(t).prepareArchives(context.Background(), spec, evidence, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "already exists after downloading") {
		t.Fatalf("error = %v, want post-download release rejection", err)
	}
}

func TestStageDraftRejectsUnexpectedExistingAssetWithoutDeletion(t *testing.T) {
	spec := testReleaseSpec()
	evidence, archives := testReleaseEvidence(spec, true)
	fixture := newGitHubFixture(t, spec, evidence, archives)
	fixture.release = fixture.matchingRelease(true, false)
	fixture.assets[999] = githubAsset{ID: 999, Name: "foreign.zip", State: "uploaded", Size: 1, Digest: "sha256:" + strings.Repeat("0", 64)}

	_, err := fixture.publisher(t).stageDraft(context.Background(), spec, evidence, writeArchives(t, archives, evidence))
	if err == nil || !strings.Contains(err.Error(), "unexpected asset") {
		t.Fatalf("error = %v, want unexpected asset rejection", err)
	}
	if fixture.deletes != 0 {
		t.Fatalf("publisher deleted %d assets", fixture.deletes)
	}
}

func TestStageDraftWaitsForExactPublishedReleaseToBecomeImmutable(t *testing.T) {
	spec := testReleaseSpec()
	preflightEvidence, archives := testReleaseEvidence(spec, true)
	finalEvidence := addTagDefenseEvidence(spec, preflightEvidence, archives)
	fixture := newGitHubFixture(t, spec, finalEvidence, archives)
	fixture.release = fixture.matchingRelease(false, false)
	fixture.seedAssets(finalEvidence, archives)
	fixture.immutableAfterFetches = 2

	release, err := fixture.publisher(t).stageDraft(
		context.Background(), spec, preflightEvidence, writeArchives(t, archives, preflightEvidence),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !release.Immutable || fixture.releaseFetches != 2 || fixture.uploads != 0 || fixture.patches != 0 {
		t.Fatalf("release=%+v fetches=%d uploads=%d patches=%d", release, fixture.releaseFetches, fixture.uploads, fixture.patches)
	}
}

func TestStageDraftRejectsInvalidOrStuckPublishedRelease(t *testing.T) {
	tests := map[string]func(*githubFixture){
		"wrong identity": func(fixture *githubFixture) {
			fixture.release.Name = "wrong release"
		},
		"immutability timeout": func(_ *githubFixture) {},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			spec := testReleaseSpec()
			preflightEvidence, archives := testReleaseEvidence(spec, true)
			finalEvidence := addTagDefenseEvidence(spec, preflightEvidence, archives)
			fixture := newGitHubFixture(t, spec, finalEvidence, archives)
			fixture.release = fixture.matchingRelease(false, false)
			fixture.seedAssets(finalEvidence, archives)
			mutate(fixture)

			_, err := fixture.publisher(t).stageDraft(
				context.Background(), spec, preflightEvidence, writeArchives(t, archives, preflightEvidence),
			)
			if err == nil {
				t.Fatal("stageDraft accepted an invalid or permanently mutable published release")
			}
			if fixture.uploads != 0 || fixture.patches != 0 {
				t.Fatalf("stageDraft mutated published release: uploads=%d patches=%d", fixture.uploads, fixture.patches)
			}
		})
	}
}

func TestPublishRejectsPublishedMutableRelease(t *testing.T) {
	spec := testReleaseSpec()
	preflightEvidence, archives := testReleaseEvidence(spec, true)
	evidence := addTagDefenseEvidence(spec, preflightEvidence, archives)
	fixture := newGitHubFixture(t, spec, evidence, archives)
	fixture.release = fixture.matchingRelease(false, false)
	fixture.seedAssets(evidence, archives)

	_, err := fixture.publisher(t).publish(context.Background(), spec, evidence)
	if err == nil || !strings.Contains(err.Error(), "did not become immutable") {
		t.Fatalf("error = %v, want mutable release rejection", err)
	}
}

func TestPublishResumesNonDraftReleaseUntilImmutableWithoutSecondPatch(t *testing.T) {
	spec := testReleaseSpec()
	preflightEvidence, archives := testReleaseEvidence(spec, true)
	evidence := addTagDefenseEvidence(spec, preflightEvidence, archives)
	fixture := newGitHubFixture(t, spec, evidence, archives)
	fixture.release = fixture.matchingRelease(false, false)
	fixture.seedAssets(evidence, archives)
	fixture.immutableAfterFetches = 2

	result, err := fixture.publisher(t).publish(context.Background(), spec, evidence)
	if err != nil {
		t.Fatal(err)
	}
	if !result.release.Immutable || fixture.patches != 0 || fixture.releaseFetches != 2 {
		t.Fatalf("result=%+v patches=%d fetches=%d", result.release, fixture.patches, fixture.releaseFetches)
	}
}

func TestPublishRejectsMutableNonDraftOlderDefenseWhenActionsSourceExpired(t *testing.T) {
	spec := testReleaseSpec()
	preflightEvidence, archives := testReleaseEvidence(spec, true)
	firstEvidence := addTagDefenseEvidence(spec, preflightEvidence, archives)
	fixture := newGitHubFixture(t, spec, firstEvidence, archives)
	fixture.release = fixture.matchingRelease(false, false)
	fixture.seedAssets(firstEvidence, archives)
	fixture.artifactsExpired = true
	fixture.immutableAfterFetches = 1

	_, err := fixture.publisher(t).publish(context.Background(), spec, nextTagDefenseAttempt(spec, firstEvidence))
	if err == nil || fixture.releaseFetches != 0 || fixture.patches != 0 {
		t.Fatalf("error=%v fetches=%d patches=%d", err, fixture.releaseFetches, fixture.patches)
	}
}

func TestPublishRejectsMutablePublishedReleaseWithWrongAssetsBeforePolling(t *testing.T) {
	spec := testReleaseSpec()
	preflightEvidence, archives := testReleaseEvidence(spec, true)
	evidence := addTagDefenseEvidence(spec, preflightEvidence, archives)
	fixture := newGitHubFixture(t, spec, evidence, archives)
	fixture.release = fixture.matchingRelease(false, false)
	fixture.seedAssets(evidence, archives)
	fixture.assets[999] = githubAsset{ID: 999, Name: "foreign.zip", State: "uploaded", Size: 1, Digest: "sha256:" + strings.Repeat("d", 64)}
	fixture.immutableAfterFetches = 1

	_, err := fixture.publisher(t).publish(context.Background(), spec, evidence)
	if err == nil || fixture.releaseFetches != 0 || fixture.patches != 0 {
		t.Fatalf("error=%v fetches=%d patches=%d", err, fixture.releaseFetches, fixture.patches)
	}
}

func TestValidateReleaseIdentityRejectsWrongTargetCommitish(t *testing.T) {
	spec := testReleaseSpec()
	evidence, archives := testReleaseEvidence(spec, true)
	fixture := newGitHubFixture(t, spec, evidence, archives)
	release := fixture.matchingRelease(true, false)
	release.TargetCommitish = testOtherSHA
	if err := validateReleaseIdentity(release, spec, true); err == nil || !strings.Contains(err.Error(), "target_commitish") {
		t.Fatalf("error = %v", err)
	}
}

func TestPreTagCheckRequiresImmutableRepositorySetting(t *testing.T) {
	spec := testReleaseSpec()
	evidence, archives := testReleaseEvidence(spec, false)
	fixture := newGitHubFixture(t, spec, evidence, archives)
	fixture.immutableSetting = false

	_, err := fixture.publisher(t).prepareArchives(context.Background(), spec, evidence, t.TempDir())
	var responseErr *apiError
	if !errors.As(err, &responseErr) || responseErr.status != http.StatusNotFound {
		t.Fatalf("error = %v, want immutable setting 404", err)
	}
}

func TestParseTagEvidencePreservesPerArtifactAttemptsAndSizes(t *testing.T) {
	spec := testReleaseSpec()
	evidence, _ := testReleaseEvidence(spec, true)
	annotation := testTagAnnotation(evidence)
	parsed, err := parseTagEvidence(spec, annotation)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range artifactKinds {
		if parsed.Artifacts[kind] != evidence.Artifacts[kind] {
			t.Fatalf("%s artifact = %+v, want %+v", kind, parsed.Artifacts[kind], evidence.Artifacts[kind])
		}
	}
	if parsed.Stage3 != evidence.Stage3 {
		t.Fatalf("Stage3 = %+v, want %+v", parsed.Stage3, evidence.Stage3)
	}

	_, err = parseTagEvidence(spec, annotation+"\nnormal_artifact_size=123\n")
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate error = %v", err)
	}
}

func TestWorkflowAnnotationRendererFeedsTagParser(t *testing.T) {
	spec := testReleaseSpec()
	evidence, _ := testReleaseEvidence(spec, true)
	evidence.Stage3.TestedAt = "2026-08-09T12:34:56.123456789Z"
	annotation := renderWorkflowTagAnnotation(t, evidence)

	parsed, err := parseTagEvidence(spec, annotation)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Stage3 != evidence.Stage3 {
		t.Fatalf("Stage3 = %+v, want %+v", parsed.Stage3, evidence.Stage3)
	}
	for _, kind := range artifactKinds {
		if parsed.Artifacts[kind] != evidence.Artifacts[kind] {
			t.Fatalf("%s artifact = %+v, want %+v", kind, parsed.Artifacts[kind], evidence.Artifacts[kind])
		}
		needle := fmt.Sprintf("%s_artifact_attempt=", kind)
		if strings.Count(annotation, needle) != 1 {
			t.Fatalf("annotation contains %q %d times", needle, strings.Count(annotation, needle))
		}
	}
}

func TestParseTagEvidenceRejectsAllZeroEvidence(t *testing.T) {
	spec := testReleaseSpec()
	evidence, _ := testReleaseEvidence(spec, true)
	annotation := testTagAnnotation(evidence)
	annotation = strings.Replace(annotation, evidence.Stage3.ManifestDigest, strings.Repeat("0", 64), 1)
	if _, err := parseTagEvidence(spec, annotation); err == nil {
		t.Fatal("parseTagEvidence accepted an all-zero Stage 3 digest")
	}
}

func TestPreTagEvidenceFromEnvironmentRejectsReconstructedCurrentAttempt(t *testing.T) {
	spec := testReleaseSpec()
	evidence, _ := testReleaseEvidence(spec, false)
	environment := evidenceEnvironment(evidence)
	parsed, err := preTagEvidenceFromEnvironment(spec, func(key string) string { return environment[key] })
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Artifacts[artifactNormal].Attempt == parsed.Artifacts[artifactRace].Attempt {
		t.Fatal("test evidence does not exercise distinct partial-rerun attempts")
	}

	environment["RACE_NAME"] = expectedArtifactName(artifactRace, spec.sha, parsed.Artifacts[artifactNormal].Attempt)
	if _, err := preTagEvidenceFromEnvironment(spec, func(key string) string { return environment[key] }); err == nil {
		t.Fatal("pre-tag evidence accepted a name reconstructed from another attempt")
	}
}

func TestTagDefenseEvidenceUsesActualRunAndArtifactOutputs(t *testing.T) {
	spec := testReleaseSpec()
	preflightEvidence, archives := testReleaseEvidence(spec, true)
	expected := addTagDefenseEvidence(spec, preflightEvidence, archives)
	defense := expected.Artifacts[artifactTagDefense]
	environment := map[string]string{
		"GITHUB_RUN_ID":   strconv.FormatInt(expected.DefenseRun, 10),
		"DEFENSE_ID":      strconv.FormatInt(defense.ID, 10),
		"DEFENSE_NAME":    defense.Name,
		"DEFENSE_DIGEST":  defense.Digest,
		"DEFENSE_ATTEMPT": strconv.FormatInt(defense.Attempt, 10),
	}
	actual, err := withTagDefenseFromEnvironment(spec, preflightEvidence, func(key string) string { return environment[key] })
	if err != nil {
		t.Fatal(err)
	}
	if actual.DefenseRun != expected.DefenseRun || actual.Artifacts[artifactTagDefense] != defense {
		t.Fatalf("tag-defense evidence = %+v", actual)
	}

	environment["DEFENSE_NAME"] = fmt.Sprintf("tag-defense-evidence-%s-%s-%d", spec.sha, "9999", defense.Attempt)
	if _, err := withTagDefenseFromEnvironment(spec, preflightEvidence, func(key string) string { return environment[key] }); err == nil {
		t.Fatal("tag-defense evidence accepted a name for another run")
	}
}

func TestValidateSpecRequiresExactPublicationReadyContract(t *testing.T) {
	tests := map[string]string{
		"missing title":       publicationReadyStatus + "\n",
		"wrong title":         "# Genesis RC2 release notes\n\n" + publicationReadyStatus + "\n",
		"title not first":     "Preamble\n" + releaseTitle + "\n\n" + publicationReadyStatus + "\n",
		"missing status":      releaseTitle + "\n",
		"wrong status":        releaseTitle + "\n\nStatus: ready.\n",
		"alternate status":    releaseTitle + "\n\n" + publicationReadyStatus + "\nStatus: draft.\n",
		"alternate separator": releaseTitle + "\n\n" + publicationReadyStatus + "\nstatus : draft.\n",
		"indented status":     releaseTitle + "\n\n" + publicationReadyStatus + "\n  Status: draft.\n",
		"duplicate":           releaseTitle + "\n\n" + publicationReadyStatus + "\n" + publicationReadyStatus + "\n",
		"carriage return":     releaseTitle + "\r\n\r\n" + publicationReadyStatus + "\r\n",
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			spec := testReleaseSpec()
			spec.body = body
			if err := validateSpec(spec); err == nil {
				t.Fatal("validateSpec succeeded")
			}
		})
	}
}

func TestValidateSpecRejectsDraftAndPendingPublicationNotes(t *testing.T) {
	for _, marker := range publicationBlockers {
		spec := testReleaseSpec()
		spec.body += "\n" + marker + "\n"
		if err := validateSpec(spec); err == nil {
			t.Fatalf("validateSpec with %q succeeded", marker)
		}
	}
	for _, heading := range []string{
		"## Pending before tag publication",
		"### pending before tag publication - unresolved",
	} {
		spec := testReleaseSpec()
		spec.body += "\n" + heading + "\n"
		if err := validateSpec(spec); err == nil {
			t.Fatalf("validateSpec with %q succeeded", heading)
		}
	}
}

func TestRunValidateOnlyStopsBeforeTagAndGitHubChecks(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, releaseNotesPath), []byte(releaseTitle+"\n\n"+publicationReadyStatus+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	environment := map[string]string{"CANDIDATE_TAG": releaseTag, "CANDIDATE_SHA": testSHA}
	var output bytes.Buffer
	if err := run(context.Background(), []string{"--validate-only"}, func(key string) string { return environment[key] }, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "publication-ready") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestVerifyLocalAnnotatedTagChecksPeeledCommit(t *testing.T) {
	repositoryRoot, taggedSHA, otherSHA := createTaggedRepository(t)
	spec := testReleaseSpec()
	spec.sha = taggedSHA
	if err := verifyLocalAnnotatedTag(context.Background(), repositoryRoot, spec); err != nil {
		t.Fatalf("verify valid annotated tag: %v", err)
	}
	spec.sha = otherSHA
	if err := verifyLocalAnnotatedTag(context.Background(), repositoryRoot, spec); err == nil || !strings.Contains(err.Error(), "must peel to") {
		t.Fatalf("wrong peeled commit error = %v", err)
	}
}

type githubFixture struct {
	t                           *testing.T
	spec                        releaseSpec
	evidence                    releaseEvidence
	archives                    map[artifactKind][]byte
	downloadOverrides           map[int64][]byte
	server                      *httptest.Server
	immutableSetting            bool
	artifactsExpired            bool
	expiredArtifacts            map[int64]bool
	immutableAfterFetches       int
	releaseFetches              int
	createReleaseAfterDownloads int
	artifactDownloads           int
	release                     githubRelease
	assets                      map[int64]githubAsset
	assetBodies                 map[int64][]byte
	nextAssetID                 int64
	uploads                     int
	patches                     int
	deletes                     int
	mu                          sync.Mutex
}

func newGitHubFixture(t *testing.T, spec releaseSpec, evidence releaseEvidence, archives map[artifactKind][]byte) *githubFixture {
	t.Helper()
	fixture := &githubFixture{
		t:                 t,
		spec:              spec,
		evidence:          evidence,
		archives:          archives,
		downloadOverrides: make(map[int64][]byte),
		expiredArtifacts:  make(map[int64]bool),
		immutableSetting:  true,
		assets:            make(map[int64]githubAsset),
		assetBodies:       make(map[int64][]byte),
		nextAssetID:       500,
	}
	fixture.server = httptest.NewServer(http.HandlerFunc(fixture.handle))
	t.Cleanup(fixture.server.Close)
	return fixture
}

func (f *githubFixture) publisher(t *testing.T) *publisher {
	t.Helper()
	p, err := newPublisher(f.server.URL, "ceyewan/genesis", "test-token", f.server.Client())
	if err != nil {
		t.Fatal(err)
	}
	p.pollInterval = 0
	p.sleep = func(context.Context, time.Duration) error { return nil }
	return p
}

func (f *githubFixture) handle(w http.ResponseWriter, request *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if request.Header.Get("Authorization") != "Bearer test-token" {
		f.t.Fatalf("Authorization = %q", request.Header.Get("Authorization"))
	}
	if request.Header.Get("X-GitHub-Api-Version") != apiVersion {
		f.t.Fatalf("API version = %q", request.Header.Get("X-GitHub-Api-Version"))
	}
	path := request.URL.Path
	switch {
	case request.Method == http.MethodGet && path == "/repos/ceyewan/genesis/immutable-releases":
		if !f.immutableSetting {
			http.NotFound(w, request)
			return
		}
		writeJSON(f.t, w, map[string]bool{"enabled": true}, http.StatusOK)
	case request.Method == http.MethodGet && strings.HasPrefix(path, "/repos/ceyewan/genesis/actions/artifacts/"):
		f.handleArtifact(w, request)
	case request.Method == http.MethodGet && path == "/repos/ceyewan/genesis/releases":
		if f.release.ID == 0 {
			writeJSON(f.t, w, []githubRelease{}, http.StatusOK)
		} else {
			writeJSON(f.t, w, []githubRelease{f.release}, http.StatusOK)
		}
	case request.Method == http.MethodPost && path == "/repos/ceyewan/genesis/releases":
		var payload createReleaseRequest
		decodeRequest(f.t, request, &payload)
		if f.release.ID != 0 {
			http.Error(w, "already exists", http.StatusUnprocessableEntity)
			return
		}
		if !payload.Draft || !payload.Prerelease || payload.Body != f.spec.body {
			f.t.Fatalf("create payload = %+v", payload)
		}
		f.release = f.matchingRelease(true, false)
		writeJSON(f.t, w, f.release, http.StatusCreated)
	case request.Method == http.MethodGet && path == "/repos/ceyewan/genesis/releases/42/assets":
		writeJSON(f.t, w, f.sortedAssets(), http.StatusOK)
	case request.Method == http.MethodPost && path == "/upload/42/assets":
		f.handleAssetUpload(w, request)
	case request.Method == http.MethodGet && strings.HasPrefix(path, "/repos/ceyewan/genesis/releases/assets/"):
		id := mustPathID(f.t, path)
		asset, ok := f.assets[id]
		if !ok {
			http.NotFound(w, request)
			return
		}
		writeJSON(f.t, w, asset, http.StatusOK)
	case request.Method == http.MethodGet && path == "/repos/ceyewan/genesis/releases/42":
		f.releaseFetches++
		if f.immutableAfterFetches > 0 && f.releaseFetches >= f.immutableAfterFetches {
			f.release.Immutable = true
		}
		writeJSON(f.t, w, f.release, http.StatusOK)
	case request.Method == http.MethodPatch && path == "/repos/ceyewan/genesis/releases/42":
		var payload updateReleaseRequest
		decodeRequest(f.t, request, &payload)
		if payload.Draft || !payload.Prerelease || payload.Body != f.spec.body {
			f.t.Fatalf("update payload = %+v", payload)
		}
		f.patches++
		f.release.Draft = false
		f.release.Immutable = true
		f.release.HTMLURL = "https://github.test/ceyewan/genesis/releases/tag/" + f.spec.tag
		writeJSON(f.t, w, f.release, http.StatusOK)
	case request.Method == http.MethodDelete:
		f.deletes++
		f.t.Fatalf("unexpected destructive request: %s", path)
	default:
		f.t.Fatalf("unexpected request: %s %s?%s", request.Method, path, request.URL.RawQuery)
	}
}

func (f *githubFixture) handleArtifact(w http.ResponseWriter, request *http.Request) {
	parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
	if len(parts) < 6 {
		f.t.Fatalf("artifact path = %q", request.URL.Path)
	}
	id, err := strconv.ParseInt(parts[5], 10, 64)
	if err != nil {
		f.t.Fatal(err)
	}
	var kind artifactKind
	var expected artifactEvidence
	for _, candidate := range f.evidence.artifactKinds() {
		if f.evidence.Artifacts[candidate].ID == id {
			kind = candidate
			expected = f.evidence.Artifacts[candidate]
			break
		}
	}
	if kind == "" {
		http.NotFound(w, request)
		return
	}
	body := f.archives[kind]
	if override, ok := f.downloadOverrides[id]; ok {
		body = override
	}
	if len(parts) == 7 && parts[6] == "zip" {
		f.artifactDownloads++
		if f.createReleaseAfterDownloads > 0 && f.artifactDownloads == f.createReleaseAfterDownloads {
			f.release = f.matchingRelease(true, false)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
		return
	}
	artifact := githubArtifact{
		ID:          id,
		Name:        expected.Name,
		SizeInBytes: int64(len(f.archives[kind])),
		Expired:     f.artifactsExpired || f.expiredArtifacts[id],
		Digest:      "sha256:" + expected.Digest,
		WorkflowRun: &struct {
			ID      int64  `json:"id"`
			HeadSHA string `json:"head_sha"`
		}{ID: f.expectedRun(kind), HeadSHA: f.evidence.WorkflowSHA},
	}
	writeJSON(f.t, w, artifact, http.StatusOK)
}

func (f *githubFixture) handleAssetUpload(w http.ResponseWriter, request *http.Request) {
	name := request.URL.Query().Get("name")
	for _, asset := range f.assets {
		if asset.Name == name {
			http.Error(w, "already exists", http.StatusUnprocessableEntity)
			return
		}
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		f.t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	f.nextAssetID++
	asset := githubAsset{
		ID:     f.nextAssetID,
		Name:   name,
		State:  "uploaded",
		Size:   int64(len(body)),
		Digest: "sha256:" + hex.EncodeToString(sum[:]),
	}
	f.assets[asset.ID] = asset
	f.assetBodies[asset.ID] = body
	f.uploads++
	writeJSON(f.t, w, asset, http.StatusCreated)
}

func (f *githubFixture) matchingRelease(draft, immutable bool) githubRelease {
	return githubRelease{
		ID:              42,
		TagName:         f.spec.tag,
		TargetCommitish: f.spec.sha,
		Name:            f.spec.name,
		Body:            f.spec.body,
		Draft:           draft,
		Prerelease:      true,
		Immutable:       immutable,
		HTMLURL:         "https://github.test/ceyewan/genesis/releases/tag/" + f.spec.tag,
		UploadURL:       f.server.URL + "/upload/42/assets{?name,label}",
	}
}

func (f *githubFixture) seedAssets(evidence releaseEvidence, archives map[artifactKind][]byte) {
	for _, kind := range evidence.artifactKinds() {
		artifact := evidence.Artifacts[kind]
		size := artifact.Size
		if size == 0 {
			size = int64(len(archives[kind]))
		}
		f.nextAssetID++
		f.assets[f.nextAssetID] = githubAsset{
			ID:     f.nextAssetID,
			Name:   artifact.assetName(),
			State:  "uploaded",
			Size:   size,
			Digest: "sha256:" + artifact.Digest,
		}
		f.assetBodies[f.nextAssetID] = archives[kind]
	}
}

func (f *githubFixture) expectedRun(kind artifactKind) int64 {
	if kind == artifactTagDefense {
		return f.evidence.DefenseRun
	}
	return f.evidence.PreflightRun
}

func mutateDefenseAsset(fixture *githubFixture, mutate func(githubAsset) githubAsset) {
	for id, asset := range fixture.assets {
		if strings.HasPrefix(asset.Name, "tag-defense-evidence-") {
			fixture.assets[id] = mutate(asset)
			return
		}
	}
	fixture.t.Fatal("tag-defense release asset not found")
}

func (f *githubFixture) sortedAssets() []githubAsset {
	assets := make([]githubAsset, 0, len(f.assets))
	for _, asset := range f.assets {
		assets = append(assets, asset)
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].ID < assets[j].ID })
	return assets
}

func testReleaseSpec() releaseSpec {
	return releaseSpec{
		tag:  releaseTag,
		sha:  testSHA,
		name: releaseTag,
		body: releaseTitle + "\n\n" + publicationReadyStatus + "\n\nTrailing newline is preserved.\n",
	}
}

func testReleaseEvidence(spec releaseSpec, includeSizes bool) (releaseEvidence, map[artifactKind][]byte) {
	evidence := releaseEvidence{
		PreflightRun:     9001,
		PreflightAttempt: 5,
		WorkflowSHA:      spec.sha,
		ResonanceSHA:     testOtherSHA,
		Stage3: stage3Evidence{
			ManifestPath:   stage3ManifestPath,
			ManifestDigest: strings.Repeat("a", 64),
			TestedSHA:      testStage3SHA,
			TestedAt:       "2026-08-09T12:34:56Z",
		},
		Artifacts: make(map[artifactKind]artifactEvidence, len(artifactKinds)),
	}
	archives := make(map[artifactKind][]byte, len(artifactKinds))
	for index, kind := range artifactKinds {
		attempt := int64(index + 1)
		if kind == artifactReleaseEvidence {
			attempt = evidence.PreflightAttempt
		}
		body := fmt.Appendf(nil, "zip bytes for %s attempt %d", kind, attempt)
		sum := sha256.Sum256(body)
		artifact := artifactEvidence{
			Kind:    kind,
			ID:      int64(100 + index),
			Name:    expectedArtifactName(kind, spec.sha, attempt),
			Digest:  hex.EncodeToString(sum[:]),
			Attempt: attempt,
		}
		if includeSizes {
			artifact.Size = int64(len(body))
		}
		evidence.Artifacts[kind] = artifact
		archives[kind] = body
	}
	return evidence, archives
}

func addTagDefenseEvidence(
	spec releaseSpec,
	preflight releaseEvidence,
	archives map[artifactKind][]byte,
) releaseEvidence {
	evidence := preflight
	evidence.Artifacts = make(map[artifactKind]artifactEvidence, len(preflight.Artifacts)+1)
	maps.Copy(evidence.Artifacts, preflight.Artifacts)
	evidence.DefenseRun = 9002
	attempt := int64(7)
	body := []byte("tag-triggered release-gate evidence zip")
	sum := sha256.Sum256(body)
	evidence.Artifacts[artifactTagDefense] = artifactEvidence{
		Kind:    artifactTagDefense,
		ID:      106,
		Name:    fmt.Sprintf("tag-defense-evidence-%s-%d-%d", spec.sha, evidence.DefenseRun, attempt),
		Digest:  hex.EncodeToString(sum[:]),
		Attempt: attempt,
	}
	archives[artifactTagDefense] = body
	return evidence
}

func mergePreparedPreflightEvidence(final, preparedPreflight releaseEvidence) releaseEvidence {
	evidence := final
	evidence.Artifacts = make(map[artifactKind]artifactEvidence, len(final.Artifacts))
	maps.Copy(evidence.Artifacts, final.Artifacts)
	for _, kind := range artifactKinds {
		evidence.Artifacts[kind] = preparedPreflight.Artifacts[kind]
	}
	return evidence
}

func nextTagDefenseAttempt(spec releaseSpec, previous releaseEvidence) releaseEvidence {
	evidence := previous
	evidence.CanonicalDefense = nil
	evidence.Artifacts = make(map[artifactKind]artifactEvidence, len(previous.Artifacts))
	maps.Copy(evidence.Artifacts, previous.Artifacts)
	defense := evidence.Artifacts[artifactTagDefense]
	defense.ID++
	defense.Attempt++
	defense.Name = fmt.Sprintf("tag-defense-evidence-%s-%d-%d", spec.sha, evidence.DefenseRun, defense.Attempt)
	defense.Digest = strings.Repeat("b", 64)
	defense.Size = 0
	evidence.Artifacts[artifactTagDefense] = defense
	return evidence
}

func testTagAnnotation(evidence releaseEvidence) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "Genesis %s\n\n", releaseTag)
	fmt.Fprintf(&builder, "Resonance-SHA: %s\n", evidence.ResonanceSHA)
	fmt.Fprintf(&builder, "Stage3-Manifest-Path: %s\n", evidence.Stage3.ManifestPath)
	fmt.Fprintf(&builder, "Stage3-Manifest-SHA256: %s\n", evidence.Stage3.ManifestDigest)
	fmt.Fprintf(&builder, "Stage3-Tested-Resonance-SHA: %s\n", evidence.Stage3.TestedSHA)
	fmt.Fprintf(&builder, "Stage3-Tested-At: %s\n", evidence.Stage3.TestedAt)
	fmt.Fprintf(&builder, "Resonance-Main-Tip-At-Publish: %s\n", evidence.ResonanceSHA)
	fmt.Fprintln(&builder, "Resonance-Main-Checked-At: 2026-08-09T12:35:00Z")
	fmt.Fprintf(&builder, "Preflight-Run: %d\n", evidence.PreflightRun)
	fmt.Fprintf(&builder, "Preflight-Attempt: %d\n", evidence.PreflightAttempt)
	fmt.Fprintf(&builder, "Workflow-SHA: %s\n", evidence.WorkflowSHA)
	for _, kind := range artifactKinds {
		artifact := evidence.Artifacts[kind]
		fmt.Fprintf(&builder, "%s_artifact_sha256=%s\n", kind, artifact.Digest)
		fmt.Fprintf(&builder, "%s_artifact_id=%d\n", kind, artifact.ID)
		fmt.Fprintf(&builder, "%s_artifact_name=%s\n", kind, artifact.Name)
		fmt.Fprintf(&builder, "%s_artifact_attempt=%d\n", kind, artifact.Attempt)
		fmt.Fprintf(&builder, "%s_artifact_size=%d\n", kind, artifact.Size)
	}
	return builder.String()
}

func evidenceEnvironment(evidence releaseEvidence) map[string]string {
	environment := map[string]string{
		"GITHUB_RUN_ID":               strconv.FormatInt(evidence.PreflightRun, 10),
		"GITHUB_WORKFLOW_SHA":         evidence.WorkflowSHA,
		"RESONANCE_SHA":               evidence.ResonanceSHA,
		"STAGE3_MANIFEST_PATH":        evidence.Stage3.ManifestPath,
		"STAGE3_MANIFEST_SHA256":      evidence.Stage3.ManifestDigest,
		"STAGE3_TESTED_RESONANCE_SHA": evidence.Stage3.TestedSHA,
		"STAGE3_TESTED_AT":            evidence.Stage3.TestedAt,
	}
	for _, kind := range artifactKinds {
		artifact := evidence.Artifacts[kind]
		prefix := strings.ToUpper(string(kind))
		environment[prefix+"_ID"] = strconv.FormatInt(artifact.ID, 10)
		environment[prefix+"_NAME"] = artifact.Name
		environment[prefix+"_DIGEST"] = artifact.Digest
		environment[prefix+"_ATTEMPT"] = strconv.FormatInt(artifact.Attempt, 10)
		if artifact.Size > 0 {
			environment[prefix+"_SIZE"] = strconv.FormatInt(artifact.Size, 10)
		}
	}
	return environment
}

func renderWorkflowTagAnnotation(t *testing.T, evidence releaseEvidence) string {
	t.Helper()
	environment := evidenceEnvironment(evidence)
	environment["CANDIDATE_TAG"] = releaseTag
	environment["RESONANCE_MAIN_TIP"] = evidence.ResonanceSHA
	environment["RESONANCE_MAIN_CHECKED_AT"] = "2026-08-09T12:35:00Z"

	script := filepath.Join("..", "..", "..", ".github", "scripts", "render-rc2-tag-annotation.sh")
	command := exec.Command("bash", script)
	command.Env = os.Environ()
	for key, value := range environment {
		command.Env = append(command.Env, key+"="+value)
	}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("render workflow tag annotation: %v: %s", err, output)
	}
	return string(output)
}

func writeArchives(t *testing.T, archives map[artifactKind][]byte, evidence releaseEvidence) string {
	t.Helper()
	directory := t.TempDir()
	for _, kind := range artifactKinds {
		if err := os.WriteFile(filepath.Join(directory, evidence.Artifacts[kind].assetName()), archives[kind], 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return directory
}

func createTaggedRepository(t *testing.T) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	runGit(t, root, "init", "--quiet")
	runGit(t, root, "config", "user.name", "Genesis Test")
	runGit(t, root, "config", "user.email", "genesis-test@example.invalid")
	path := filepath.Join(root, "release.txt")
	if err := os.WriteFile(path, []byte("tagged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "release.txt")
	runGit(t, root, "commit", "--quiet", "-m", "tagged commit")
	taggedSHA := runGit(t, root, "rev-parse", "HEAD")
	runGit(t, root, "tag", "-a", releaseTag, "-m", "RC2")
	if err := os.WriteFile(path, []byte("later\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "release.txt")
	runGit(t, root, "commit", "--quiet", "-m", "later commit")
	return root, taggedSHA, runGit(t, root, "rev-parse", "HEAD")
}

func runGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	commandArgs := append([]string{"-C", root}, args...)
	output, err := exec.Command("git", commandArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func writeJSON(t *testing.T, writer http.ResponseWriter, value any, status int) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		t.Fatal(err)
	}
}

func decodeRequest(t *testing.T, request *http.Request, output any) {
	t.Helper()
	if err := json.NewDecoder(request.Body).Decode(output); err != nil {
		t.Fatal(err)
	}
}

func mustPathID(t *testing.T, path string) int64 {
	t.Helper()
	parts := strings.Split(strings.Trim(path, "/"), "/")
	id, err := strconv.ParseInt(parts[len(parts)-1], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
