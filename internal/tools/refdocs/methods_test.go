package main

import (
	"bytes"
	"flag"
	"go/ast"
	"go/doc"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

var update = flag.Bool("update", false, "rewrite testdata/methods-golden from the output")

// testRoot is a small repository: a core package with examples and an
// overlay, a module package with problems, and directories that aren't
// library packages.
const testRoot = "testdata/methods"

// testSince marks everything in widget, and gadget.Old, as released.
var testSince = sinceIndex{
	"gorbital.dev/widget\tKind":            "v0.1.0",
	"gorbital.dev/widget\tKindSmall":       "v0.1.0",
	"gorbital.dev/widget\tKindLarge":       "v0.1.0",
	"gorbital.dev/widget\tMaxSize":         "v0.1.0",
	"gorbital.dev/widget\tErrBroken":       "v0.1.0",
	"gorbital.dev/widget\tWidget":          "v0.1.0",
	"gorbital.dev/widget\tNew":             "v0.1.0",
	"gorbital.dev/widget\tWidget.Spin":     "v0.1.0",
	"gorbital.dev/widget\tWidget.Stop":     "v0.1.0",
	"gorbital.dev/widget\tPair":            "v0.1.0",
	"gorbital.dev/widget\tPair.Swap":       "v0.1.0",
	"gorbital.dev/widget\tSpinner":         "v0.1.0",
	"gorbital.dev/widget\tHelper":          "v0.1.0",
	"gorbital.dev/modules/gadget\tOld":     "v0.1.0",
	"gorbital.dev/modules/gadget\tLoose":   "v0.1.0",
	"gorbital.dev/modules/gadget\tGadget":  "v0.1.0",
	"gorbital.dev/modules/gadget\tMissing": "v0.1.0",
}

func testSite(t *testing.T) *methodsSite {
	t.Helper()
	pkgs, err := loadLibrary(testRoot)
	if err != nil {
		t.Fatal(err)
	}
	overlays, err := readOverlays(filepath.Join(testRoot, methodsOverlayDir))
	if err != nil {
		t.Fatal(err)
	}
	return &methodsSite{pkgs: pkgs, since: testSince, overlays: overlays}
}

func TestLoadLibraryPackageSet(t *testing.T) {
	site := testSite(t)
	var got []string
	for _, p := range site.pkgs {
		got = append(got, p.importPath+" → "+p.slug)
	}
	want := []string{"gorbital.dev/modules/gadget → modules-gadget", "gorbital.dev/widget → widget"}
	if !slices.Equal(got, want) {
		t.Errorf("packages = %q, want %q (internal, cmd main and cli are excluded)", got, want)
	}
}

func TestParseListingLine(t *testing.T) {
	for _, tc := range []struct {
		line, pkg, ident string
		ok               bool
	}{
		{"pkg gorbital.dev/ratelimit, func Per(int, time.Duration) Limit", "gorbital.dev/ratelimit", "Per", true},
		{"pkg gorbital.dev/ratelimit, method (*Limiter) Take(context.Context, string) (Decision, error)", "gorbital.dev/ratelimit", "Limiter.Take", true},
		{"pkg gorbital.dev/actor, method (Actor) Can(string) bool", "gorbital.dev/actor", "Actor.Can", true},
		{"pkg gorbital.dev/ratelimit, type Decision struct, Allowed bool", "gorbital.dev/ratelimit", "Decision", true},
		{"pkg gorbital.dev/modules/settings, type Value[T any] struct", "gorbital.dev/modules/settings", "Value", true},
		{"pkg gorbital.dev/modules/settings, func Declare[T any](*Registry, string, T) Value[T]", "gorbital.dev/modules/settings", "Declare", true},
		{"pkg gorbital.dev/mail, type Sender interface, Send(context.Context, Message) error", "gorbital.dev/mail", "Sender", true},
		{"pkg gorbital.dev/actor, const KindUser = \"user\"", "gorbital.dev/actor", "KindUser", true},
		{"pkg gorbital.dev/actor, const KindUser Kind", "gorbital.dev/actor", "KindUser", true},
		{"pkg gorbital.dev/actor, var ErrForbidden error", "gorbital.dev/actor", "ErrForbidden", true},
		{"# a comment", "", "", false},
		{"pkg gorbital.dev/actor, embedded Foo", "", "", false},
		{"pkg gorbital.dev/actor", "", "", false},
	} {
		pkg, ident, ok := parseListingLine(tc.line)
		if pkg != tc.pkg || ident != tc.ident || ok != tc.ok {
			t.Errorf("parseListingLine(%q) = %q, %q, %v; want %q, %q, %v", tc.line, pkg, ident, ok, tc.pkg, tc.ident, tc.ok)
		}
	}
}

func TestLoadSince(t *testing.T) {
	fsys := fstest.MapFS{
		"since/v0.10.0/x.txt": {Data: []byte("pkg gorbital.dev/x, func A()\npkg gorbital.dev/x, func C()\n")},
		"since/v0.2.0/x.txt":  {Data: []byte("pkg gorbital.dev/x, func A()\npkg gorbital.dev/x, method (*T) M()\n")},
	}
	idx, err := loadSince(fsys)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ ident, want string }{
		{"A", "v0.2.0"}, // the oldest listing wins, compared numerically
		{"T.M", "v0.2.0"},
		{"C", "v0.10.0"},
		{"D", nextVersion},
	} {
		if got := idx.of("gorbital.dev/x", tc.ident); got != tc.want {
			t.Errorf("since of %s = %q, want %q", tc.ident, got, tc.want)
		}
	}
}

