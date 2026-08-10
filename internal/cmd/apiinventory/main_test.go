package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestInventoryHeaderDescribesTestkitImportability(t *testing.T) {
	module := &packages.Module{Path: "example.com/module"}
	withoutTestkit := inventoryHeader([]*packages.Package{{
		PkgPath: "example.com/module/trace",
		Module:  module,
	}})
	if strings.Contains(withoutTestkit, "`testkit` is included") {
		t.Fatalf("internalized testkit is still described as public: %s", withoutTestkit)
	}

	withTestkit := inventoryHeader([]*packages.Package{{
		PkgPath: "example.com/module/testkit",
		Module:  module,
	}})
	if !strings.Contains(withTestkit, "`testkit` is included because it is an importable module package") {
		t.Fatalf("public baseline testkit explanation is missing: %s", withTestkit)
	}
}

func TestPackageEntriesCanonicalPublicSurface(t *testing.T) {
	first := packageEntriesForSource(t, `
package surface

type Embedded struct{}

func (Embedded) Promoted(value int) error { return nil }
func (*Embedded) PointerOnly(label string) {}

type GenericEmbedded[T any] struct{}

func (GenericEmbedded[T]) Value(input T) T { return input }

type Surface struct {
	Public int `+"`json:\"public\"`"+`
	hiddenA string
	Embedded
	Callback func(input string) (output bool)
}

type GenericSurface struct {
	GenericEmbedded[string]
}

func (Surface) Own(param string) (result int) { return 0 }
func Exported(first int, second string) (result bool) { return false }

type Contract interface {
	Call(named int) (result string)
}
`)
	second := packageEntriesForSource(t, `
package surface

type Embedded struct{}

func (Embedded) Promoted(number int) error { return nil }
func (*Embedded) PointerOnly(text string) {}

type GenericEmbedded[T any] struct{}

func (GenericEmbedded[T]) Value(value T) T { return value }

type Surface struct {
	Public int `+"`json:\"public\"`"+`
	privateRenamed string
	Embedded
	Callback func(value string) (ok bool)
}

type GenericSurface struct {
	GenericEmbedded[string]
}

func (Surface) Own(value string) (number int) { return 0 }
func Exported(number int, text string) (ok bool) { return false }

type Contract interface {
	Call(value int) (text string)
}
`)

	if !slices.Equal(first, second) {
		t.Fatalf("parameter or private field names changed the inventory:\nfirst:\n%s\nsecond:\n%s", strings.Join(first, "\n"), strings.Join(second, "\n"))
	}

	want := []string{
		"func: `func Exported(int, string) bool`",
		"method: `func (*Surface).PointerOnly(string)`",
		"method: `func (GenericEmbedded[T]).Value(T) T`",
		"method: `func (GenericSurface).Value(string) string`",
		"method: `func (Surface).Own(string) int`",
		"method: `func (Surface).Promoted(int) error`",
		"type: `type Contract interface{Call(int) string}`",
		"type: `type Surface struct{Public int \"json:\\\"public\\\"\"; Embedded; Callback func(string) bool; /* unexported fields; comparable=false */}`",
	}
	for _, entry := range want {
		if !slices.Contains(first, entry) {
			t.Errorf("missing canonical entry %q\nentries:\n%s", entry, strings.Join(first, "\n"))
		}
	}
	joined := strings.Join(first, "\n")
	for _, forbidden := range []string{"hiddenA", "privateRenamed", "first int", "second string", "result bool", "param string", "named int"} {
		if strings.Contains(joined, forbidden) {
			t.Errorf("inventory contains non-API name %q:\n%s", forbidden, joined)
		}
	}
	if got := packageEntriesForSource(t, `package stable; func Exported(name string) {}`); !slices.IsSorted(got) {
		t.Fatalf("package entries are not sorted: %v", got)
	}
}

func TestPackageEntriesRecordsComparabilityWithoutPrivateFields(t *testing.T) {
	comparable := strings.Join(packageEntriesForSource(t, `
package surface
type Surface struct {
	Public string
	hidden int
}
`), "\n")
	nonComparable := strings.Join(packageEntriesForSource(t, `
package surface
type Surface struct {
	Public string
	privateRenamed map[string]int
}
`), "\n")

	if !strings.Contains(comparable, "/* unexported fields; comparable=true */") {
		t.Fatalf("comparable struct marker missing: %s", comparable)
	}
	if !strings.Contains(nonComparable, "/* unexported fields; comparable=false */") {
		t.Fatalf("non-comparable struct marker missing: %s", nonComparable)
	}
	for _, privateDetail := range []string{"hidden", "privateRenamed", "map[string]int"} {
		if strings.Contains(comparable+nonComparable, privateDetail) {
			t.Fatalf("private field detail %q leaked into inventory:\n%s\n%s", privateDetail, comparable, nonComparable)
		}
	}
	if comparable == nonComparable {
		t.Fatal("private-field comparability change did not change the inventory")
	}
}

