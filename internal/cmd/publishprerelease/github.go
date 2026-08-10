package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxErrorBody           = 16 << 10
	maxJSONBody            = 4 << 20
	maxArtifactArchiveSize = int64(2 << 30)
	maxReleasePages        = 20
	finalPollAttempts      = 12
	finalPollInterval      = time.Second
)

type githubArtifact struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	SizeInBytes int64  `json:"size_in_bytes"`
	Expired     bool   `json:"expired"`
	Digest      string `json:"digest"`
	WorkflowRun *struct {
		ID      int64  `json:"id"`
		HeadSHA string `json:"head_sha"`
	} `json:"workflow_run"`
}

type githubAsset struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	State   string `json:"state"`
	Size    int64  `json:"size"`
	Digest  string `json:"digest"`
	URL     string `json:"url"`
	Browser string `json:"browser_download_url"`
}

type githubRelease struct {
	ID              int64  `json:"id"`
	TagName         string `json:"tag_name"`
	TargetCommitish string `json:"target_commitish"`
	Name            string `json:"name"`
	Body            string `json:"body"`
	Draft           bool   `json:"draft"`
	Prerelease      bool   `json:"prerelease"`
	Immutable       bool   `json:"immutable"`
	HTMLURL         string `json:"html_url"`
	UploadURL       string `json:"upload_url"`
}

type createReleaseRequest struct {
	TagName              string `json:"tag_name"`
	TargetCommitish      string `json:"target_commitish"`
	Name                 string `json:"name"`
	Body                 string `json:"body"`
	Draft                bool   `json:"draft"`
	Prerelease           bool   `json:"prerelease"`
	GenerateReleaseNotes bool   `json:"generate_release_notes"`
	MakeLatest           string `json:"make_latest"`
}

type updateReleaseRequest struct {
	TagName              string `json:"tag_name"`
	TargetCommitish      string `json:"target_commitish"`
	Name                 string `json:"name"`
	Body                 string `json:"body"`
	Draft                bool   `json:"draft"`
	Prerelease           bool   `json:"prerelease"`
	GenerateReleaseNotes bool   `json:"generate_release_notes"`
	MakeLatest           string `json:"make_latest"`
}

type publishResult struct {
	release   githubRelease
	published bool
}

type publisher struct {
	apiURL       string
	repository   string
	token        string
	client       *http.Client
	pollAttempts int
	pollInterval time.Duration
	sleep        func(context.Context, time.Duration) error
}

type apiError struct {
	method string
	path   string
	status int
	body   string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("GitHub API %s %s returned %d: %s", e.method, e.path, e.status, e.body)
}

