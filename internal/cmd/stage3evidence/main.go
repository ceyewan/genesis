// Command stage3evidence validates the immutable Resonance Stage 3 handoff
// that gates Genesis v1.0.0-rc.2 publication.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	manifestRelativePath = "docs/verification/evidence/genesis-v1.0.0-rc.2-stage3.json"
	manifestSchema       = "genesis-stage3-evidence/v1"
	passStatus           = "PASS"
	maxManifestBytes     = 1 << 20

	genesisModule    = "github.com/ceyewan/genesis"
	genesisRC1       = "v1.0.0-rc.1"
	genesisRC1Sum    = "h1:X3VK5VpPxIrgyzQsPPPSHQHaiNvMhhT/wcGCWkuFS8U="
	genesisRC1ModSum = "h1:VUPsG33Toz8lKJk2tEkgeWd7SFMIDjYtwvzYOuQmRU4="
)

var (
	commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
	digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

type manifest struct {
	SchemaVersion      string          `json:"schema_version"`
	Status             string          `json:"status"`
	TestedAt           string          `json:"tested_at"`
	TestedResonanceSHA string          `json:"tested_resonance_sha"`
	GenesisRC1         moduleIdentity  `json:"genesis_rc1"`
	Inputs             immutableInputs `json:"inputs"`
	Checks             stage3Checks    `json:"checks"`
}

type moduleIdentity struct {
	Version  string `json:"version"`
	Sum      string `json:"sum"`
	GoModSum string `json:"go_mod_sum"`
}

type immutableInputs struct {
	ComposeSHA256     string `json:"compose_sha256"`
	ApplicationImage  string `json:"application_image"`
	PilotControlImage string `json:"pilot_control_image"`
	PilotRuntimeImage string `json:"pilot_runtime_image"`
}

type stage3Checks struct {
	Compose   checkEvidence `json:"compose"`
	IM        checkEvidence `json:"im"`
	Agent     checkEvidence `json:"agent"`
	Recovery  checkEvidence `json:"recovery"`
	Telemetry checkEvidence `json:"telemetry"`
	Benchmark checkEvidence `json:"benchmark"`
}

type checkEvidence struct {
	Status         string `json:"status"`
	CompletedAt    string `json:"completed_at"`
	EvidenceSHA256 string `json:"evidence_sha256"`
}

type validationResult struct {
	manifestPath string
	sha256       string
	testedSHA    string
	testedAt     string
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, time.Now); err != nil {
		fmt.Fprintf(os.Stderr, "stage3evidence: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, output io.Writer, now func() time.Time) error {
	flags := flag.NewFlagSet("stage3evidence", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	repository := flags.String("repo", "", "exact Resonance checkout root")
	consumerSHA := flags.String("consumer-sha", "", "full Resonance consumer commit SHA")
	if err := flags.Parse(args); err != nil {
		return usageError(err)
	}
	if flags.NArg() != 0 || *repository == "" || *consumerSHA == "" {
		return usageError(nil)
	}
	if now == nil {
		return errors.New("clock is nil")
	}

	result, err := validateHandoff(ctx, *repository, *consumerSHA, now().UTC())
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(
		output,
		"stage3_manifest_path=%s\nstage3_manifest_sha256=%s\nstage3_tested_resonance_sha=%s\nstage3_tested_at=%s\n",
		result.manifestPath,
		result.sha256,
		result.testedSHA,
		result.testedAt,
	)
	if err != nil {
		return fmt.Errorf("write validation result: %w", err)
	}
	return nil
}

func usageError(cause error) error {
	const usage = "usage: stage3evidence --repo <resonance-checkout> --consumer-sha <40-lowercase-hex>"
	if cause == nil {
		return errors.New(usage)
	}
	return fmt.Errorf("%s: %w", usage, cause)
}

func validateHandoff(ctx context.Context, repository, consumerSHA string, now time.Time) (validationResult, error) {
	if err := validateCommitSHA("consumer SHA", consumerSHA); err != nil {
		return validationResult{}, err
	}
	repositoryRoot, err := validateRepositoryRoot(ctx, repository)
	if err != nil {
		return validationResult{}, err
	}
	data, err := readManifest(repositoryRoot)
	if err != nil {
		return validationResult{}, err
	}
	parsed, err := decodeManifest(data)
	if err != nil {
		return validationResult{}, err
	}
	if err := validateManifest(parsed, now); err != nil {
		return validationResult{}, err
	}
	if err := validateRepositoryState(ctx, repositoryRoot, consumerSHA, parsed.TestedResonanceSHA); err != nil {
		return validationResult{}, err
	}
	committedData, err := gitBytes(ctx, repositoryRoot, "show", consumerSHA+":"+manifestRelativePath)
	if err != nil {
		return validationResult{}, fmt.Errorf("read committed Stage 3 manifest: %w", err)
	}
	if !bytes.Equal(data, committedData) {
		return validationResult{}, errors.New("fixed Stage 3 manifest bytes differ from the exact consumer commit")
	}
	if err := validateCommittedModuleIdentity(ctx, repositoryRoot, parsed.TestedResonanceSHA); err != nil {
		return validationResult{}, err
	}

	digest := sha256.Sum256(committedData)
	return validationResult{
		manifestPath: manifestRelativePath,
		sha256:       hex.EncodeToString(digest[:]),
		testedSHA:    parsed.TestedResonanceSHA,
		testedAt:     parsed.TestedAt,
	}, nil
}

func validateRepositoryRoot(ctx context.Context, repository string) (string, error) {
	absRepository, err := filepath.Abs(repository)
	if err != nil {
		return "", fmt.Errorf("resolve repository path: %w", err)
	}
	absRepository, err = filepath.EvalSymlinks(absRepository)
	if err != nil {
		return "", fmt.Errorf("resolve repository symlinks: %w", err)
	}
	root, err := gitOutput(ctx, absRepository, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("locate Resonance repository root: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve Git repository root: %w", err)
	}
	if filepath.Clean(absRepository) != filepath.Clean(root) {
		return "", fmt.Errorf("--repo must name the exact Resonance repository root %q, got %q", root, absRepository)
	}
	return root, nil
}

func readManifest(repositoryRoot string) ([]byte, error) {
	path := filepath.Join(repositoryRoot, filepath.FromSlash(manifestRelativePath))
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect fixed Stage 3 manifest %s: %w", manifestRelativePath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("fixed Stage 3 manifest %s must be a regular non-symlink file", manifestRelativePath)
	}
	if info.Size() > maxManifestBytes {
		return nil, fmt.Errorf("fixed Stage 3 manifest %s exceeds %d bytes", manifestRelativePath, maxManifestBytes)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open fixed Stage 3 manifest %s: %w", manifestRelativePath, err)
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxManifestBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read fixed Stage 3 manifest %s: %w", manifestRelativePath, err)
	}
	if len(data) > maxManifestBytes {
		return nil, fmt.Errorf("fixed Stage 3 manifest %s exceeds %d bytes", manifestRelativePath, maxManifestBytes)
	}
	return data, nil
}

func decodeManifest(data []byte) (manifest, error) {
	if len(data) == 0 {
		return manifest{}, errors.New("stage 3 manifest is empty")
	}
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return manifest{}, fmt.Errorf("decode Stage 3 manifest: %w", err)
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var parsed manifest
	if err := decoder.Decode(&parsed); err != nil {
		return manifest{}, fmt.Errorf("decode Stage 3 manifest: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return manifest{}, fmt.Errorf("decode Stage 3 manifest: %w", err)
	}
	return parsed, nil
}

func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := inspectJSONValue(decoder); err != nil {
		return err
	}
	return requireJSONEOF(decoder)
}

func inspectJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("object key is not a string")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate JSON key %q", key)
			}
			seen[key] = struct{}{}
			if err := inspectJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if err := inspectJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing any
	err := decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.New("multiple JSON values are not allowed")
}

