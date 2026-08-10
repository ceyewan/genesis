package main

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	stage3ManifestPath = "docs/verification/evidence/genesis-v1.0.0-rc.2-stage3.json"
)

type artifactKind string

const (
	artifactNormal          artifactKind = "normal"
	artifactRace            artifactKind = "race"
	artifactAPI             artifactKind = "api"
	artifactConsumer        artifactKind = "consumer"
	artifactReleaseEvidence artifactKind = "evidence"
	artifactTagDefense      artifactKind = "defense"
)

var artifactKinds = []artifactKind{
	artifactNormal,
	artifactRace,
	artifactAPI,
	artifactConsumer,
	artifactReleaseEvidence,
}

var tagDefenseReleaseAssetPattern = regexp.MustCompile(
	`^tag-defense-evidence-([0-9a-f]{40})-([1-9][0-9]*)-([1-9][0-9]*)-artifact-([1-9][0-9]*)-sha256-([0-9a-f]{64})\.zip$`,
)

type artifactEvidence struct {
	Kind    artifactKind
	ID      int64
	Name    string
	Digest  string
	Attempt int64
	Size    int64
}

func (a artifactEvidence) assetName() string {
	if a.Kind == artifactTagDefense {
		return fmt.Sprintf("%s-artifact-%d-sha256-%s.zip", a.Name, a.ID, a.Digest)
	}
	return a.Name + ".zip"
}

type tagDefenseReleaseAssetIdentity struct {
	CandidateSHA string
	Run          int64
	Attempt      int64
	ArtifactID   int64
	Digest       string
}

func parseTagDefenseReleaseAssetName(name string) (tagDefenseReleaseAssetIdentity, error) {
	matches := tagDefenseReleaseAssetPattern.FindStringSubmatch(name)
	if matches == nil {
		return tagDefenseReleaseAssetIdentity{}, fmt.Errorf("invalid tag-defense release asset name %q", name)
	}
	run, err := parsePositiveInt64("tag-defense release asset run", matches[2])
	if err != nil {
		return tagDefenseReleaseAssetIdentity{}, err
	}
	attempt, err := parsePositiveInt64("tag-defense release asset attempt", matches[3])
	if err != nil {
		return tagDefenseReleaseAssetIdentity{}, err
	}
	artifactID, err := parsePositiveInt64("tag-defense release asset Actions ID", matches[4])
	if err != nil {
		return tagDefenseReleaseAssetIdentity{}, err
	}
	return tagDefenseReleaseAssetIdentity{
		CandidateSHA: matches[1],
		Run:          run,
		Attempt:      attempt,
		ArtifactID:   artifactID,
		Digest:       matches[5],
	}, nil
}

type stage3Evidence struct {
	ManifestPath   string
	ManifestDigest string
	TestedSHA      string
	TestedAt       string
}

type releaseEvidence struct {
	PreflightRun     int64
	PreflightAttempt int64
	DefenseRun       int64
	WorkflowSHA      string
	ResonanceSHA     string
	Stage3           stage3Evidence
	Artifacts        map[artifactKind]artifactEvidence
	CanonicalDefense *artifactEvidence
}

func (e releaseEvidence) artifactKinds() []artifactKind {
	kinds := append([]artifactKind(nil), artifactKinds...)
	if _, ok := e.Artifacts[artifactTagDefense]; ok {
		kinds = append(kinds, artifactTagDefense)
	}
	return kinds
}

func (e releaseEvidence) expectedArtifact(kind artifactKind) artifactEvidence {
	if kind == artifactTagDefense && e.CanonicalDefense != nil {
		return *e.CanonicalDefense
	}
	return e.Artifacts[kind]
}