func TestEmbeddedSinceMatchesAPIListings(t *testing.T) {
	idx, err := loadSince(sinceFS)
	if err != nil {
		t.Fatal(err)
	}
	for _, ident := range []string{"Run", "Cleanup.Close", "WithCleanup"} {
		if got := idx.of("gorbital.dev/app", ident); got != "v0.1.0" {
			t.Errorf("gorbital.dev/app %s since %q, want v0.1.0", ident, got)
		}
	}
}

// parseDoc builds a go/doc package from one source file.
func parseDoc(t *testing.T, src string) (*token.FileSet, *doc.Package) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	d, err := doc.NewFromFiles(fset, []*ast.File{f}, "gorbital.dev/x")
	if err != nil {
		t.Fatal(err)
	}
	return fset, d
}

func TestFormatDecl(t *testing.T) {
	src := `package x

// F has a body that isn't shown.
func F(a, b int, opts ...string) (n int, err error) { return 0, nil }

// T has an unexported field.
type T struct {
	// A is documented.
	A string
	b int
}

// M is a method.
func (t *T) M(ctx interface{ Done() <-chan struct{} }) {}

// G is generic.
func G[K comparable, V any](m map[K]V) []K { return nil }

// I has an unexported method.
type I interface {
	Exported() error
	hidden()
}

// Both are grouped.
const (
	One = 1 // the first
	Two = 2
)
`
	fset, d := parseDoc(t, src)
	typ := func(name string) *doc.Type {
		for _, ty := range d.Types {
			if ty.Name == name {
				return ty
			}
		}
		t.Fatalf("no type %s", name)
		return nil
	}
	for _, tc := range []struct {
		name string
		decl ast.Decl
		want string
	}{
		{"function without body or doc", d.Funcs[0].Decl, "func F(a, b int, opts ...string) (n int, err error)"},
		{"generic function", d.Funcs[1].Decl, "func G[K comparable, V any](m map[K]V) []K"},
		{"method", typ("T").Methods[0].Decl, "func (t *T) M(ctx interface{ Done() <-chan struct{} })"},
		{"struct with field doc and filtered fields", typ("T").Decl, "type T struct {\n\t// A is documented.\n\tA string\n\t// contains filtered or unexported fields\n}"},
		{"interface with filtered methods", typ("I").Decl, "type I interface {\n\tExported() error\n\t// contains filtered or unexported methods\n}"},
		{"constant group with line comments", d.Consts[0].Decl, "const (\n\tOne = 1 // the first\n\tTwo = 2\n)"},
	} {
		if got := formatDecl(fset, tc.decl); got != tc.want {
			t.Errorf("%s:\ngot:\n%s\nwant:\n%s", tc.name, got, tc.want)
		}
	}
}