func validateManifest(value manifest, now time.Time) error {
	if value.SchemaVersion != manifestSchema {
		return fmt.Errorf("schema_version must be %q, got %q", manifestSchema, value.SchemaVersion)
	}
	if value.Status != passStatus {
		return fmt.Errorf("manifest status must be %q, got %q", passStatus, value.Status)
	}
	if err := validateCommitSHA("tested_resonance_sha", value.TestedResonanceSHA); err != nil {
		return err
	}
	testedAt, err := validateTimestamp("tested_at", value.TestedAt)
	if err != nil {
		return err
	}
	if testedAt.After(now.Add(5 * time.Minute)) {
		return fmt.Errorf("tested_at %q is more than five minutes in the future", value.TestedAt)
	}
	if value.GenesisRC1.Version != genesisRC1 || value.GenesisRC1.Sum != genesisRC1Sum || value.GenesisRC1.GoModSum != genesisRC1ModSum {
		return fmt.Errorf(
			"genesis_rc1 must be the published identity Version=%q Sum=%q GoModSum=%q",
			genesisRC1,
			genesisRC1Sum,
			genesisRC1ModSum,
		)
	}
	if err := validateDigest("inputs.compose_sha256", value.Inputs.ComposeSHA256); err != nil {
		return err
	}
	images := []struct {
		name  string
		value string
	}{
		{name: "inputs.application_image", value: value.Inputs.ApplicationImage},
		{name: "inputs.pilot_control_image", value: value.Inputs.PilotControlImage},
		{name: "inputs.pilot_runtime_image", value: value.Inputs.PilotRuntimeImage},
	}
	for _, image := range images {
		if err := validateImageReference(image.name, image.value); err != nil {
			return err
		}
	}

	checks := []struct {
		name  string
		value checkEvidence
	}{
		{name: "compose", value: value.Checks.Compose},
		{name: "im", value: value.Checks.IM},
		{name: "agent", value: value.Checks.Agent},
		{name: "recovery", value: value.Checks.Recovery},
		{name: "telemetry", value: value.Checks.Telemetry},
		{name: "benchmark", value: value.Checks.Benchmark},
	}
	for _, check := range checks {
		if err := validateCheck(check.name, check.value, testedAt); err != nil {
			return err
		}
	}
	return nil
}