func preTagEvidenceFromEnvironment(spec releaseSpec, getenv func(string) string) (releaseEvidence, error) {
	runID, err := parsePositiveInt64("GITHUB_RUN_ID", getenv("GITHUB_RUN_ID"))
	if err != nil {
		return releaseEvidence{}, err
	}
	evidence := releaseEvidence{
		PreflightRun: runID,
		WorkflowSHA:  getenv("GITHUB_WORKFLOW_SHA"),
		ResonanceSHA: getenv("RESONANCE_SHA"),
		Stage3: stage3Evidence{
			ManifestPath:   getenv("STAGE3_MANIFEST_PATH"),
			ManifestDigest: getenv("STAGE3_MANIFEST_SHA256"),
			TestedSHA:      getenv("STAGE3_TESTED_RESONANCE_SHA"),
			TestedAt:       getenv("STAGE3_TESTED_AT"),
		},
		Artifacts: make(map[artifactKind]artifactEvidence, len(artifactKinds)),
	}
	for _, kind := range artifactKinds {
		prefix := strings.ToUpper(string(kind))
		id, parseErr := parsePositiveInt64(prefix+"_ID", getenv(prefix+"_ID"))
		if parseErr != nil {
			return releaseEvidence{}, parseErr
		}
		attempt, parseErr := parsePositiveInt64(prefix+"_ATTEMPT", getenv(prefix+"_ATTEMPT"))
		if parseErr != nil {
			return releaseEvidence{}, parseErr
		}
		evidence.Artifacts[kind] = artifactEvidence{
			Kind:    kind,
			ID:      id,
			Name:    getenv(prefix + "_NAME"),
			Digest:  getenv(prefix + "_DIGEST"),
			Attempt: attempt,
		}
	}
	evidence.PreflightAttempt = evidence.Artifacts[artifactReleaseEvidence].Attempt
	if err := validateReleaseEvidence(spec, evidence, false); err != nil {
		return releaseEvidence{}, err
	}
	return evidence, nil
}

func parseTagEvidence(spec releaseSpec, annotation string) (releaseEvidence, error) {
	values := make(map[string]string)
	for line := range strings.SplitSeq(annotation, "\n") {
		key, value, recognized := annotationField(line)
		if !recognized {
			continue
		}
		if _, duplicate := values[key]; duplicate {
			return releaseEvidence{}, fmt.Errorf("duplicate tag annotation field %q", key)
		}
		values[key] = value
	}

	required := []string{
		"Resonance-SHA",
		"Stage3-Manifest-Path",
		"Stage3-Manifest-SHA256",
		"Stage3-Tested-Resonance-SHA",
		"Stage3-Tested-At",
		"Resonance-Main-Tip-At-Publish",
		"Resonance-Main-Checked-At",
		"Preflight-Run",
		"Preflight-Attempt",
		"Workflow-SHA",
	}
	for _, kind := range artifactKinds {
		prefix := string(kind) + "_artifact_"
		required = append(required, prefix+"sha256", prefix+"id", prefix+"name", prefix+"attempt", prefix+"size")
	}
	for _, key := range required {
		if values[key] == "" {
			return releaseEvidence{}, fmt.Errorf("missing tag annotation field %q", key)
		}
	}

	runID, err := parsePositiveInt64("Preflight-Run", values["Preflight-Run"])
	if err != nil {
		return releaseEvidence{}, err
	}
	preflightAttempt, err := parsePositiveInt64("Preflight-Attempt", values["Preflight-Attempt"])
	if err != nil {
		return releaseEvidence{}, err
	}
	evidence := releaseEvidence{
		PreflightRun:     runID,
		PreflightAttempt: preflightAttempt,
		WorkflowSHA:      values["Workflow-SHA"],
		ResonanceSHA:     values["Resonance-SHA"],
		Stage3: stage3Evidence{
			ManifestPath:   values["Stage3-Manifest-Path"],
			ManifestDigest: values["Stage3-Manifest-SHA256"],
			TestedSHA:      values["Stage3-Tested-Resonance-SHA"],
			TestedAt:       values["Stage3-Tested-At"],
		},
		Artifacts: make(map[artifactKind]artifactEvidence, len(artifactKinds)),
	}
	if values["Resonance-Main-Tip-At-Publish"] != evidence.ResonanceSHA {
		return releaseEvidence{}, errors.New("Resonance-Main-Tip-At-Publish does not equal Resonance-SHA")
	}
	if !validCanonicalUTCTimestamp(values["Resonance-Main-Checked-At"]) {
		return releaseEvidence{}, fmt.Errorf("invalid Resonance-Main-Checked-At %q", values["Resonance-Main-Checked-At"])
	}

	for _, kind := range artifactKinds {
		prefix := string(kind) + "_artifact_"
		id, parseErr := parsePositiveInt64(prefix+"id", values[prefix+"id"])
		if parseErr != nil {
			return releaseEvidence{}, parseErr
		}
		attempt, parseErr := parsePositiveInt64(prefix+"attempt", values[prefix+"attempt"])
		if parseErr != nil {
			return releaseEvidence{}, parseErr
		}
		size, parseErr := parsePositiveInt64(prefix+"size", values[prefix+"size"])
		if parseErr != nil {
			return releaseEvidence{}, parseErr
		}
		evidence.Artifacts[kind] = artifactEvidence{
			Kind:    kind,
			ID:      id,
			Name:    values[prefix+"name"],
			Digest:  values[prefix+"sha256"],
			Attempt: attempt,
			Size:    size,
		}
	}
	if err := validateReleaseEvidence(spec, evidence, true); err != nil {
		return releaseEvidence{}, err
	}
	return evidence, nil
}

