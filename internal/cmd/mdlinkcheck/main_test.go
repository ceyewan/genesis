package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckRepositorySupportsLocalMarkdownLinkForms(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "README.md", `# Home

[same page](#home)
[directory README](guide/#install)
[directory listing](empty/)
[URL decoded](space%20document.md#encoded-heading)
[inline code heading](space%20document.md#using-config)
[details][details-ref]
[collapsed][]
[shortcut]
[external](https://example.com/not-fetched)

[details-ref]: docs/details.md#details
[collapsed]: <docs/details.md#details>
[shortcut]: docs/details.md#details
`)
	writeTestFile(t, root, "guide/README.md", "# Install\n")
	if err := os.Mkdir(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, root, "space document.md", "# Encoded Heading\n\n## Using `Config`\n")
	writeTestFile(t, root, "docs/details.md", "# Details\n")

	stats, problems, err := checkRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Fatalf("problems = %+v", problems)
	}
	if stats.files != 4 {
		t.Fatalf("files = %d, want 4", stats.files)
	}
	if stats.localLinks != 8 {
		t.Fatalf("local links = %d, want 8", stats.localLinks)
	}
}

func TestCheckRepositoryReportsBrokenPathsAnchorsReferencesAndDirectories(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "README.md", `# Home

[missing](missing.md)
[bad anchor](target.md#absent)
[directory fragment without README](empty/#missing)
[outside](../outside.md)
[bad encoding](bad%ZZ.md)
[missing reference][not-defined]
`)
	writeTestFile(t, root, "target.md", "# Present\n")
	if err := os.Mkdir(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, problems, err := checkRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	joined := problemMessages(problems)
	for _, want := range []string{
		"target does not exist",
		"no matching Markdown anchor",
		"directory link",
		"escapes the repository",
		"invalid URL escape",
		"has no definition",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("problems %q do not contain %q", joined, want)
		}
	}
}

func TestCheckRepositoryUnderstandsDuplicateHeadingSlugsAndExplicitAnchors(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "README.md", `[first](target.md#repeat)
[second](target.md#repeat-1)
[explicit](target.md#stable-id)
[emoji](target.md#-components)
`)
	writeTestFile(t, root, "target.md", `# Repeat
# Repeat
<a id="stable-id"></a>
# 🚀 Components
`)

	_, problems, err := checkRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Fatalf("problems = %+v", problems)
	}
}

func TestCheckRepositoryReportsWrongPathCase(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "README.md", "[wrong case](Target.md)\n")
	writeTestFile(t, root, "target.md", "# Target\n")

	_, problems, err := checkRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	if joined := problemMessages(problems); !strings.Contains(joined, "wrong case") {
		t.Fatalf("problems %q do not report wrong path case", joined)
	}
}

func TestCheckRepositoryIgnoresLinksInsideCodeAndSymlinkedMarkdown(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "README.md", "`[inline](missing.md)`\n\n```text\n[fenced](missing.md)\n```\n")
	if err := os.Symlink("README.md", filepath.Join(root, "ALIAS.md")); err != nil {
		t.Fatal(err)
	}

	stats, problems, err := checkRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Fatalf("problems = %+v", problems)
	}
	if stats.files != 1 {
		t.Fatalf("files = %d, want symlink to be skipped", stats.files)
	}
}

func writeTestFile(t *testing.T, root, relative, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func problemMessages(problems []problem) string {
	var messages []string
	for _, item := range problems {
		messages = append(messages, item.message)
	}
	return strings.Join(messages, "\n")
}
