// Command publishprerelease stages and publishes the immutable RC2 GitHub
// prerelease after the exact release evidence has passed its protected gates.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	releaseTag             = "v1.0.0-rc.2"
	releaseTitle           = "# Genesis v1.0.0-rc.2 release notes"
	publicationReadyStatus = "Status: publication-ready."
	releaseNotesPath       = "docs/v1-rc2-release-notes.md"
	defaultAPIURL          = "https://api.github.com"
	apiVersion             = "2026-03-10"
	githubHTTPTimeout      = 10 * time.Minute
)

var (
	commitPattern       = regexp.MustCompile(`^[0-9a-f]{40}$`)
	digestPattern       = regexp.MustCompile(`^[0-9a-f]{64}$`)
	repositoryPattern   = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	publicationBlockers = []string{
		"Status: draft, unpublished.",
		"## Pending before tag publication",
	}
	pendingBeforeTagPattern = regexp.MustCompile(`(?mi)^#{1,6}[ \t]+pending[ \t]+before[ \t]+tag[ \t]+publication(?:[ \t].*)?$`)
	statusLinePattern       = regexp.MustCompile(`(?i)^[ \t]*status[ \t]*:`)
)

type releaseSpec struct {
	tag  string
	sha  string
	name string
	body string
}

type commandMode int

const (
	modePublish commandMode = iota
	modeValidateOnly
	modePreTagCheck
	modeStageDraft
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Getenv, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "publishprerelease: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, output io.Writer) error {
	mode, archiveDir, err := parseCommand(args)
	if err != nil {
		return err
	}
	body, err := os.ReadFile(releaseNotesPath)
	if err != nil {
		return fmt.Errorf("read release notes: %w", err)
	}
	spec := releaseSpec{
		tag:  getenv("CANDIDATE_TAG"),
		sha:  getenv("CANDIDATE_SHA"),
		name: releaseTag,
		body: string(body),
	}
	if err := validateSpec(spec); err != nil {
		return err
	}
	if mode == modeValidateOnly {
		_, err := fmt.Fprintf(output, "Release notes are publication-ready for %s at %s\n", spec.tag, spec.sha)
		return err
	}

	apiURL := getenv("GITHUB_API_URL")
	if apiURL == "" {
		apiURL = defaultAPIURL
	}
	p, err := newPublisher(apiURL, getenv("GITHUB_REPOSITORY"), getenv("RELEASE_TOKEN"), &http.Client{Timeout: githubHTTPTimeout})
	if err != nil {
		return err
	}
	if mode == modePreTagCheck {
		evidence, err := preTagEvidenceFromEnvironment(spec, getenv)
		if err != nil {
			return err
		}
		prepared, err := p.prepareArchives(ctx, spec, evidence, archiveDir)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintln(output, "immutable_releases_enabled=true"); err != nil {
			return err
		}
		for _, kind := range artifactKinds {
			if _, err := fmt.Fprintf(output, "%s_artifact_size=%d\n", kind, prepared.Artifacts[kind].Size); err != nil {
				return err
			}
		}
		return nil
	}

	annotation, err := readAndVerifyLocalAnnotatedTag(ctx, ".", spec)
	if err != nil {
		return err
	}
	evidence, err := parseTagEvidence(spec, annotation)
	if err != nil {
		return fmt.Errorf("validate annotated tag evidence: %w", err)
	}
	if mode == modeStageDraft {
		release, err := p.stageDraft(ctx, spec, evidence, archiveDir)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(output, "GitHub prerelease draft staged with exact assets for %s: release_id=%d\n", spec.tag, release.ID)
		return err
	}
	evidence, err = withTagDefenseFromEnvironment(spec, evidence, getenv)
	if err != nil {
		return fmt.Errorf("validate tag-defense evidence: %w", err)
	}

	result, err := p.publish(ctx, spec, evidence)
	if err != nil {
		return err
	}
	action := "verified existing immutable"
	if result.published {
		action = "published immutable"
	}
	_, err = fmt.Fprintf(output, "GitHub prerelease %s %s: %s\n", spec.tag, action, result.release.HTMLURL)
	return err
}