func withTagDefenseFromEnvironment(spec releaseSpec, evidence releaseEvidence, getenv func(string) string) (releaseEvidence, error) {
	runID, err := parsePositiveInt64("GITHUB_RUN_ID", getenv("GITHUB_RUN_ID"))
	if err != nil {
		return releaseEvidence{}, err
	}
	id, err := parsePositiveInt64("DEFENSE_ID", getenv("DEFENSE_ID"))
	if err != nil {
		return releaseEvidence{}, err
	}
	attempt, err := parsePositiveInt64("DEFENSE_ATTEMPT", getenv("DEFENSE_ATTEMPT"))
	if err != nil {
		return releaseEvidence{}, err
	}
	evidence.DefenseRun = runID
	evidence.Artifacts[artifactTagDefense] = artifactEvidence{
		Kind:    artifactTagDefense,
		ID:      id,
		Name:    getenv("DEFENSE_NAME"),
		Digest:  getenv("DEFENSE_DIGEST"),
		Attempt: attempt,
	}
	if err := validateFinalReleaseEvidence(spec, evidence); err != nil {
		return releaseEvidence{}, err
	}
	return evidence, nil
}

func annotationField(line string) (string, string, bool) {
	colonFields := []string{
		"Resonance-SHA",
		"Stage3-Manifest-Path",
		"Stage3-Manifest-SHA256",
		"Stage3-Tested-Resonance-SHA",
		"Stage3-Tested-At",
		"Resonance-Main-Tip-At-Publish",
		"Resonance-Main-Checked-At",
		"Preflight-Run",
		"Preflight-Attempt",
		"Workflow-SHA",
	}
	for _, key := range colonFields {
		prefix := key + ": "
		if after, ok := strings.CutPrefix(line, prefix); ok {
			return key, after, true
		}
		if strings.HasPrefix(strings.TrimLeft(line, " \t"), key) {
			return key, "", true
		}
	}
	for _, kind := range artifactKinds {
		for _, suffix := range []string{"sha256", "id", "name", "attempt", "size"} {
			key := string(kind) + "_artifact_" + suffix
			prefix := key + "="
			if after, ok := strings.CutPrefix(line, prefix); ok {
				return key, after, true
			}
			if strings.HasPrefix(strings.TrimLeft(line, " \t"), key) {
				return key, "", true
			}
		}
	}
	return "", "", false
}

