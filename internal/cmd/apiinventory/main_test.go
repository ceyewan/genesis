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

	"github.com/stretchr/testify/require"
)

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
	inventory, err := generate(moduleDir)
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

	if _, err := generate(moduleDir); err != nil {
		t.Fatalf("main and internal example packages should be accepted: %v", err)
	}

	writeModuleFile("examples/demo/proto/proto.go", "package proto\ntype Public struct{}\n")
	_, err := generate(moduleDir)
	if err == nil || !strings.Contains(err.Error(), "importable example package must move under internal") {
		t.Fatalf("public example package was not rejected: %v", err)
	}
}

func TestCheckCompatibilityPreservesStableAPI(t *testing.T) {
	t.Parallel()
	baseline := []byte("## `pkg`\n\n- func: `func Keep(int) string`\n")
	baselinePath := writeTestFile(t, t.TempDir(), "baseline.md", baseline)
	tests := []struct {
		name    string
		current []byte
		wantErr bool
	}{
		{name: "unchanged", current: baseline},
		{name: "addition", current: append(bytes.Clone(baseline), []byte("- func: `func Added()`\n")...)},
		{name: "signature change", current: []byte("## `pkg`\n\n- func: `func Keep(string) string`\n"), wantErr: true},
		{name: "removal", current: []byte("## `pkg`\n"), wantErr: true},
		{name: "rename", current: []byte("## `pkg`\n\n- func: `func Renamed(int) string`\n"), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := checkCompatibility(tt.current, baselinePath)
			if tt.wantErr {
				require.ErrorContains(t, err, "func Keep(int) string")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestCheckCompatibilityRejectsEmptyBaseline(t *testing.T) {
	t.Parallel()
	baselinePath := writeTestFile(t, t.TempDir(), "baseline.md", []byte("# Empty baseline\n"))
	require.ErrorContains(t, checkCompatibility([]byte("## `pkg`\n\n- func: `func Keep()`\n"), baselinePath), "baseline contains no exported declarations")
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