func parseCommand(args []string) (commandMode, string, error) {
	switch {
	case len(args) == 0:
		return modePublish, "", nil
	case len(args) == 1 && args[0] == "--validate-only":
		return modeValidateOnly, "", nil
	case len(args) == 2 && args[0] == "--pre-tag-check" && args[1] != "":
		return modePreTagCheck, args[1], nil
	case len(args) == 2 && args[0] == "--stage-draft" && args[1] != "":
		return modeStageDraft, args[1], nil
	default:
		return modePublish, "", errors.New("usage: publishprerelease [--validate-only | --pre-tag-check <archive-dir> | --stage-draft <archive-dir>]")
	}
}

func validateSpec(spec releaseSpec) error {
	if spec.tag != releaseTag {
		return fmt.Errorf("candidate tag must be %q, got %q", releaseTag, spec.tag)
	}
	if !validCommit(spec.sha) {
		return fmt.Errorf("candidate SHA must be a full lowercase commit SHA, got %q", spec.sha)
	}
	if spec.name != releaseTag {
		return fmt.Errorf("release name must be %q, got %q", releaseTag, spec.name)
	}
	if spec.body == "" {
		return errors.New("release notes are empty")
	}
	if !utf8.ValidString(spec.body) {
		return errors.New("release notes are not valid UTF-8")
	}
	for _, marker := range publicationBlockers {
		if strings.Contains(spec.body, marker) {
			return fmt.Errorf("release notes still contain publication blocker %q", marker)
		}
	}
	if pendingBeforeTagPattern.MatchString(spec.body) {
		return errors.New("release notes still contain a pending-before-tag publication section")
	}
	lines := strings.Split(spec.body, "\n")
	if lines[0] != releaseTitle || countExactLine(spec.body, releaseTitle) != 1 {
		return fmt.Errorf("release notes must start with exactly one %q heading", releaseTitle)
	}
	statusLines := 0
	for _, line := range lines {
		if !statusLinePattern.MatchString(line) {
			continue
		}
		statusLines++
		if line != publicationReadyStatus {
			return fmt.Errorf("release notes contain a non-publication-ready status line %q", line)
		}
	}
	if statusLines != 1 {
		return fmt.Errorf("release notes must contain exactly one %q status line", publicationReadyStatus)
	}
	return nil
}

func countExactLine(body, expected string) int {
	count := 0
	for line := range strings.SplitSeq(body, "\n") {
		if line == expected {
			count++
		}
	}
	return count
}

func verifyLocalAnnotatedTag(ctx context.Context, repositoryRoot string, spec releaseSpec) error {
	_, err := readAndVerifyLocalAnnotatedTag(ctx, repositoryRoot, spec)
	return err
}

func readAndVerifyLocalAnnotatedTag(ctx context.Context, repositoryRoot string, spec releaseSpec) (string, error) {
	ref := "refs/tags/" + spec.tag
	objectType, err := gitOutput(ctx, repositoryRoot, "cat-file", "-t", ref)
	if err != nil {
		return "", fmt.Errorf("inspect local tag object: %w", err)
	}
	if objectType != "tag" {
		return "", fmt.Errorf("local %s must be an annotated tag object, got %q", ref, objectType)
	}
	peeledCommit, err := gitOutput(ctx, repositoryRoot, "rev-list", "-n", "1", ref)
	if err != nil {
		return "", fmt.Errorf("peel local tag: %w", err)
	}
	if peeledCommit != spec.sha {
		return "", fmt.Errorf("local %s must peel to %s, got %s", ref, spec.sha, peeledCommit)
	}
	annotation, err := gitOutput(ctx, repositoryRoot, "for-each-ref", "--format=%(contents)", ref)
	if err != nil {
		return "", fmt.Errorf("read local tag annotation: %w", err)
	}
	return annotation, nil
}

func gitOutput(ctx context.Context, repositoryRoot string, args ...string) (string, error) {
	commandArgs := append([]string{"-C", repositoryRoot}, args...)
	output, err := exec.CommandContext(ctx, "git", commandArgs...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func validCommit(value string) bool {
	return commitPattern.MatchString(value) && value != strings.Repeat("0", 40)
}

func validDigest(value string) bool {
	return digestPattern.MatchString(value) && value != strings.Repeat("0", 64)
}