func validateReleaseEvidence(spec releaseSpec, evidence releaseEvidence, requireSize bool) error {
	if evidence.PreflightRun <= 0 {
		return errors.New("preflight run must be positive")
	}
	if evidence.PreflightAttempt <= 0 {
		return errors.New("preflight attempt must be positive")
	}
	if evidence.WorkflowSHA != spec.sha {
		return fmt.Errorf("workflow SHA must be candidate SHA %s, got %q", spec.sha, evidence.WorkflowSHA)
	}
	if !validCommit(evidence.ResonanceSHA) {
		return fmt.Errorf("invalid Resonance SHA %q", evidence.ResonanceSHA)
	}
	if evidence.Stage3.ManifestPath != stage3ManifestPath {
		return fmt.Errorf("stage 3 manifest path must be %q, got %q", stage3ManifestPath, evidence.Stage3.ManifestPath)
	}
	if !validDigest(evidence.Stage3.ManifestDigest) {
		return fmt.Errorf("invalid Stage 3 manifest digest %q", evidence.Stage3.ManifestDigest)
	}
	if !validCommit(evidence.Stage3.TestedSHA) {
		return fmt.Errorf("invalid Stage 3 tested Resonance SHA %q", evidence.Stage3.TestedSHA)
	}
	if !validCanonicalUTCTimestamp(evidence.Stage3.TestedAt) {
		return fmt.Errorf("invalid Stage 3 tested_at %q", evidence.Stage3.TestedAt)
	}

	seenIDs := make(map[int64]artifactKind, len(artifactKinds))
	seenNames := make(map[string]artifactKind, len(artifactKinds))
	for _, kind := range artifactKinds {
		artifact, ok := evidence.Artifacts[kind]
		if !ok {
			return fmt.Errorf("missing %s artifact evidence", kind)
		}
		if artifact.Kind != kind {
			return fmt.Errorf("%s artifact has mismatched kind %q", kind, artifact.Kind)
		}
		if artifact.ID <= 0 || artifact.Attempt <= 0 {
			return fmt.Errorf("%s artifact ID and attempt must be positive", kind)
		}
		if !validDigest(artifact.Digest) {
			return fmt.Errorf("invalid %s artifact digest %q", kind, artifact.Digest)
		}
		if requireSize && artifact.Size <= 0 {
			return fmt.Errorf("%s artifact size must be positive", kind)
		}
		expectedName := expectedArtifactName(kind, spec.sha, artifact.Attempt)
		if artifact.Name != expectedName {
			return fmt.Errorf("%s artifact name must be %q, got %q", kind, expectedName, artifact.Name)
		}
		if previous, duplicate := seenIDs[artifact.ID]; duplicate {
			return fmt.Errorf("%s and %s artifacts share ID %d", previous, kind, artifact.ID)
		}
		seenIDs[artifact.ID] = kind
		if previous, duplicate := seenNames[artifact.Name]; duplicate {
			return fmt.Errorf("%s and %s artifacts share name %q", previous, kind, artifact.Name)
		}
		seenNames[artifact.Name] = kind
	}
	if evidence.Artifacts[artifactReleaseEvidence].Attempt != evidence.PreflightAttempt {
		return errors.New("preflight attempt does not equal release-evidence artifact attempt")
	}
	return nil
}

func validateFinalReleaseEvidence(spec releaseSpec, evidence releaseEvidence) error {
	if err := validateReleaseEvidence(spec, evidence, true); err != nil {
		return err
	}
	if evidence.DefenseRun <= 0 {
		return errors.New("tag-defense run must be positive")
	}
	defense, ok := evidence.Artifacts[artifactTagDefense]
	if !ok {
		return errors.New("missing tag-defense artifact evidence")
	}
	if defense.Kind != artifactTagDefense || defense.ID <= 0 || defense.Attempt <= 0 {
		return errors.New("tag-defense artifact identity must be positive")
	}
	if !validDigest(defense.Digest) {
		return fmt.Errorf("invalid tag-defense artifact digest %q", defense.Digest)
	}
	expectedName := fmt.Sprintf("tag-defense-evidence-%s-%d-%d", spec.sha, evidence.DefenseRun, defense.Attempt)
	if defense.Name != expectedName {
		return fmt.Errorf("tag-defense artifact name must be %q, got %q", expectedName, defense.Name)
	}
	for _, kind := range artifactKinds {
		artifact := evidence.Artifacts[kind]
		if artifact.ID == defense.ID {
			return fmt.Errorf("%s and tag-defense artifacts share ID %d", kind, defense.ID)
		}
		if artifact.Name == defense.Name {
			return fmt.Errorf("%s and tag-defense artifacts share name %q", kind, defense.Name)
		}
	}
	return nil
}

func expectedArtifactName(kind artifactKind, candidateSHA string, attempt int64) string {
	prefix := map[artifactKind]string{
		artifactNormal:          "testcontainers-normal",
		artifactRace:            "testcontainers-race",
		artifactAPI:             "v1-api-godoc",
		artifactConsumer:        "resonance-consumer",
		artifactReleaseEvidence: "release-evidence",
	}[kind]
	return fmt.Sprintf("%s-%s-%d", prefix, candidateSHA, attempt)
}

func parsePositiveInt64(name, value string) (int64, error) {
	if value == "" || value[0] == '0' || strings.ContainsAny(value, "+-") {
		return 0, fmt.Errorf("%s must be a positive decimal integer, got %q", name, value)
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive decimal integer, got %q", name, value)
	}
	return parsed, nil
}

func validCanonicalUTCTimestamp(value string) bool {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return false
	}
	_, offset := parsed.Zone()
	return offset == 0 && strings.HasSuffix(value, "Z") && parsed.Format(time.RFC3339Nano) == value
}