func TestGenerateIncludesPromotedJWTRegisteredClaimsMethods(t *testing.T) {
	moduleDir, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := generate(moduleDir, false)
	if err != nil {
		t.Fatalf("generate module inventory: %v", err)
	}

	want := []string{
		"- method: `func (Claims).GetAudience() (github.com/golang-jwt/jwt/v5.ClaimStrings, error)`",
		"- method: `func (Claims).GetExpirationTime() (*github.com/golang-jwt/jwt/v5.NumericDate, error)`",
		"- method: `func (Claims).GetIssuedAt() (*github.com/golang-jwt/jwt/v5.NumericDate, error)`",
		"- method: `func (Claims).GetIssuer() (string, error)`",
		"- method: `func (Claims).GetNotBefore() (*github.com/golang-jwt/jwt/v5.NumericDate, error)`",
		"- method: `func (Claims).GetSubject() (string, error)`",
	}
	for _, entry := range want {
		if !bytes.Contains(inventory, []byte(entry)) {
			t.Errorf("generated inventory is missing promoted jwt method %q", entry)
		}
	}
}

func TestGenerateRejectsImportableExamplePackageOutsideInternal(t *testing.T) {
	moduleDir := t.TempDir()
	writeModuleFile := func(name, content string) {
		t.Helper()
		path := filepath.Join(moduleDir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeModuleFile("go.mod", "module example.com/surface\n\ngo 1.26\n")
	writeModuleFile("stable/stable.go", "package stable\nfunc Exported() {}\n")
	writeModuleFile("examples/demo/main.go", "package main\nfunc main() {}\n")
	writeModuleFile("examples/demo/internal/proto/proto.go", "package proto\ntype Internal struct{}\n")

	if _, err := generate(moduleDir, false); err != nil {
		t.Fatalf("main and internal example packages should be accepted: %v", err)
	}

	writeModuleFile("examples/demo/proto/proto.go", "package proto\ntype Public struct{}\n")
	_, err := generate(moduleDir, false)
	if err == nil || !strings.Contains(err.Error(), "importable example package must move under internal") {
		t.Fatalf("public example package was not rejected: %v", err)
	}
	if _, err := generate(moduleDir, true); err != nil {
		t.Fatalf("explicit frozen-baseline mode rejected a legacy example package: %v", err)
	}
}

func TestCheckCompatibilityRequiresStrictReplacementPair(t *testing.T) {
	baseline := []byte("## `pkg`\n\n- func: `func Change(int) string`\n- func: `func Keep()`\n")
	current := []byte("## `pkg`\n\n- func: `func Change(string) string`\n- func: `func Keep()`\n")
	dir := t.TempDir()
	baselinePath := writeTestFile(t, dir, "baseline.md", baseline)
	allowPath := writeTestFile(t, dir, "allow.md", []byte("## `pkg`\n\n- func: `func Change(int) string`\n"))
	expectedPath := writeTestFile(t, dir, "expected.md", []byte("## `pkg`\n\n- func: `func Change(string) string`\n"))

	if err := checkCompatibility(current, baselinePath, "", "", ""); err == nil {
		t.Fatal("unreviewed signature change passed compatibility check")
	}
	if err := checkCompatibility(current, baselinePath, allowPath, expectedPath, ""); err != nil {
		t.Fatalf("strict reviewed replacement failed compatibility check: %v", err)
	}
}

func TestCheckCompatibilityRejectsMismatchedPair(t *testing.T) {
	baseline := []byte("## `pkg`\n\n- func: `func Change(int)`\n")
	current := []byte("## `pkg`\n\n- func: `func Renamed(string)`\n")
	dir := t.TempDir()
	baselinePath := writeTestFile(t, dir, "baseline.md", baseline)
	allowPath := writeTestFile(t, dir, "allow.md", baseline)
	expectedPath := writeTestFile(t, dir, "expected.md", current)

	err := checkCompatibility(current, baselinePath, allowPath, expectedPath, "")
	if err == nil || !strings.Contains(err.Error(), "missing expected replacement") {
		t.Fatalf("mismatched one-entry old/new sets were not rejected as an invalid pair: %v", err)
	}
}

func TestCheckCompatibilityRejectsExpectedReplacementDrift(t *testing.T) {
	baseline := []byte("## `pkg`\n\n- func: `func Change(int)`\n")
	allow := baseline
	expected := []byte("## `pkg`\n\n- func: `func Change(string)`\n")
	dir := t.TempDir()
	baselinePath := writeTestFile(t, dir, "baseline.md", baseline)
	allowPath := writeTestFile(t, dir, "allow.md", allow)
	expectedPath := writeTestFile(t, dir, "expected.md", expected)

	tests := map[string][]byte{
		"changed again": []byte("## `pkg`\n\n- func: `func Change(bool)`\n"),
		"deleted":       []byte("## `pkg`\n"),
	}
	for name, current := range tests {
		t.Run(name, func(t *testing.T) {
			err := checkCompatibility(current, baselinePath, allowPath, expectedPath, "")
			if err == nil || !strings.Contains(err.Error(), "approved replacement is not current") {
				t.Fatalf("replacement drift passed compatibility check: %v", err)
			}
		})
	}
}

func TestCheckCompatibilityRejectsStaleAndUnpairedExceptions(t *testing.T) {
	inventory := []byte("## `pkg`\n\n- func: `func Keep()`\n")
	dir := t.TempDir()
	baselinePath := writeTestFile(t, dir, "baseline.md", inventory)
	allowPath := writeTestFile(t, dir, "allow.md", inventory)
	expectedPath := writeTestFile(t, dir, "expected.md", []byte("## `pkg`\n\n- func: `func Keep(string)`\n"))

	if err := checkCompatibility(inventory, baselinePath, allowPath, expectedPath, ""); err == nil || !strings.Contains(err.Error(), "no longer needed") {
		t.Fatalf("stale compatibility exception passed: %v", err)
	}
	if err := checkCompatibility(inventory, baselinePath, allowPath, "", ""); err == nil || !strings.Contains(err.Error(), "must be supplied together") {
		t.Fatalf("unpaired compatibility file passed: %v", err)
	}
}

func TestCheckCompatibilityRequiresExplicitExactRemoval(t *testing.T) {
	baseline := []byte("## `pkg`\n\n- func: `func Keep()`\n- func: `func Remove()`\n")
	current := []byte("## `pkg`\n\n- func: `func Keep()`\n")
	dir := t.TempDir()
	baselinePath := writeTestFile(t, dir, "baseline.md", baseline)
	removalsPath := writeTestFile(t, dir, "removals.md", []byte("## `pkg`\n\n- func: `func Remove()`\n"))

	if err := checkCompatibility(current, baselinePath, "", "", ""); err == nil {
		t.Fatal("unreviewed removal passed compatibility check")
	}
	if err := checkCompatibility(current, baselinePath, "", "", removalsPath); err != nil {
		t.Fatalf("reviewed exact removal failed compatibility check: %v", err)
	}
}

func TestCheckCompatibilityRejectsInvalidRemovalException(t *testing.T) {
	baseline := []byte("## `pkg`\n\n- func: `func Remove(int)`\n")
	dir := t.TempDir()
	baselinePath := writeTestFile(t, dir, "baseline.md", baseline)

	tests := map[string]struct {
		current []byte
		removal []byte
		want    string
	}{
		"still current": {
			current: baseline,
			removal: baseline,
			want:    "approved removal is still current",
		},
		"not in baseline": {
			current: []byte("## `pkg`\n"),
			removal: []byte("## `pkg`\n\n- func: `func Missing()`\n"),
			want:    "approved removal is not present in baseline",
		},
		"signature change disguised as removal": {
			current: []byte("## `pkg`\n\n- func: `func Remove(string)`\n"),
			removal: baseline,
			want:    "approved removal has a current replacement",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			removalsPath := writeTestFile(t, t.TempDir(), "removals.md", test.removal)
			err := checkCompatibility(test.current, baselinePath, "", "", removalsPath)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("invalid removal exception was not rejected with %q: %v", test.want, err)
			}
		})
	}
}

