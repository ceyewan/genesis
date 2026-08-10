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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	testNow       = "2026-08-09T02:00:00Z"
	testTestedAt  = "2026-08-09T01:30:00Z"
	testCompleted = "2026-08-09T01:00:00Z"
)

type testRepository struct {
	root        string
	testedSHA   string
	consumerSHA string
	data        []byte
}

func TestRunValidHandoffOutputsWorkflowContract(t *testing.T) {
	repository := createTestRepository(t, genesisRC1, genesisRC1Sum, genesisRC1ModSum)
	now := mustTime(t, testNow)
	var output bytes.Buffer

	err := run(
		context.Background(),
		[]string{"--repo", repository.root, "--consumer-sha", repository.consumerSHA},
		&output,
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(repository.data)
	want := fmt.Sprintf(
		"stage3_manifest_path=%s\nstage3_manifest_sha256=%s\nstage3_tested_resonance_sha=%s\nstage3_tested_at=%s\n",
		manifestRelativePath,
		hex.EncodeToString(digest[:]),
		repository.testedSHA,
		testTestedAt,
	)
	if output.String() != want {
		t.Fatalf("output = %q, want %q", output.String(), want)
	}
}

func TestRunRejectsInvalidInvocationWithoutOutput(t *testing.T) {
	for name, args := range map[string][]string{
		"missing arguments": nil,
		"missing repo":      {"--consumer-sha", strings.Repeat("1", 40)},
		"extra argument":    {"--repo", ".", "--consumer-sha", strings.Repeat("1", 40), "extra"},
		"unknown flag":      {"--unknown"},
	} {
		t.Run(name, func(t *testing.T) {
			var output bytes.Buffer
			err := run(context.Background(), args, &output, time.Now)
			if err == nil || !strings.Contains(err.Error(), "usage: stage3evidence") {
				t.Fatalf("error = %v, want usage error", err)
			}
			if output.Len() != 0 {
				t.Fatalf("output = %q, want empty", output.String())
			}
		})
	}
}

func TestDecodeManifestRejectsUnknownFields(t *testing.T) {
	sha := strings.Repeat("1", 40)
	data := string(marshalCompactManifest(t, validManifest(sha)))
	tests := map[string]string{
		"top level": strings.Replace(data, `"schema_version"`, `"unknown":true,"schema_version"`, 1),
		"module":    strings.Replace(data, `"version"`, `"unknown":true,"version"`, 1),
		"inputs":    strings.Replace(data, `"compose_sha256"`, `"unknown":true,"compose_sha256"`, 1),
		"checks":    strings.Replace(data, `"compose":{"status"`, `"unknown":{},"compose":{"status"`, 1),
		"check":     strings.Replace(data, `"compose":{"status"`, `"compose":{"unknown":true,"status"`, 1),
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := decodeManifest([]byte(input))
			if err == nil || !strings.Contains(err.Error(), "unknown field") {
				t.Fatalf("error = %v, want unknown-field rejection", err)
			}
		})
	}
}

func TestDecodeManifestRejectsAmbiguousOrTrailingJSON(t *testing.T) {
	data := string(marshalCompactManifest(t, validManifest(strings.Repeat("1", 40))))
	tests := map[string]string{
		"duplicate key":  strings.Replace(data, `"schema_version":`, `"schema_version":"duplicate","schema_version":`, 1),
		"trailing value": data + `{}`,
		"malformed":      strings.TrimSuffix(strings.TrimSpace(data), "}"),
		"empty":          "",
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeManifest([]byte(input)); err == nil {
				t.Fatal("decodeManifest succeeded")
			}
		})
	}
}