func TestDocMarkdown(t *testing.T) {
	site := testSite(t)
	widget := site.pkgs[1]
	for _, tc := range []struct {
		name, text string
		level      int
		want       string
	}{
		{"paragraph", "Spin spins.\n", 5, "Spin spins.\n\n"},
		{"heading at level", "Intro.\n\n# Usage\n\nText.\n", 2, "Intro.\n\n## Usage\n\nText.\n\n"},
		{"list", "Items:\n  - one\n  - two\n", 5, "Items:\n\n  - one\n  - two\n\n"},
		{"Go code block", "Use:\n\n\tw := New(\"a\")\n", 5, "Use:\n\n```go\nw := New(\"a\")\n```\n\n"},
		{"other code block", "Run:\n\n\tgo build -ldflags \"-X a=b\" ./cmd/api\n", 5, "Run:\n\n```\ngo build -ldflags \"-X a=b\" ./cmd/api\n```\n\n"},
		{"link in this package", "See [New] and [Widget.Spin].\n", 5, "See [New](#New) and [Widget.Spin](#Widget.Spin).\n\n"},
		{"link to another library package", "See [gorbital.dev/modules/gadget.Make].\n", 5, "See [gorbital.dev/modules/gadget.Make](modules-gadget.md#Make).\n\n"},
		{"link to the standard library", "See [errors.Is] and [io].\n", 5, "See [errors.Is](https://pkg.go.dev/errors#Is) and [io](https://pkg.go.dev/io).\n\n"},
		{"URL", "See https://example.com/x.\n", 5, "See [https://example.com/x](https://example.com/x).\n\n"},
		{"empty", "", 5, ""},
	} {
		if got := docMarkdown(widget, site.slugs(), tc.text, tc.level); got != tc.want {
			t.Errorf("%s:\ngot:  %q\nwant: %q", tc.name, got, tc.want)
		}
	}
}

func TestExampleCode(t *testing.T) {
	site := testSite(t)
	widget := site.pkgs[1]
	var newEx *doc.Example
	for _, f := range widget.doc.Types[slices.IndexFunc(widget.doc.Types, func(ty *doc.Type) bool { return ty.Name == "Widget" })].Funcs {
		if f.Name == "New" {
			newEx = f.Examples[0]
		}
	}
	if newEx == nil {
		t.Fatal("ExampleNew isn't attached to New")
	}
	want := "w := widget.New(\"a\")\nfmt.Println(w.Name)\n"
	if got := exampleCode(widget.fset, newEx); got != want {
		t.Errorf("exampleCode = %q, want %q (braces, indentation and the output comment removed)", got, want)
	}
	if newEx.Output != "a\n" {
		t.Errorf("output = %q", newEx.Output)
	}
}

// TestMethodGrouping checks that constructors, methods and typed constants
// sit under their type, in go doc's order.
func TestMethodGrouping(t *testing.T) {
	site := testSite(t)
	page := string(site.renderPackage(site.pkgs[1]))
	inOrder := func(parts ...string) {
		t.Helper()
		at := 0
		for _, p := range parts {
			i := strings.Index(page[at:], p)
			if i < 0 {
				t.Fatalf("%q missing or out of order after offset %d", p, at)
			}
			at += i + len(p)
		}
	}
	inOrder(
		"# widget\n",
		"## Usage\n",
		"Use widgets for [small things](../guides/widgets.md).",
		"## Contents\n",
		"  - [`Kind`](#Kind): [`KindSmall`](#KindSmall), [`KindLarge`](#KindLarge)\n",
		"  - [`Widget`](#Widget): [`New`](#New), [`Widget.Spin`](#Widget.Spin), [`Widget.Stop`](#Widget.Stop)\n",
		"## Constants\n", `<a id="MaxSize"></a>`,
		"## Variables\n", `<a id="ErrBroken"></a>`,
		"## Functions\n", "### func Helper\n",
		"## Types\n",
		"### type Kind\n", `<a id="KindSmall"></a>`, "a small widget",
		"### type Pair\n", "#### func (*Pair[K, V]) Swap\n",
		`<a id="Spinner.Spin"></a>`, "### type Spinner\n",
		`<a id="Widget.Name"></a>`, `<a id="Widget.Reader"></a>`, "### type Widget\n",
		`<a id="New"></a>`, "#### func New\n", "**Example**", "Output:\n\n```text\na\n```",
		`<a id="Widget.Spin"></a>`, "#### func (*Widget) Spin\n", "  - once\n", "**Example (twice)**", "A widget spins.",
		"#### func (Widget) Stop\n",
	)
	if strings.Contains(page, "size int") {
		t.Error("unexported field shown")
	}
}