func validateCheck(name string, value checkEvidence, testedAt time.Time) error {
	if value.Status != passStatus {
		return fmt.Errorf("checks.%s.status must be %q, got %q", name, passStatus, value.Status)
	}
	completedAt, err := validateTimestamp("checks."+name+".completed_at", value.CompletedAt)
	if err != nil {
		return err
	}
	if completedAt.After(testedAt) {
		return fmt.Errorf("checks.%s.completed_at must not be after tested_at", name)
	}
	return validateDigest("checks."+name+".evidence_sha256", value.EvidenceSHA256)
}

func validateTimestamp(name, value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be an RFC3339 timestamp: %w", name, err)
	}
	if parsed.IsZero() {
		return time.Time{}, fmt.Errorf("%s must be non-zero", name)
	}
	_, offset := parsed.Zone()
	if offset != 0 || parsed.Format(time.RFC3339Nano) != value {
		return time.Time{}, fmt.Errorf("%s must be a canonical UTC RFC3339 timestamp ending in Z", name)
	}
	return parsed, nil
}

func validateCommitSHA(name, value string) error {
	if !commitPattern.MatchString(value) || allZeroHex(value) {
		return fmt.Errorf("%s must be a non-zero full lowercase 40-hex commit SHA, got %q", name, value)
	}
	return nil
}

func validateDigest(name, value string) error {
	if !digestPattern.MatchString(value) || allZeroHex(strings.TrimPrefix(value, "sha256:")) {
		return fmt.Errorf("%s must be a non-zero lowercase sha256 digest, got %q", name, value)
	}
	return nil
}

func validateImageReference(name, value string) error {
	if strings.Count(value, "@") != 1 {
		return fmt.Errorf("%s must be an immutable name@sha256:digest reference, got %q", name, value)
	}
	imageName, digest, _ := strings.Cut(value, "@")
	if strings.TrimSpace(imageName) != imageName || imageName == "" || strings.ContainsAny(imageName, "\t\r\n ") {
		return fmt.Errorf("%s has an invalid image name in %q", name, value)
	}
	if err := validateDigest(name, digest); err != nil {
		return fmt.Errorf("%s must be an immutable name@sha256:digest reference: %w", name, err)
	}
	return nil
}

func allZeroHex(value string) bool {
	return strings.Trim(value, "0") == ""
}

func validateRepositoryState(ctx context.Context, repositoryRoot, consumerSHA, testedSHA string) error {
	resolvedConsumer, err := resolveCommit(ctx, repositoryRoot, consumerSHA)
	if err != nil {
		return fmt.Errorf("resolve consumer SHA: %w", err)
	}
	if resolvedConsumer != consumerSHA {
		return fmt.Errorf("consumer SHA resolved to %s, want exact %s", resolvedConsumer, consumerSHA)
	}
	resolvedTested, err := resolveCommit(ctx, repositoryRoot, testedSHA)
	if err != nil {
		return fmt.Errorf("resolve tested Resonance SHA: %w", err)
	}
	if resolvedTested != testedSHA {
		return fmt.Errorf("tested Resonance SHA resolved to %s, want exact %s", resolvedTested, testedSHA)
	}
	head, err := gitOutput(ctx, repositoryRoot, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return fmt.Errorf("resolve checked-out HEAD: %w", err)
	}
	if head != consumerSHA {
		return fmt.Errorf("resonance checkout HEAD is %s, want exact consumer SHA %s", head, consumerSHA)
	}
	status, err := gitOutput(ctx, repositoryRoot, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return fmt.Errorf("inspect Resonance checkout: %w", err)
	}
	if status != "" {
		return fmt.Errorf("resonance checkout must be clean, got status %q", status)
	}
	if _, err := gitOutput(ctx, repositoryRoot, "merge-base", "--is-ancestor", testedSHA, consumerSHA); err != nil {
		return fmt.Errorf("tested Resonance SHA %s is not an ancestor of consumer SHA %s: %w", testedSHA, consumerSHA, err)
	}
	diff, err := gitOutput(ctx, repositoryRoot, "diff", "--name-status", "--no-renames", testedSHA, consumerSHA, "--")
	if err != nil {
		return fmt.Errorf("inspect tested-to-consumer diff: %w", err)
	}
	expected := "A\t" + manifestRelativePath
	if diff != expected {
		return fmt.Errorf("tested-to-consumer diff must add only %s, got %q", manifestRelativePath, diff)
	}
	return nil
}