func TestValidateManifestRejectsInvalidIdentityAndInputs(t *testing.T) {
	sha := strings.Repeat("1", 40)
	now := mustTime(t, testNow)
	tests := map[string]func(*manifest){
		"schema":                    func(value *manifest) { value.SchemaVersion = "v2" },
		"status":                    func(value *manifest) { value.Status = "PENDING" },
		"short tested SHA":          func(value *manifest) { value.TestedResonanceSHA = "1234" },
		"uppercase tested SHA":      func(value *manifest) { value.TestedResonanceSHA = strings.Repeat("A", 40) },
		"zero tested SHA":           func(value *manifest) { value.TestedResonanceSHA = strings.Repeat("0", 40) },
		"invalid tested time":       func(value *manifest) { value.TestedAt = "not-a-time" },
		"zero tested time":          func(value *manifest) { value.TestedAt = time.Time{}.Format(time.RFC3339Nano) },
		"non-UTC tested time":       func(value *manifest) { value.TestedAt = "2026-08-09T09:30:00+08:00" },
		"future tested time":        func(value *manifest) { value.TestedAt = "2026-08-09T03:00:00Z" },
		"RC1 version":               func(value *manifest) { value.GenesisRC1.Version = "v1.0.0-rc.2" },
		"RC1 sum":                   func(value *manifest) { value.GenesisRC1.Sum = "h1:wrong" },
		"RC1 go.mod sum":            func(value *manifest) { value.GenesisRC1.GoModSum = "h1:wrong" },
		"compose digest prefix":     func(value *manifest) { value.Inputs.ComposeSHA256 = strings.Repeat("a", 64) },
		"compose zero digest":       func(value *manifest) { value.Inputs.ComposeSHA256 = "sha256:" + strings.Repeat("0", 64) },
		"mutable application image": func(value *manifest) { value.Inputs.ApplicationImage = "resonance:latest" },
		"empty image name":          func(value *manifest) { value.Inputs.PilotControlImage = "@sha256:" + strings.Repeat("b", 64) },
		"image whitespace": func(value *manifest) {
			value.Inputs.PilotRuntimeImage = "pilot runtime@sha256:" + strings.Repeat("c", 64)
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			value := validManifest(sha)
			mutate(&value)
			if err := validateManifest(value, now); err == nil {
				t.Fatal("validateManifest succeeded")
			}
		})
	}
}

func TestValidateManifestRequiresAllSixPassingChecks(t *testing.T) {
	sha := strings.Repeat("1", 40)
	now := mustTime(t, testNow)
	tests := map[string]func(*manifest){
		"compose":   func(value *manifest) { value.Checks.Compose.Status = "FAIL" },
		"im":        func(value *manifest) { value.Checks.IM.Status = "PENDING" },
		"agent":     func(value *manifest) { value.Checks.Agent.Status = "" },
		"recovery":  func(value *manifest) { value.Checks.Recovery.Status = "FAIL" },
		"telemetry": func(value *manifest) { value.Checks.Telemetry.Status = "SKIP" },
		"benchmark": func(value *manifest) { value.Checks.Benchmark.Status = "WARN" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			value := validManifest(sha)
			mutate(&value)
			err := validateManifest(value, now)
			if err == nil || !strings.Contains(err.Error(), "checks."+name+".status") {
				t.Fatalf("error = %v, want %s status rejection", err, name)
			}
		})
	}
}

func TestValidateManifestRequiresBoundedCheckTimeAndDigest(t *testing.T) {
	sha := strings.Repeat("1", 40)
	now := mustTime(t, testNow)
	tests := map[string]func(*manifest){
		"invalid timestamp": func(value *manifest) { value.Checks.IM.CompletedAt = "invalid" },
		"after tested_at":   func(value *manifest) { value.Checks.Agent.CompletedAt = "2026-08-09T01:31:00Z" },
		"zero digest":       func(value *manifest) { value.Checks.Recovery.EvidenceSHA256 = "sha256:" + strings.Repeat("0", 64) },
		"uppercase digest":  func(value *manifest) { value.Checks.Telemetry.EvidenceSHA256 = "sha256:" + strings.Repeat("A", 64) },
		"missing digest":    func(value *manifest) { value.Checks.Benchmark.EvidenceSHA256 = "" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			value := validManifest(sha)
			mutate(&value)
			if err := validateManifest(value, now); err == nil {
				t.Fatal("validateManifest succeeded")
			}
		})
	}
}