func newPublisher(apiURL, repository, token string, client *http.Client) (*publisher, error) {
	parsed, err := url.Parse(apiURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid GitHub API URL %q", apiURL)
	}
	if !repositoryPattern.MatchString(repository) {
		return nil, fmt.Errorf("invalid GITHUB_REPOSITORY %q", repository)
	}
	if token == "" {
		return nil, errors.New("RELEASE_APP_TOKEN is empty")
	}
	if client == nil {
		return nil, errors.New("HTTP client is nil")
	}
	return &publisher{
		apiURL:       strings.TrimRight(apiURL, "/"),
		repository:   repository,
		token:        token,
		client:       client,
		pollAttempts: finalPollAttempts,
		pollInterval: finalPollInterval,
		sleep:        sleepContext,
	}, nil
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (p *publisher) prepareArchives(ctx context.Context, spec releaseSpec, evidence releaseEvidence, archiveDir string) (releaseEvidence, error) {
	if err := p.requireImmutableReleases(ctx); err != nil {
		return releaseEvidence{}, err
	}
	if err := p.requireReleaseAbsent(ctx, spec.tag, "before downloading pre-tag artifacts"); err != nil {
		return releaseEvidence{}, err
	}
	if err := os.MkdirAll(archiveDir, 0o700); err != nil {
		return releaseEvidence{}, fmt.Errorf("create archive directory: %w", err)
	}
	for _, kind := range artifactKinds {
		expected := evidence.Artifacts[kind]
		metadata, err := p.fetchArtifactMetadata(ctx, expected, evidence)
		if err != nil {
			return releaseEvidence{}, err
		}
		expected.Size = metadata.SizeInBytes
		if err := p.downloadArtifact(ctx, expected, archiveDir); err != nil {
			return releaseEvidence{}, err
		}
		evidence.Artifacts[kind] = expected
	}
	if err := validateReleaseEvidence(spec, evidence, true); err != nil {
		return releaseEvidence{}, err
	}
	if err := p.requireReleaseAbsent(ctx, spec.tag, "after downloading pre-tag artifacts"); err != nil {
		return releaseEvidence{}, err
	}
	return evidence, nil
}

func (p *publisher) stageDraft(ctx context.Context, spec releaseSpec, evidence releaseEvidence, archiveDir string) (githubRelease, error) {
	if err := p.requireImmutableReleases(ctx); err != nil {
		return githubRelease{}, err
	}
	if err := validateArchiveDirectory(evidence, archiveDir); err != nil {
		return githubRelease{}, err
	}
	release, found, err := p.findRelease(ctx, spec.tag)
	if err != nil {
		return githubRelease{}, err
	}
	if found && !release.Draft {
		if err := validateReleaseIdentity(release, spec, false); err != nil {
			return githubRelease{}, fmt.Errorf("published GitHub release is inconsistent: %w", err)
		}
		if !release.Immutable {
			return p.waitForImmutablePreflightRelease(ctx, release.ID, spec, evidence)
		}
		if err := p.validatePublishedPreflightRelease(ctx, release, spec, evidence); err != nil {
			return githubRelease{}, err
		}
		return release, nil
	}
	if !found {
		release, err = p.createDraftWithConflictRecovery(ctx, spec)
		if err != nil {
			return githubRelease{}, err
		}
	}
	if err := validateReleaseIdentity(release, spec, true); err != nil {
		return githubRelease{}, fmt.Errorf("draft release is inconsistent: %w", err)
	}
	if err := p.ensureDraftAssets(ctx, release, evidence, archiveDir); err != nil {
		return githubRelease{}, err
	}
	refetched, err := p.fetchReleaseByID(ctx, release.ID)
	if err != nil {
		return githubRelease{}, err
	}
	if err := validateReleaseIdentity(refetched, spec, true); err != nil {
		return githubRelease{}, fmt.Errorf("staged draft changed unexpectedly: %w", err)
	}
	if err := p.validateExactAssets(ctx, refetched.ID, evidence); err != nil {
		return githubRelease{}, err
	}
	return refetched, nil
}

func (p *publisher) publish(ctx context.Context, spec releaseSpec, evidence releaseEvidence) (publishResult, error) {
	if err := validateFinalReleaseEvidence(spec, evidence); err != nil {
		return publishResult{}, err
	}
	if err := p.requireImmutableReleases(ctx); err != nil {
		return publishResult{}, err
	}
	release, found, err := p.findRelease(ctx, spec.tag)
	if err != nil {
		return publishResult{}, err
	}
	if found && !release.Draft {
		evidence, err = p.canonicalizeTagDefenseAsset(ctx, release.ID, spec, evidence, !release.Immutable)
		if err != nil {
			return publishResult{}, err
		}
		resumed, resumeErr := p.resumePublishedRelease(ctx, release, spec, evidence)
		if resumeErr != nil {
			return publishResult{}, resumeErr
		}
		return publishResult{release: resumed}, nil
	}

	if !found {
		release, err = p.createDraftWithConflictRecovery(ctx, spec)
		if err != nil {
			return publishResult{}, err
		}
		if !release.Draft {
			evidence, err = p.canonicalizeTagDefenseAsset(ctx, release.ID, spec, evidence, !release.Immutable)
			if err != nil {
				return publishResult{}, err
			}
			resumed, resumeErr := p.resumePublishedRelease(ctx, release, spec, evidence)
			if resumeErr != nil {
				return publishResult{}, resumeErr
			}
			return publishResult{release: resumed}, nil
		}
	}
	if err := validateReleaseIdentity(release, spec, true); err != nil {
		return publishResult{}, fmt.Errorf("draft release is inconsistent: %w", err)
	}
	evidence, err = p.canonicalizeTagDefenseAsset(ctx, release.ID, spec, evidence, true)
	if err != nil {
		return publishResult{}, err
	}

	missing, err := p.missingDraftArtifacts(ctx, release.ID, evidence)
	if err != nil {
		return publishResult{}, err
	}
	if len(missing) > 0 {
		archiveDir, mkdirErr := os.MkdirTemp("", "genesis-release-artifacts-")
		if mkdirErr != nil {
			return publishResult{}, fmt.Errorf("create temporary archive directory: %w", mkdirErr)
		}
		defer os.RemoveAll(archiveDir)
		prepared, prepareErr := p.prepareArtifacts(ctx, spec, evidence, archiveDir, missing)
		if prepareErr != nil {
			return publishResult{}, fmt.Errorf("recover incomplete draft from exact Actions artifacts: %w", prepareErr)
		}
		evidence = prepared
		if ensureErr := p.ensureDraftAssets(ctx, release, prepared, archiveDir); ensureErr != nil {
			return publishResult{}, ensureErr
		}
	}
	if err := p.validateExactAssets(ctx, release.ID, evidence); err != nil {
		return publishResult{}, err
	}
	// Recheck immediately before the irreversible draft publication transition.
	if err := p.requireImmutableReleases(ctx); err != nil {
		return publishResult{}, err
	}
	published, err := p.publishDraft(ctx, release.ID, spec)
	if err != nil {
		return publishResult{}, err
	}
	final, err := p.waitForImmutableRelease(ctx, published.ID, spec, evidence)
	if err != nil {
		return publishResult{}, err
	}
	return publishResult{release: final, published: true}, nil
}

func (p *publisher) canonicalizeTagDefenseAsset(
	ctx context.Context,
	releaseID int64,
	spec releaseSpec,
	evidence releaseEvidence,
	requireSourceRevalidation bool,
) (releaseEvidence, error) {
	assets, err := p.listReleaseAssets(ctx, releaseID)
	if err != nil {
		return releaseEvidence{}, err
	}
	current := evidence.Artifacts[artifactTagDefense]
	var canonical *artifactEvidence
	for _, asset := range assets {
		if !strings.HasPrefix(asset.Name, "tag-defense-evidence-") {
			continue
		}
		if canonical != nil {
			return releaseEvidence{}, errors.New("release contains multiple tag-defense assets for the same tag run")
		}
		identity, parseErr := parseTagDefenseReleaseAssetName(asset.Name)
		if parseErr != nil {
			return releaseEvidence{}, parseErr
		}
		if identity.CandidateSHA != spec.sha || identity.Run != evidence.DefenseRun {
			return releaseEvidence{}, fmt.Errorf("tag-defense release asset %q belongs to a different candidate or run", asset.Name)
		}
		if identity.Attempt > current.Attempt {
			return releaseEvidence{}, fmt.Errorf("tag-defense release asset attempt %d is newer than current run attempt %d", identity.Attempt, current.Attempt)
		}
		if asset.ID <= 0 || asset.State != "uploaded" || asset.Size <= 0 || !strings.HasPrefix(asset.Digest, "sha256:") {
			return releaseEvidence{}, fmt.Errorf("tag-defense release asset %q is not in a durable uploaded state", asset.Name)
		}
		digestValue := strings.TrimPrefix(asset.Digest, "sha256:")
		if !validDigest(digestValue) {
			return releaseEvidence{}, fmt.Errorf("tag-defense release asset %q has invalid digest %q", asset.Name, asset.Digest)
		}
		if digestValue != identity.Digest {
			return releaseEvidence{}, errors.New("tag-defense release asset filename digest does not match its uploaded digest")
		}
		if identity.Attempt == current.Attempt && asset.Name != current.assetName() {
			return releaseEvidence{}, errors.New("current tag-defense release asset conflicts with current Actions evidence")
		}
		locked := current
		locked.ID = identity.ArtifactID
		locked.Name = fmt.Sprintf("tag-defense-evidence-%s-%d-%d", identity.CandidateSHA, identity.Run, identity.Attempt)
		locked.Digest = identity.Digest
		locked.Attempt = identity.Attempt
		locked.Size = asset.Size
		if requireSourceRevalidation && identity.Attempt < current.Attempt {
			if _, metadataErr := p.fetchArtifactMetadata(ctx, locked, evidence); metadataErr != nil {
				return releaseEvidence{}, fmt.Errorf("revalidate existing draft tag-defense asset against Actions: %w", metadataErr)
			}
		}
		canonical = &locked
	}
	evidence.CanonicalDefense = canonical
	return evidence, nil
}

func (p *publisher) prepareArtifacts(
	ctx context.Context,
	spec releaseSpec,
	evidence releaseEvidence,
	archiveDir string,
	kinds []artifactKind,
) (releaseEvidence, error) {
	if err := os.MkdirAll(archiveDir, 0o700); err != nil {
		return releaseEvidence{}, fmt.Errorf("create archive directory: %w", err)
	}
	for _, kind := range kinds {
		expected, ok := evidence.Artifacts[kind]
		if !ok {
			return releaseEvidence{}, fmt.Errorf("missing %s artifact evidence", kind)
		}
		metadata, err := p.fetchArtifactMetadata(ctx, expected, evidence)
		if err != nil {
			return releaseEvidence{}, err
		}
		expected.Size = metadata.SizeInBytes
		if err := p.downloadArtifact(ctx, expected, archiveDir); err != nil {
			return releaseEvidence{}, err
		}
		evidence.Artifacts[kind] = expected
	}
	if err := validateFinalReleaseEvidence(spec, evidence); err != nil {
		return releaseEvidence{}, err
	}
	return evidence, nil
}

func (p *publisher) requireImmutableReleases(ctx context.Context) error {
	path := "/repos/" + p.repository + "/immutable-releases"
	request, err := p.newRequest(ctx, http.MethodGet, p.apiURL+path, nil)
	if err != nil {
		return err
	}
	response, err := p.client.Do(request)
	if err != nil {
		return fmt.Errorf("query immutable releases setting: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return responseError(request.Method, path, response)
	}
	var setting struct {
		Enabled bool `json:"enabled"`
	}
	if err := decodeJSON(response.Body, &setting); err != nil {
		return fmt.Errorf("decode immutable releases setting: %w", err)
	}
	if !setting.Enabled {
		return errors.New("repository immutable releases setting is not enabled")
	}
	return nil
}

func (p *publisher) fetchArtifactMetadata(ctx context.Context, expected artifactEvidence, evidence releaseEvidence) (githubArtifact, error) {
	path := fmt.Sprintf("/repos/%s/actions/artifacts/%d", p.repository, expected.ID)
	request, err := p.newRequest(ctx, http.MethodGet, p.apiURL+path, nil)
	if err != nil {
		return githubArtifact{}, err
	}
	response, err := p.client.Do(request)
	if err != nil {
		return githubArtifact{}, fmt.Errorf("fetch %s artifact metadata: %w", expected.Kind, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return githubArtifact{}, responseError(request.Method, path, response)
	}
	var artifact githubArtifact
	if err := decodeJSON(response.Body, &artifact); err != nil {
		return githubArtifact{}, fmt.Errorf("decode %s artifact metadata: %w", expected.Kind, err)
	}
	if artifact.ID != expected.ID || artifact.Name != expected.Name {
		return githubArtifact{}, fmt.Errorf("%s artifact metadata identity mismatch", expected.Kind)
	}
	if artifact.Expired {
		return githubArtifact{}, fmt.Errorf("%s artifact %d has expired", expected.Kind, expected.ID)
	}
	if artifact.SizeInBytes <= 0 || artifact.SizeInBytes > maxArtifactArchiveSize {
		return githubArtifact{}, fmt.Errorf("%s artifact size %d is outside the accepted range", expected.Kind, artifact.SizeInBytes)
	}
	if expected.Size > 0 && artifact.SizeInBytes != expected.Size {
		return githubArtifact{}, fmt.Errorf("%s artifact size must be %d, got %d", expected.Kind, expected.Size, artifact.SizeInBytes)
	}
	if artifact.Digest != "sha256:"+expected.Digest {
		return githubArtifact{}, fmt.Errorf("%s artifact digest must be sha256:%s, got %q", expected.Kind, expected.Digest, artifact.Digest)
	}
	expectedRun := evidence.PreflightRun
	if expected.Kind == artifactTagDefense {
		expectedRun = evidence.DefenseRun
	}
	if artifact.WorkflowRun == nil || artifact.WorkflowRun.ID != expectedRun || artifact.WorkflowRun.HeadSHA != evidence.WorkflowSHA {
		return githubArtifact{}, fmt.Errorf("%s artifact workflow lineage does not match run %d at %s", expected.Kind, expectedRun, evidence.WorkflowSHA)
	}
	return artifact, nil
}

func (p *publisher) downloadArtifact(ctx context.Context, expected artifactEvidence, archiveDir string) error {
	path := fmt.Sprintf("/repos/%s/actions/artifacts/%d/zip", p.repository, expected.ID)
	request, err := p.newRequest(ctx, http.MethodGet, p.apiURL+path, nil)
	if err != nil {
		return err
	}
	response, err := p.client.Do(request)
	if err != nil {
		return fmt.Errorf("download %s artifact: %w", expected.Kind, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return responseError(request.Method, path, response)
	}
	archivePath := filepath.Join(archiveDir, expected.assetName())
	file, err := os.OpenFile(archivePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create %s archive without overwrite: %w", expected.Kind, err)
	}
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hasher), io.LimitReader(response.Body, expected.Size+1))
	closeErr := file.Close()
	if copyErr != nil {
		return fmt.Errorf("write %s archive: %w", expected.Kind, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close %s archive: %w", expected.Kind, closeErr)
	}
	if written != expected.Size {
		return fmt.Errorf("%s downloaded archive size must be %d, got %d", expected.Kind, expected.Size, written)
	}
	actualDigest := hex.EncodeToString(hasher.Sum(nil))
	if actualDigest != expected.Digest {
		return fmt.Errorf("%s downloaded archive digest must be %s, got %s", expected.Kind, expected.Digest, actualDigest)
	}
	return nil
}

func validateArchiveDirectory(evidence releaseEvidence, archiveDir string) error {
	for _, kind := range evidence.artifactKinds() {
		artifact := evidence.expectedArtifact(kind)
		path := filepath.Join(archiveDir, artifact.assetName())
		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open staged %s archive: %w", kind, err)
		}
		hasher := sha256.New()
		size, copyErr := io.Copy(hasher, io.LimitReader(file, artifact.Size+1))
		closeErr := file.Close()
		if copyErr != nil {
			return fmt.Errorf("hash staged %s archive: %w", kind, copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close staged %s archive: %w", kind, closeErr)
		}
		if size != artifact.Size {
			return fmt.Errorf("staged %s archive size must be %d, got %d", kind, artifact.Size, size)
		}
		actualDigest := hex.EncodeToString(hasher.Sum(nil))
		if actualDigest != artifact.Digest {
			return fmt.Errorf("staged %s archive digest must be %s, got %s", kind, artifact.Digest, actualDigest)
		}
	}
	return nil
}

func (p *publisher) findRelease(ctx context.Context, tag string) (githubRelease, bool, error) {
	var matching *githubRelease
	for page := 1; page <= maxReleasePages; page++ {
		path := fmt.Sprintf("/repos/%s/releases?per_page=100&page=%d", p.repository, page)
		request, err := p.newRequest(ctx, http.MethodGet, p.apiURL+path, nil)
		if err != nil {
			return githubRelease{}, false, err
		}
		response, err := p.client.Do(request)
		if err != nil {
			return githubRelease{}, false, fmt.Errorf("list GitHub releases: %w", err)
		}
		if response.StatusCode != http.StatusOK {
			defer response.Body.Close()
			return githubRelease{}, false, responseError(request.Method, path, response)
		}
		var releases []githubRelease
		decodeErr := decodeJSON(response.Body, &releases)
		closeErr := response.Body.Close()
		if decodeErr != nil {
			return githubRelease{}, false, fmt.Errorf("decode GitHub releases: %w", decodeErr)
		}
		if closeErr != nil {
			return githubRelease{}, false, fmt.Errorf("close GitHub releases response: %w", closeErr)
		}
		for index := range releases {
			if releases[index].TagName != tag {
				continue
			}
			if matching != nil {
				return githubRelease{}, false, fmt.Errorf("multiple GitHub releases use tag %q", tag)
			}
			copyRelease := releases[index]
			matching = &copyRelease
		}
		if len(releases) < 100 {
			if matching == nil {
				return githubRelease{}, false, nil
			}
			return *matching, true, nil
		}
	}
	return githubRelease{}, false, fmt.Errorf("release search exceeded %d pages", maxReleasePages)
}

func (p *publisher) requireReleaseAbsent(ctx context.Context, tag, phase string) error {
	release, found, err := p.findRelease(ctx, tag)
	if err != nil {
		return err
	}
	if found {
		return fmt.Errorf("GitHub release %d for tag %q already exists %s", release.ID, tag, phase)
	}
	return nil
}

func (p *publisher) createDraftWithConflictRecovery(ctx context.Context, spec releaseSpec) (githubRelease, error) {
	payload := createReleaseRequest{
		TagName:              spec.tag,
		TargetCommitish:      spec.sha,
		Name:                 spec.name,
		Body:                 spec.body,
		Draft:                true,
		Prerelease:           true,
		GenerateReleaseNotes: false,
		MakeLatest:           "false",
	}
	var created githubRelease
	path := "/repos/" + p.repository + "/releases"
	status, err := p.sendJSON(ctx, http.MethodPost, p.apiURL+path, payload, &created)
	if err == nil && status == http.StatusCreated {
		if validateErr := validateReleaseIdentity(created, spec, true); validateErr != nil {
			return githubRelease{}, fmt.Errorf("created draft is inconsistent: %w", validateErr)
		}
		return created, nil
	}
	var responseErr *apiError
	if !errors.As(err, &responseErr) || responseErr.status != http.StatusUnprocessableEntity {
		if err == nil {
			err = fmt.Errorf("create release returned unexpected status %d", status)
		}
		return githubRelease{}, err
	}
	existing, found, fetchErr := p.findRelease(ctx, spec.tag)
	if fetchErr != nil {
		return githubRelease{}, errors.Join(err, fetchErr)
	}
	if !found {
		return githubRelease{}, err
	}
	if existing.Draft {
		if validateErr := validateReleaseIdentity(existing, spec, true); validateErr != nil {
			return githubRelease{}, errors.Join(err, validateErr)
		}
	} else if validateErr := validateReleaseIdentity(existing, spec, false); validateErr != nil {
		return githubRelease{}, errors.Join(err, validateErr)
	}
	return existing, nil
}

func (p *publisher) fetchReleaseByID(ctx context.Context, releaseID int64) (githubRelease, error) {
	path := fmt.Sprintf("/repos/%s/releases/%d", p.repository, releaseID)
	request, err := p.newRequest(ctx, http.MethodGet, p.apiURL+path, nil)
	if err != nil {
		return githubRelease{}, err
	}
	response, err := p.client.Do(request)
	if err != nil {
		return githubRelease{}, fmt.Errorf("fetch GitHub release: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return githubRelease{}, responseError(request.Method, path, response)
	}
	var release githubRelease
	if err := decodeJSON(response.Body, &release); err != nil {
		return githubRelease{}, fmt.Errorf("decode GitHub release: %w", err)
	}
	return release, nil
}

func (p *publisher) ensureDraftAssets(ctx context.Context, release githubRelease, evidence releaseEvidence, archiveDir string) error {
	assets, err := p.listReleaseAssets(ctx, release.ID)
	if err != nil {
		return err
	}
	byName := make(map[string]githubAsset, len(assets))
	allowed := expectedAssetNames(evidence)
	for _, asset := range assets {
		if _, ok := allowed[asset.Name]; !ok {
			return fmt.Errorf("draft release contains unexpected asset %q", asset.Name)
		}
		if _, duplicate := byName[asset.Name]; duplicate {
			return fmt.Errorf("draft release contains duplicate asset %q", asset.Name)
		}
		byName[asset.Name] = asset
	}
	for _, kind := range evidence.artifactKinds() {
		expected := evidence.expectedArtifact(kind)
		if existing, ok := byName[expected.assetName()]; ok {
			if err := validateAsset(existing, expected); err != nil {
				return fmt.Errorf("existing draft asset conflicts: %w", err)
			}
			continue
		}
		if _, err := p.uploadAsset(ctx, release, expected, filepath.Join(archiveDir, expected.assetName())); err != nil {
			return err
		}
	}
	return p.validateExactAssets(ctx, release.ID, evidence)
}

func (p *publisher) uploadAsset(ctx context.Context, release githubRelease, expected artifactEvidence, archivePath string) (githubAsset, error) {
	baseUploadURL := strings.Split(release.UploadURL, "{")[0]
	parsed, err := url.Parse(baseUploadURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return githubAsset{}, fmt.Errorf("invalid release upload URL %q", release.UploadURL)
	}
	query := parsed.Query()
	query.Set("name", expected.assetName())
	parsed.RawQuery = query.Encode()
	file, err := os.Open(archivePath)
	if err != nil {
		return githubAsset{}, fmt.Errorf("open %s release asset: %w", expected.Kind, err)
	}
	defer file.Close()
	request, err := p.newRequest(ctx, http.MethodPost, parsed.String(), file)
	if err != nil {
		return githubAsset{}, err
	}
	request.Header.Set("Content-Type", "application/zip")
	request.ContentLength = expected.Size
	response, err := p.client.Do(request)
	if err != nil {
		return githubAsset{}, fmt.Errorf("upload %s release asset: %w", expected.Kind, err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnprocessableEntity {
		conflictErr := responseError(request.Method, parsed.Path, response)
		assets, listErr := p.listReleaseAssets(ctx, release.ID)
		if listErr != nil {
			return githubAsset{}, errors.Join(conflictErr, listErr)
		}
		for _, asset := range assets {
			if asset.Name == expected.assetName() {
				if validateErr := validateAsset(asset, expected); validateErr != nil {
					return githubAsset{}, errors.Join(conflictErr, validateErr)
				}
				return p.waitForAsset(ctx, asset.ID, expected)
			}
		}
		return githubAsset{}, conflictErr
	}
	if response.StatusCode != http.StatusCreated {
		return githubAsset{}, responseError(request.Method, parsed.Path, response)
	}
	var asset githubAsset
	if err := decodeJSON(response.Body, &asset); err != nil {
		return githubAsset{}, fmt.Errorf("decode uploaded %s asset: %w", expected.Kind, err)
	}
	if asset.ID <= 0 || asset.Name != expected.assetName() {
		return githubAsset{}, fmt.Errorf("uploaded %s asset identity mismatch", expected.Kind)
	}
	return p.waitForAsset(ctx, asset.ID, expected)
}

func (p *publisher) waitForAsset(ctx context.Context, assetID int64, expected artifactEvidence) (githubAsset, error) {
	for attempt := 0; attempt < p.pollAttempts; attempt++ {
		asset, err := p.fetchAssetByID(ctx, assetID)
		if err != nil {
			return githubAsset{}, err
		}
		if asset.Name != expected.assetName() || asset.Size != expected.Size {
			return githubAsset{}, fmt.Errorf("uploaded %s asset identity or size changed", expected.Kind)
		}
		if asset.Digest != "" && asset.Digest != "sha256:"+expected.Digest {
			return githubAsset{}, fmt.Errorf("uploaded %s asset digest conflicts: %q", expected.Kind, asset.Digest)
		}
		if asset.State == "uploaded" && asset.Digest == "sha256:"+expected.Digest {
			return asset, nil
		}
		if attempt+1 < p.pollAttempts {
			if err := p.sleep(ctx, p.pollInterval); err != nil {
				return githubAsset{}, err
			}
		}
	}
	return githubAsset{}, fmt.Errorf("uploaded %s asset did not reach an exact hashed state", expected.Kind)
}

func (p *publisher) fetchAssetByID(ctx context.Context, assetID int64) (githubAsset, error) {
	path := fmt.Sprintf("/repos/%s/releases/assets/%d", p.repository, assetID)
	request, err := p.newRequest(ctx, http.MethodGet, p.apiURL+path, nil)
	if err != nil {
		return githubAsset{}, err
	}
	response, err := p.client.Do(request)
	if err != nil {
		return githubAsset{}, fmt.Errorf("fetch release asset: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return githubAsset{}, responseError(request.Method, path, response)
	}
	var asset githubAsset
	if err := decodeJSON(response.Body, &asset); err != nil {
		return githubAsset{}, fmt.Errorf("decode release asset: %w", err)
	}
	return asset, nil
}

func (p *publisher) listReleaseAssets(ctx context.Context, releaseID int64) ([]githubAsset, error) {
	path := fmt.Sprintf("/repos/%s/releases/%d/assets?per_page=100", p.repository, releaseID)
	request, err := p.newRequest(ctx, http.MethodGet, p.apiURL+path, nil)
	if err != nil {
		return nil, err
	}
	response, err := p.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("list release assets: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, responseError(request.Method, path, response)
	}
	var assets []githubAsset
	if err := decodeJSON(response.Body, &assets); err != nil {
		return nil, fmt.Errorf("decode release assets: %w", err)
	}
	if len(assets) >= 100 {
		return nil, errors.New("release asset list reached the one-page safety limit")
	}
	return assets, nil
}

func (p *publisher) missingDraftArtifacts(ctx context.Context, releaseID int64, evidence releaseEvidence) ([]artifactKind, error) {
	assets, err := p.listReleaseAssets(ctx, releaseID)
	if err != nil {
		return nil, err
	}
	allowed := expectedAssetNames(evidence)
	if len(assets) > len(allowed) {
		return nil, errors.New("draft release contains more assets than expected")
	}
	seen := make(map[string]struct{}, len(assets))
	for _, asset := range assets {
		expected, ok := allowed[asset.Name]
		if !ok {
			return nil, fmt.Errorf("draft release contains unexpected asset %q", asset.Name)
		}
		if _, duplicate := seen[asset.Name]; duplicate {
			return nil, fmt.Errorf("draft release contains duplicate asset %q", asset.Name)
		}
		seen[asset.Name] = struct{}{}
		if err := validateAsset(asset, expected); err != nil {
			return nil, err
		}
	}
	missing := make([]artifactKind, 0, len(allowed)-len(assets))
	for _, kind := range evidence.artifactKinds() {
		if _, ok := seen[evidence.expectedArtifact(kind).assetName()]; !ok {
			missing = append(missing, kind)
		}
	}
	return missing, nil
}

func (p *publisher) validateExactAssets(ctx context.Context, releaseID int64, evidence releaseEvidence) error {
	assets, err := p.listReleaseAssets(ctx, releaseID)
	if err != nil {
		return err
	}
	expected := expectedAssetNames(evidence)
	if len(assets) != len(expected) {
		return fmt.Errorf("release must contain exactly %d evidence assets, got %d", len(expected), len(assets))
	}
	seen := make(map[string]struct{}, len(assets))
	for _, asset := range assets {
		artifact, ok := expected[asset.Name]
		if !ok {
			return fmt.Errorf("release contains unexpected asset %q", asset.Name)
		}
		if _, duplicate := seen[asset.Name]; duplicate {
			return fmt.Errorf("release contains duplicate asset %q", asset.Name)
		}
		seen[asset.Name] = struct{}{}
		if err := validateAsset(asset, artifact); err != nil {
			return err
		}
	}
	return nil
}

func expectedAssetNames(evidence releaseEvidence) map[string]artifactEvidence {
	expected := make(map[string]artifactEvidence, len(evidence.Artifacts))
	for _, kind := range evidence.artifactKinds() {
		artifact := evidence.expectedArtifact(kind)
		expected[artifact.assetName()] = artifact
	}
	return expected
}

func validateAsset(asset githubAsset, expected artifactEvidence) error {
	if asset.ID <= 0 {
		return fmt.Errorf("release asset %q has invalid ID %d", asset.Name, asset.ID)
	}
	if asset.Name != expected.assetName() {
		return fmt.Errorf("release asset name must be %q, got %q", expected.assetName(), asset.Name)
	}
	if asset.State != "uploaded" {
		return fmt.Errorf("release asset %q state must be uploaded, got %q", asset.Name, asset.State)
	}
	if expected.Size > 0 && asset.Size != expected.Size {
		return fmt.Errorf("release asset %q size must be %d, got %d", asset.Name, expected.Size, asset.Size)
	}
	if expected.Size == 0 && asset.Size <= 0 {
		return fmt.Errorf("release asset %q size must be positive, got %d", asset.Name, asset.Size)
	}
	if asset.Digest != "sha256:"+expected.Digest {
		return fmt.Errorf("release asset %q digest must be sha256:%s, got %q", asset.Name, expected.Digest, asset.Digest)
	}
	return nil
}

func (p *publisher) publishDraft(ctx context.Context, releaseID int64, spec releaseSpec) (githubRelease, error) {
	payload := updateReleaseRequest{
		TagName:              spec.tag,
		TargetCommitish:      spec.sha,
		Name:                 spec.name,
		Body:                 spec.body,
		Draft:                false,
		Prerelease:           true,
		GenerateReleaseNotes: false,
		MakeLatest:           "false",
	}
	path := fmt.Sprintf("/repos/%s/releases/%d", p.repository, releaseID)
	var release githubRelease
	status, err := p.sendJSON(ctx, http.MethodPatch, p.apiURL+path, payload, &release)
	if err == nil && status == http.StatusOK {
		if validateErr := validateReleaseIdentity(release, spec, false); validateErr != nil {
			return githubRelease{}, fmt.Errorf("published release is inconsistent: %w", validateErr)
		}
		return release, nil
	}
	var responseErr *apiError
	if errors.As(err, &responseErr) && (responseErr.status == http.StatusConflict || responseErr.status == http.StatusUnprocessableEntity) {
		refetched, fetchErr := p.fetchReleaseByID(ctx, releaseID)
		if fetchErr != nil {
			return githubRelease{}, errors.Join(err, fetchErr)
		}
		if validateErr := validateReleaseIdentity(refetched, spec, false); validateErr == nil {
			return refetched, nil
		}
	}
	if err == nil {
		err = fmt.Errorf("publish release returned unexpected status %d", status)
	}
	return githubRelease{}, err
}

func (p *publisher) waitForImmutableRelease(ctx context.Context, releaseID int64, spec releaseSpec, evidence releaseEvidence) (githubRelease, error) {
	for attempt := 0; attempt < p.pollAttempts; attempt++ {
		release, err := p.fetchReleaseByID(ctx, releaseID)
		if err != nil {
			return githubRelease{}, err
		}
		if err := validateReleaseIdentity(release, spec, false); err != nil {
			return githubRelease{}, err
		}
		if release.Immutable {
			if err := p.validateExactAssets(ctx, release.ID, evidence); err != nil {
				return githubRelease{}, err
			}
			return release, nil
		}
		if attempt+1 < p.pollAttempts {
			if err := p.sleep(ctx, p.pollInterval); err != nil {
				return githubRelease{}, err
			}
		}
	}
	return githubRelease{}, errors.New("published release did not become immutable before the bounded deadline")
}

func (p *publisher) resumePublishedRelease(
	ctx context.Context,
	release githubRelease,
	spec releaseSpec,
	evidence releaseEvidence,
) (githubRelease, error) {
	if err := validateReleaseIdentity(release, spec, false); err != nil {
		return githubRelease{}, fmt.Errorf("published GitHub release is inconsistent: %w", err)
	}
	if err := p.validateExactAssets(ctx, release.ID, evidence); err != nil {
		return githubRelease{}, err
	}
	if release.Immutable {
		return release, nil
	}
	return p.waitForImmutableRelease(ctx, release.ID, spec, evidence)
}

func (p *publisher) waitForImmutablePreflightRelease(
	ctx context.Context,
	releaseID int64,
	spec releaseSpec,
	evidence releaseEvidence,
) (githubRelease, error) {
	for attempt := 0; attempt < p.pollAttempts; attempt++ {
		release, err := p.fetchReleaseByID(ctx, releaseID)
		if err != nil {
			return githubRelease{}, err
		}
		if err := validateReleaseIdentity(release, spec, false); err != nil {
			return githubRelease{}, fmt.Errorf("published GitHub release changed while waiting for immutability: %w", err)
		}
		if release.Immutable {
			if err := p.validatePublishedPreflightRelease(ctx, release, spec, evidence); err != nil {
				return githubRelease{}, err
			}
			return release, nil
		}
		if attempt+1 < p.pollAttempts {
			if err := p.sleep(ctx, p.pollInterval); err != nil {
				return githubRelease{}, err
			}
		}
	}
	return githubRelease{}, errors.New("published preflight release did not become immutable before the bounded deadline")
}

func (p *publisher) validatePublishedPreflightRelease(
	ctx context.Context,
	release githubRelease,
	spec releaseSpec,
	evidence releaseEvidence,
) error {
	if err := validateReleaseIdentity(release, spec, false); err != nil {
		return fmt.Errorf("published GitHub release is inconsistent: %w", err)
	}
	if !release.Immutable {
		return errors.New("published GitHub release is not immutable")
	}
	assets, err := p.listReleaseAssets(ctx, release.ID)
	if err != nil {
		return err
	}
	if len(assets) != len(artifactKinds)+1 {
		return fmt.Errorf("immutable release must contain five preflight assets and one tag-defense asset, got %d", len(assets))
	}
	expected := expectedAssetNames(evidence)
	seenDefense := false
	for _, asset := range assets {
		if artifact, ok := expected[asset.Name]; ok {
			if err := validateAsset(asset, artifact); err != nil {
				return err
			}
			delete(expected, asset.Name)
			continue
		}
		if seenDefense || !strings.HasPrefix(asset.Name, "tag-defense-evidence-") {
			return fmt.Errorf("immutable release contains unexpected asset %q", asset.Name)
		}
		identity, parseErr := parseTagDefenseReleaseAssetName(asset.Name)
		if parseErr != nil {
			return parseErr
		}
		if identity.CandidateSHA != spec.sha {
			return fmt.Errorf("tag-defense release asset %q belongs to a different candidate", asset.Name)
		}
		if asset.Digest != "sha256:"+identity.Digest {
			return fmt.Errorf("tag-defense release asset %q digest does not match its durable filename", asset.Name)
		}
		if identity.Run <= 0 || identity.Attempt <= 0 || identity.ArtifactID <= 0 {
			return errors.New("tag-defense release asset identity must be positive")
		}
		if asset.ID <= 0 || asset.State != "uploaded" || asset.Size <= 0 || !validDigest(identity.Digest) {
			return fmt.Errorf("tag-defense release asset %q is not in a valid immutable state", asset.Name)
		}
		seenDefense = true
	}
	if len(expected) != 0 || !seenDefense {
		return errors.New("immutable release is missing required preflight or tag-defense assets")
	}
	return nil
}

func validateReleaseIdentity(release githubRelease, spec releaseSpec, draft bool) error {
	if release.ID <= 0 {
		return fmt.Errorf("release id must be positive, got %d", release.ID)
	}
	if release.TagName != spec.tag {
		return fmt.Errorf("tag_name must be %q, got %q", spec.tag, release.TagName)
	}
	if release.TargetCommitish != spec.sha {
		return fmt.Errorf("target_commitish must be %q, got %q", spec.sha, release.TargetCommitish)
	}
	if release.Name != spec.name {
		return fmt.Errorf("name must be %q, got %q", spec.name, release.Name)
	}
	if release.Body != spec.body {
		return fmt.Errorf("body digest must be %s, got %s", digest(spec.body), digest(release.Body))
	}
	if release.Draft != draft {
		return fmt.Errorf("draft must be %t", draft)
	}
	if !release.Prerelease {
		return errors.New("prerelease must be true")
	}
	if draft && release.UploadURL == "" {
		return errors.New("draft upload_url is empty")
	}
	if !draft && release.HTMLURL == "" {
		return errors.New("published html_url is empty")
	}
	return nil
}

func (p *publisher) sendJSON(ctx context.Context, method, endpoint string, payload, output any) (int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	request, err := p.newRequest(ctx, method, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return 0, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := p.client.Do(request)
	if err != nil {
		return 0, fmt.Errorf("GitHub API %s request: %w", method, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response.StatusCode, responseError(request.Method, request.URL.Path, response)
	}
	if output != nil {
		if err := decodeJSON(response.Body, output); err != nil {
			return response.StatusCode, err
		}
	}
	return response.StatusCode, nil
}

func (p *publisher) newRequest(ctx context.Context, method, endpoint string, body io.Reader) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Authorization", "Bearer "+p.token)
	request.Header.Set("User-Agent", "genesis-rc2-release-publisher")
	request.Header.Set("X-GitHub-Api-Version", apiVersion)
	return request, nil
}

func decodeJSON(reader io.Reader, output any) error {
	decoder := json.NewDecoder(io.LimitReader(reader, maxJSONBody))
	if err := decoder.Decode(output); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("JSON response contains multiple values")
		}
		return err
	}
	return nil
}

func responseError(method, path string, response *http.Response) error {
	body, err := io.ReadAll(io.LimitReader(response.Body, maxErrorBody))
	if err != nil {
		return fmt.Errorf("read GitHub API error response: %w", err)
	}
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = http.StatusText(response.StatusCode)
	}
	return &apiError{method: method, path: path, status: response.StatusCode, body: message}
}