func resolveCommit(ctx context.Context, repositoryRoot, sha string) (string, error) {
	return gitOutput(ctx, repositoryRoot, "rev-parse", "--verify", sha+"^{commit}")
}

func validateCommittedModuleIdentity(ctx context.Context, repositoryRoot, testedSHA string) error {
	goMod, err := gitOutput(ctx, repositoryRoot, "show", testedSHA+":go.mod")
	if err != nil {
		return fmt.Errorf("read tested go.mod: %w", err)
	}
	if err := validateGoModIdentity(goMod); err != nil {
		return fmt.Errorf("tested go.mod Genesis identity: %w", err)
	}
	goSum, err := gitOutput(ctx, repositoryRoot, "show", testedSHA+":go.sum")
	if err != nil {
		return fmt.Errorf("read tested go.sum: %w", err)
	}
	if err := validateGoSumIdentity(goSum); err != nil {
		return fmt.Errorf("tested go.sum Genesis identity: %w", err)
	}
	return nil
}

func validateGoModIdentity(contents string) error {
	requireVersions := make([]string, 0, 1)
	block := ""
	for line := range strings.SplitSeq(contents, "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "//", 2)[0])
		if line == "" {
			continue
		}
		if line == ")" {
			block = ""
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == "(" {
			block = fields[0]
			continue
		}
		if block == "replace" || (len(fields) > 0 && fields[0] == "replace") {
			if slicesContain(fields, genesisModule) {
				return errors.New("replace directive involving Genesis is not allowed")
			}
			continue
		}
		if block == "require" && len(fields) >= 2 && fields[0] == genesisModule {
			requireVersions = append(requireVersions, fields[1])
			continue
		}
		if block == "" && len(fields) >= 3 && fields[0] == "require" && fields[1] == genesisModule {
			requireVersions = append(requireVersions, fields[2])
		}
	}
	if len(requireVersions) != 1 || requireVersions[0] != genesisRC1 {
		return fmt.Errorf("require must select exactly %s@%s, got %v", genesisModule, genesisRC1, requireVersions)
	}
	return nil
}

func slicesContain(values []string, expected string) bool {
	return slices.Contains(values, expected)
}

func validateGoSumIdentity(contents string) error {
	want := map[string]string{
		genesisRC1:             genesisRC1Sum,
		genesisRC1 + "/go.mod": genesisRC1ModSum,
	}
	counts := make(map[string]int, len(want))
	for line := range strings.SplitSeq(contents, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != genesisModule {
			continue
		}
		expected, tracked := want[fields[1]]
		if !tracked {
			continue
		}
		counts[fields[1]]++
		if len(fields) != 3 {
			return fmt.Errorf("malformed %s %s checksum entry", genesisModule, fields[1])
		}
		if fields[2] != expected {
			return fmt.Errorf("conflicting %s %s checksum %q, want %q", genesisModule, fields[1], fields[2], expected)
		}
	}
	for version := range want {
		if counts[version] != 1 {
			return fmt.Errorf("expected exactly one %s %s checksum, got %d", genesisModule, version, counts[version])
		}
	}
	return nil
}

func gitOutput(ctx context.Context, repositoryRoot string, args ...string) (string, error) {
	output, err := gitBytes(ctx, repositoryRoot, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func gitBytes(ctx context.Context, repositoryRoot string, args ...string) ([]byte, error) {
	commandArgs := append([]string{"-C", repositoryRoot}, args...)
	output, err := exec.CommandContext(ctx, "git", commandArgs...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}