func TestRepositoryValidationRequiresExactCleanConsumerCheckout(t *testing.T) {
	t.Run("HEAD mismatch", func(t *testing.T) {
		repository := createTestRepository(t, genesisRC1, genesisRC1Sum, genesisRC1ModSum)
		err := validateRepositoryState(context.Background(), repository.root, repository.testedSHA, repository.testedSHA)
		if err == nil || !strings.Contains(err.Error(), "checkout HEAD") {
			t.Fatalf("error = %v, want exact checkout rejection", err)
		}
	})

	t.Run("dirty checkout", func(t *testing.T) {
		repository := createTestRepository(t, genesisRC1, genesisRC1Sum, genesisRC1ModSum)
		writeTestFile(t, repository.root, "untracked.txt", "dirty\n")
		err := validateRepositoryState(context.Background(), repository.root, repository.consumerSHA, repository.testedSHA)
		if err == nil || !strings.Contains(err.Error(), "must be clean") {
			t.Fatalf("error = %v, want dirty checkout rejection", err)
		}
	})

	t.Run("repo must be root", func(t *testing.T) {
		repository := createTestRepository(t, genesisRC1, genesisRC1Sum, genesisRC1ModSum)
		subdirectory := filepath.Join(repository.root, "docs")
		_, err := validateRepositoryRoot(context.Background(), subdirectory)
		if err == nil || !strings.Contains(err.Error(), "exact Resonance repository root") {
			t.Fatalf("error = %v, want exact root rejection", err)
		}
	})
}

func TestRepositoryValidationRequiresEvidenceOnlyAddition(t *testing.T) {
	t.Run("extra changed path", func(t *testing.T) {
		repository := createTestRepository(t, genesisRC1, genesisRC1Sum, genesisRC1ModSum)
		writeTestFile(t, repository.root, "extra.txt", "not evidence only\n")
		git(t, repository.root, "add", "extra.txt")
		git(t, repository.root, "commit", "-m", "add extra path")
		consumerSHA := git(t, repository.root, "rev-parse", "HEAD")
		err := validateRepositoryState(context.Background(), repository.root, consumerSHA, repository.testedSHA)
		if err == nil || !strings.Contains(err.Error(), "must add only") {
			t.Fatalf("error = %v, want extra-path rejection", err)
		}
	})

	t.Run("manifest modification instead of addition", func(t *testing.T) {
		repository := createTestRepository(t, genesisRC1, genesisRC1Sum, genesisRC1ModSum)
		value := validManifest(repository.consumerSHA)
		writeTestBytes(t, repository.root, manifestRelativePath, marshalManifest(t, value))
		git(t, repository.root, "add", manifestRelativePath)
		git(t, repository.root, "commit", "-m", "modify existing evidence")
		consumerSHA := git(t, repository.root, "rev-parse", "HEAD")
		err := validateRepositoryState(context.Background(), repository.root, consumerSHA, repository.consumerSHA)
		if err == nil || !strings.Contains(err.Error(), "must add only") {
			t.Fatalf("error = %v, want modified-manifest rejection", err)
		}
	})
}

func TestRepositoryValidationRequiresTestedAncestor(t *testing.T) {
	repository := createTestRepository(t, genesisRC1, genesisRC1Sum, genesisRC1ModSum)
	mainSHA := repository.consumerSHA
	git(t, repository.root, "checkout", "-b", "unrelated", repository.testedSHA)
	writeTestFile(t, repository.root, "unrelated.txt", "sibling\n")
	git(t, repository.root, "add", "unrelated.txt")
	git(t, repository.root, "commit", "-m", "sibling commit")
	siblingSHA := git(t, repository.root, "rev-parse", "HEAD")
	git(t, repository.root, "checkout", "--detach", mainSHA)

	err := validateRepositoryState(context.Background(), repository.root, mainSHA, siblingSHA)
	if err == nil || !strings.Contains(err.Error(), "is not an ancestor") {
		t.Fatalf("error = %v, want ancestor rejection", err)
	}
}