func TestProblems(t *testing.T) {
	site := testSite(t)
	site.overlays["nope"] = []byte("x")
	var got []string
	for _, p := range site.problems() {
		got = append(got, p.String())
	}
	want := []string{
		"internal/tools/refdocs/overlay/methods/nope.md:1: nope is not a library package (overlay names are page slugs, such as httpx or modules-auth)",
		"modules/gadget/gadget.go:7: gadget.Undocumented " + msgNoDoc,
		"modules/gadget/gadget.go:7: gadget.Undocumented " + msgNoExample,
		"modules/gadget/gadget.go:10: gadget.Fresh " + msgNoExample,
		"modules/gadget/gadget.go:18: gadget.Gadget.Run " + msgNoDoc,
		"modules/gadget/gadget.go:21: gadget.Loose " + msgNoDoc,
	}
	if !slices.Equal(got, want) {
		t.Errorf("problems:\ngot:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestGoldenPages(t *testing.T) {
	site := testSite(t)
	for _, p := range site.pages() {
		path := filepath.Join("testdata", "methods-golden", p.file)
		if *update {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, p.content, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%v (run go test -run TestGoldenPages -update)", err)
		}
		if !bytes.Equal(p.content, want) {
			t.Errorf("%s differs from the golden file; review and run go test -run TestGoldenPages -update\ngot:\n%s", p.file, p.content)
		}
	}
}

func TestRunMethodsWriteThenCheck(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(testRoot)); err != nil {
		t.Fatal(err)
	}
	// Leave only what passes the checks: gadget has problems by design.
	if err := os.RemoveAll(filepath.Join(root, "modules")); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runMethodsWith(root, testSince, false, &out); err == nil || !strings.Contains(out.String(), "docs/methods/widget.md: stale") {
		t.Fatalf("check before writing: err = %v, output:\n%s", err, &out)
	}
	out.Reset()
	if err := runMethodsWith(root, testSince, true, &out); err != nil {
		t.Fatalf("write: %v\n%s", err, &out)
	}
	out.Reset()
	if err := runMethodsWith(root, testSince, false, &out); err != nil {
		t.Fatalf("check after writing: %v\n%s", err, &out)
	}

	nav := filepath.Join(root, "docs", "docs.json")
	if err := os.WriteFile(nav, []byte(`{ "source": "docs/methods/index.md" }`), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := runMethodsWith(root, testSince, false, &out); err == nil || !strings.Contains(out.String(), "docs/docs.json: docs/methods/widget.md isn't in the Methods tab") {
		t.Fatalf("check with a page missing from docs.json: err = %v, output:\n%s", err, &out)
	}
	if err := os.Remove(nav); err != nil {
		t.Fatal(err)
	}

	pagePath := filepath.Join(root, methodsDir, "widget.md")
	if err := os.WriteFile(pagePath, []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	extra := filepath.Join(root, methodsDir, "gone.md")
	if err := os.WriteFile(extra, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	err := runMethodsWith(root, testSince, false, &out)
	if err == nil || !strings.Contains(out.String(), "docs/methods/widget.md: stale") || !strings.Contains(out.String(), "docs/methods/gone.md: no such package") {
		t.Fatalf("check after editing: err = %v, output:\n%s", err, &out)
	}
	out.Reset()
	if err := runMethodsWith(root, testSince, true, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(extra); !os.IsNotExist(err) {
		t.Errorf("write kept a page for no package: %v", err)
	}
}

func TestCompareVersions(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"v0.1.0", "v0.2.0", -1},
		{"v0.10.0", "v0.2.0", 1},
		{"v1.0.0", "v1.0.0", 0},
		{"v1.0", "v1.0.1", -1},
	} {
		if got := compareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