func TestCheckCompatibilityRejectsReplacementAndRemovalOverlap(t *testing.T) {
	baseline := []byte("## `pkg`\n\n- func: `func Change(int)`\n")
	current := []byte("## `pkg`\n\n- func: `func Change(string)`\n")
	dir := t.TempDir()
	baselinePath := writeTestFile(t, dir, "baseline.md", baseline)
	allowPath := writeTestFile(t, dir, "allow.md", baseline)
	expectedPath := writeTestFile(t, dir, "expected.md", current)
	removalsPath := writeTestFile(t, dir, "removals.md", baseline)

	err := checkCompatibility(current, baselinePath, allowPath, expectedPath, removalsPath)
	if err == nil || !strings.Contains(err.Error(), "approved as both replacement and removal") {
		t.Fatalf("overlapping replacement/removal exception passed: %v", err)
	}
}

func TestParseInventoryRejectsDuplicateAPISlot(t *testing.T) {
	_, err := parseInventory([]byte("## `pkg`\n\n- func: `func Change(int)`\n- func: `func Change(string)`\n"))
	if err == nil || !strings.Contains(err.Error(), "multiple entries occupy API slot") {
		t.Fatalf("duplicate API slot was not rejected: %v", err)
	}
}

func packageEntriesForSource(t *testing.T, source string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "surface.go", source, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse source: %v", err)
	}
	config := &types.Config{}
	pkg, err := config.Check("example.com/surface", fset, []*ast.File{file}, nil)
	if err != nil {
		t.Fatalf("type-check source: %v", err)
	}
	return packageEntries(pkg)
}

func writeTestFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