func TestValidateHandoffRejectsNonRegularManifest(t *testing.T) {
	repository := createTestRepository(t, genesisRC1, genesisRC1Sum, genesisRC1ModSum)
	manifestPath := filepath.Join(repository.root, filepath.FromSlash(manifestRelativePath))
	targetPath := filepath.Join(repository.root, "manifest-target.json")
	writeTestBytes(t, repository.root, "manifest-target.json", repository.data)
	if err := os.Remove(manifestPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(targetPath, manifestPath); err != nil {
		t.Fatal(err)
	}

	_, err := validateHandoff(context.Background(), repository.root, repository.consumerSHA, mustTime(t, testNow))
	if err == nil || !strings.Contains(err.Error(), "regular non-symlink") {
		t.Fatalf("error = %v, want symlink rejection", err)
	}
}

func TestValidateHandoffChecksCommittedRC1ModuleIdentity(t *testing.T) {
	tests := map[string]struct {
		version string
		sum     string
		modSum  string
	}{
		"version": {version: "v1.0.0-rc.0", sum: genesisRC1Sum, modSum: genesisRC1ModSum},
		"sum":     {version: genesisRC1, sum: "h1:wrong", modSum: genesisRC1ModSum},
		"mod sum": {version: genesisRC1, sum: genesisRC1Sum, modSum: "h1:wrong"},
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			repository := createTestRepository(t, input.version, input.sum, input.modSum)
			_, err := validateHandoff(context.Background(), repository.root, repository.consumerSHA, mustTime(t, testNow))
			if err == nil || !strings.Contains(err.Error(), "tested go.") {
				t.Fatalf("error = %v, want committed module identity rejection", err)
			}
		})
	}
}

func TestValidateGoModIdentityRejectsGenesisReplace(t *testing.T) {
	contents := "module example.com/resonance\n\ngo 1.26\n\nrequire " + genesisModule + " " + genesisRC1 + "\n\nreplace " + genesisModule + " => ../genesis\n"
	if err := validateGoModIdentity(contents); err == nil || !strings.Contains(err.Error(), "replace") {
		t.Fatalf("error = %v, want replace rejection", err)
	}
}

func TestValidateGoSumIdentityRejectsDuplicateOrConflictingEntries(t *testing.T) {
	moduleLine := genesisModule + " " + genesisRC1 + " " + genesisRC1Sum + "\n"
	modLine := genesisModule + " " + genesisRC1 + "/go.mod " + genesisRC1ModSum + "\n"
	tests := map[string]string{
		"duplicate module sum":   moduleLine + moduleLine + modLine,
		"duplicate go.mod sum":   moduleLine + modLine + modLine,
		"conflicting module sum": moduleLine + genesisModule + " " + genesisRC1 + " h1:wrong\n" + modLine,
		"conflicting go.mod sum": moduleLine + modLine + genesisModule + " " + genesisRC1 + "/go.mod h1:wrong\n",
		"malformed duplicate":    moduleLine + genesisModule + " " + genesisRC1 + " " + genesisRC1Sum + " extra\n" + modLine,
	}
	for name, contents := range tests {
		t.Run(name, func(t *testing.T) {
			if err := validateGoSumIdentity(contents); err == nil {
				t.Fatal("validateGoSumIdentity succeeded")
			}
		})
	}
}

func TestReadManifestRejectsOversizedFile(t *testing.T) {
	root := t.TempDir()
	writeTestBytes(t, root, manifestRelativePath, bytes.Repeat([]byte("x"), maxManifestBytes+1))
	if _, err := readManifest(root); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("error = %v, want size rejection", err)
	}
}

func TestRunPropagatesOutputFailure(t *testing.T) {
	repository := createTestRepository(t, genesisRC1, genesisRC1Sum, genesisRC1ModSum)
	err := run(
		context.Background(),
		[]string{"--repo", repository.root, "--consumer-sha", repository.consumerSHA},
		failingWriter{},
		func() time.Time { return mustTime(t, testNow) },
	)
	if err == nil || !strings.Contains(err.Error(), "write validation result") {
		t.Fatalf("error = %v, want output error", err)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func validManifest(testedSHA string) manifest {
	evidence := checkEvidence{
		Status:         passStatus,
		CompletedAt:    testCompleted,
		EvidenceSHA256: "sha256:" + strings.Repeat("d", 64),
	}
	return manifest{
		SchemaVersion:      manifestSchema,
		Status:             passStatus,
		TestedAt:           testTestedAt,
		TestedResonanceSHA: testedSHA,
		GenesisRC1: moduleIdentity{
			Version:  genesisRC1,
			Sum:      genesisRC1Sum,
			GoModSum: genesisRC1ModSum,
		},
		Inputs: immutableInputs{
			ComposeSHA256:     "sha256:" + strings.Repeat("a", 64),
			ApplicationImage:  "ghcr.io/example/resonance@sha256:" + strings.Repeat("1", 64),
			PilotControlImage: "ghcr.io/example/pilot-control@sha256:" + strings.Repeat("2", 64),
			PilotRuntimeImage: "ghcr.io/example/pilot-runtime@sha256:" + strings.Repeat("3", 64),
		},
		Checks: stage3Checks{
			Compose:   evidence,
			IM:        evidence,
			Agent:     evidence,
			Recovery:  evidence,
			Telemetry: evidence,
			Benchmark: evidence,
		},
	}
}

func createTestRepository(t *testing.T, version, sum, modSum string) testRepository {
	t.Helper()
	root := t.TempDir()
	git(t, root, "init", "--initial-branch=main")
	git(t, root, "config", "user.name", "Stage3 Test")
	git(t, root, "config", "user.email", "stage3@example.invalid")
	git(t, root, "config", "commit.gpgsign", "false")
	goMod := "module example.com/resonance\n\ngo 1.26\n\nrequire " + genesisModule + " " + version + "\n"
	goSum := genesisModule + " " + genesisRC1 + " " + sum + "\n" +
		genesisModule + " " + genesisRC1 + "/go.mod " + modSum + "\n"
	writeTestFile(t, root, "go.mod", goMod)
	writeTestFile(t, root, "go.sum", goSum)
	writeTestFile(t, root, "source.txt", "tested source\n")
	git(t, root, "add", "go.mod", "go.sum", "source.txt")
	git(t, root, "commit", "-m", "tested Stage 3 source")
	testedSHA := git(t, root, "rev-parse", "HEAD")

	data := marshalManifest(t, validManifest(testedSHA))
	writeTestBytes(t, root, manifestRelativePath, data)
	git(t, root, "add", manifestRelativePath)
	git(t, root, "commit", "-m", "add Stage 3 handoff evidence")
	consumerSHA := git(t, root, "rev-parse", "HEAD")
	return testRepository{root: root, testedSHA: testedSHA, consumerSHA: consumerSHA, data: data}
}

func marshalManifest(t *testing.T, value manifest) []byte {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}

func marshalCompactManifest(t *testing.T, value manifest) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func writeTestFile(t *testing.T, root, relativePath, contents string) {
	t.Helper()
	writeTestBytes(t, root, relativePath, []byte(contents))
}

func writeTestBytes(t *testing.T, root, relativePath string, contents []byte) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, root string, args ...string) string {
	t.Helper()
	commandArgs := append([]string{"-C", root}, args...)
	output, err := exec.Command("git", commandArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

var _ io.Writer = failingWriter{}
