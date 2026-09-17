package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gorbital.dev/cli/internal/recipes"
)

// layoutMoveResult runs orb upgrade --layout v0.2 in the current directory
// and returns its JSON result.
func layoutMoveResult(t *testing.T, wantCode int, args ...string) layoutResult {
	t.Helper()
	code, out, errOut := runOrb(t, append([]string{"upgrade", "--layout", "v0.2", "--json", "--skip-tidy", "--skip-build", "--yes"}, args...)...)
	if code != wantCode {
		t.Fatalf("orb upgrade --layout v0.2 %v = %d, want %d; stdout %s stderr %s", args, code, wantCode, out, errOut)
	}
	var res layoutResult
	if out != "" {
		if err := json.Unmarshal([]byte(out), &res); err != nil {
			t.Fatalf("orb upgrade --layout v0.2 --json output %q: %v", out, err)
		}
	}
	return res
}

// itemFor returns the report line about subject.
func itemFor(res layoutResult, subject string) (layoutItem, bool) {
	for _, item := range res.Items {
		if item.Subject == subject {
			return item, true
		}
	}
	return layoutItem{}, false
}

// TestLayoutMovePlansAnUntouchedApp checks the move of a Full app nobody
// changed: generated code goes, the app's modules are converted, the lock
// records the v0.2 layout and the migrations stay (ADR-0083).
func TestLayoutMovePlansAnUntouchedApp(t *testing.T) {
	newV01GitApp(t, recipes.TenancySingle)
	res := layoutMoveResult(t, 0)

	if res.From != recipes.LayoutV01 || res.To != recipes.LayoutV02 || res.Blocked {
		t.Fatalf("result = %+v", res)
	}
	if len(res.Manual) > 0 {
		t.Errorf("an untouched app needs manual changes: %v", res.Manual)
	}
	for _, m := range []string{"auth", "ops", "flags", "mailevents"} {
		item, ok := itemFor(res, "internal/modules/"+m)
		if !ok || item.Kind != itemLibrary {
			t.Errorf("internal/modules/%s: %+v, want a %s line", m, item, itemLibrary)
		}
	}
	if item, ok := itemFor(res, "internal/modules/projects/"); !ok || item.Kind != itemConverted {
		t.Errorf("the projects module: %+v, want a %s line", item, itemConverted)
	}
	if len(res.Modules) == 0 || !slices.ContainsFunc(res.Modules, func(c moduleConversion) bool { return c.Name == "projects" && len(c.Routes) == 5 }) {
		t.Errorf("converted modules = %+v, want the projects module's five routes", res.Modules)
	}

	// The app on disk: the composition root is gone, main.go calls
	// gorbital.Main and the lock records the layout and every file.
	for _, gone := range []string{"internal/app", "cmd/migrate", "cmd/seed", "internal/jobs"} {
		if _, err := os.Stat(gone); err == nil {
			t.Errorf("%s still exists", gone)
		}
	}
	main := readFile(t, filepath.Join("cmd", "api", "main.go"))
	if !strings.Contains(main, "gorbital.Main(") || !strings.Contains(main, "modules.All()") {
		t.Errorf("cmd/api/main.go doesn't run gorbital.Main:\n%s", main)
	}
	modules := readFile(t, filepath.Join("internal", "modules", "modules.gen.go"))
	for _, want := range []string{"ping.Module()", "projects.Module()"} {
		if !strings.Contains(modules, want) {
			t.Errorf("modules.gen.go doesn't list %s:\n%s", want, modules)
		}
	}
	lock, err := readLock(".")
	if err != nil {
		t.Fatal(err)
	}
	if lock.Inputs.Layout != recipes.LayoutV02 || len(lock.Ejected) != 0 {
		t.Errorf("gorbital.lock = %+v", lock)
	}
	if !lock.tracks("cmd/api/main.go") || lock.tracks("internal/app/app.go") {
		t.Error("gorbital.lock doesn't record the v0.2 templates' files")
	}
	// The migration history is untouched: the library's copies stay, since
	// gorbital.Migrate reads an identical copy as the same migration.
	for _, keep := range []string{"db/migrations/20260915000001_auth.sql", "db/migrations/20260915000002_projects.sql"} {
		if _, err := os.Stat(filepath.FromSlash(keep)); err != nil {
			t.Errorf("%s was removed: %v", keep, err)
		}
	}
	if _, err := os.Stat(upgradeReportPath); err != nil {
		t.Errorf("%s: %v", upgradeReportPath, err)
	}
	report := readFile(t, upgradeReportPath)
	for _, want := range []string{"## What happened", "## Going back", "internal/modules/projects/"} {
		if !strings.Contains(report, want) {
			t.Errorf("%s has no %q:\n%s", upgradeReportPath, want, report)
		}
	}
}

// TestLayoutMoveDryRunWritesNothing checks that the plan changes no file.
func TestLayoutMoveDryRunWritesNothing(t *testing.T) {
	newV01GitApp(t, recipes.TenancyMulti)
	res := layoutMoveResult(t, 0, "--dry-run")
	if !res.DryRun || len(res.Changes) == 0 {
		t.Fatalf("result = %+v", res)
	}
	if status := git(t, "status", "--porcelain"); status != "" {
		t.Errorf("--dry-run changed files:\n%s", status)
	}
	if _, err := os.Stat("internal/app/app.go"); err != nil {
		t.Errorf("--dry-run removed the composition root: %v", err)
	}
}

// TestLayoutMoveStopsAtChangesItCantMake checks that a change orb upgrade
// can't carry over stops the move, and that --allow-manual converts the rest
// and keeps the file.
func TestLayoutMoveStopsAtChangesItCantMake(t *testing.T) {
	newV01GitApp(t, recipes.TenancySingle)
	writeFile(t, filepath.Join("internal", "app", "keys.go"), "package app\n\n// changed by hand\n")
	commitAll(t, "Change a generated file")

	res := layoutMoveResult(t, 1)
	if !res.Blocked || len(res.Manual) == 0 {
		t.Fatalf("a changed generated file didn't stop the move: %+v", res)
	}
	if item, ok := itemFor(res, "internal/app/keys.go"); !ok || item.Kind != itemManual {
		t.Errorf("internal/app/keys.go: %+v, want a %s line", item, itemManual)
	}
	if status := git(t, "status", "--porcelain"); status != "" {
		t.Errorf("a blocked move changed files:\n%s", status)
	}

	res = layoutMoveResult(t, 0, "--allow-manual")
	if res.Blocked {
		t.Fatalf("--allow-manual is still blocked: %+v", res)
	}
	kept := filepath.Join(layoutPreservedDir, "internal", "app", "keys.go")
	if content := readFile(t, kept); !strings.Contains(content, "changed by hand") {
		t.Errorf("%s doesn't keep the file: %q", kept, content)
	}
	if _, err := os.Stat(filepath.Join("internal", "app", "keys.go")); err == nil {
		t.Error("internal/app/keys.go is still in the app")
	}
}

// TestLayoutMoveRefusals checks what the move refuses: an app already on the
// v0.2 layout, an app whose templates are older than this orb's, a Minimal
// app and a dirty repository.
func TestLayoutMoveRefusals(t *testing.T) {
	t.Run("already on v0.2", func(t *testing.T) {
		newV01GitApp(t, recipes.TenancySingle)
		layoutMoveResult(t, 0)
		if code, _, errOut := runOrb(t, "upgrade", "--layout", "v0.2", "--dry-run"); code != 1 || !strings.Contains(errOut, "already on the v0.2 layout") {
			t.Errorf("= %d %q", code, errOut)
		}
	})
	t.Run("unknown layout", func(t *testing.T) {
		newV01GitApp(t, recipes.TenancySingle)
		if code, _, errOut := runOrb(t, "upgrade", "--layout", "v0.3"); code != 2 || !strings.Contains(errOut, "not a layout") {
			t.Errorf("= %d %q", code, errOut)
		}
	})
	t.Run("older templates", func(t *testing.T) {
		newV01GitApp(t, recipes.TenancySingle)
		lock, err := readLock(".")
		if err != nil {
			t.Fatal(err)
		}
		i, ok := lock.find("README.md")
		if !ok {
			t.Fatal("the lock doesn't track README.md")
		}
		lock.Files[i].SHA256 = strings.Repeat("0", 64)
		b, err := lock.encode()
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, lockPath, string(b))
		commitAll(t, "An older release's lock")
		if code, _, errOut := runOrb(t, "upgrade", "--layout", "v0.2", "--dry-run"); code != 1 || !strings.Contains(errOut, "run orb upgrade") {
			t.Errorf("= %d %q", code, errOut)
		}
	})
	t.Run("dirty tree", func(t *testing.T) {
		newV01GitApp(t, recipes.TenancySingle)
		writeFile(t, "notes.txt", "uncommitted\n")
		if code, _, errOut := runOrb(t, "upgrade", "--layout", "v0.2", "--yes", "--skip-build"); code != 1 || !strings.Contains(errOut, "uncommitted changes") {
			t.Errorf("= %d %q", code, errOut)
		}
		res := layoutMoveResult(t, 0, "--allow-dirty")
		if res.Blocked {
			t.Errorf("--allow-dirty didn't convert: %+v", res)
		}
	})
}

// TestRewriteModuleRoutes checks the translation of v0.1's huma.Register
// calls: a literal becomes a gorbital verb with its options, an operation
// wrapper's fields are merged in, an operation without security becomes
// public, and an operation with a field the verbs don't carry goes through
// gorbital.dev/gorbital/operation.
func TestRewriteModuleRoutes(t *testing.T) {
	const src = `package delivery

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/modules/openapi"
)

type handler struct{}

func Register(api huma.API, h *handler) {
	signedIn := func(op huma.Operation) huma.Operation {
		op.Tags, op.Security = []string{"Books"}, openapi.Bearer
		op.Errors = append([]int{http.StatusUnauthorized, http.StatusForbidden}, op.Errors...)
		return op
	}
	huma.Register(api, signedIn(huma.Operation{
		OperationID: "books-create", Method: http.MethodPost, Path: "/v1/books",
		Summary: "Add a book", DefaultStatus: http.StatusCreated, Errors: []int{http.StatusConflict},
	}), h.create)
	huma.Register(api, huma.Operation{
		OperationID: "books-health", Method: http.MethodGet, Path: "/v1/books/health",
		Summary: "Health", Tags: []string{"Books"},
	}, h.health)
	huma.Register(api, huma.Operation{
		OperationID: "books-raw", Method: http.MethodPost, Path: "/v1/books/raw",
		Summary: "Raw", Security: openapi.Bearer, MaxBodyBytes: 1024,
	}, h.raw)
}
`
	files := map[string][]byte{"internal/modules/books/delivery/books.go": []byte(src)}
	res, err := rewriteModuleRoutes(files, "example.com/shop-api")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Problems) > 0 {
		t.Fatalf("problems: %v", res.Problems)
	}
	out := string(res.Files["internal/modules/books/delivery/books.go"])
	for _, want := range []string{
		"func Register(api *gorbital.Router, h *handler)",
		`gorbital.Post(api, "/v1/books", h.create`,
		`gorbital.OperationID("books-create")`,
		`gorbital.Tags("Books")`,
		"gorbital.Status(http.StatusCreated)",
		"gorbital.Errors(http.StatusUnauthorized, http.StatusForbidden, http.StatusConflict)",
		`gorbital.Get(api, "/v1/books/health", h.health`,
		"guard.Public()",
		"operation.Register(api, huma.Operation{",
		`"gorbital.dev/gorbital/guard"`,
		`"gorbital.dev/gorbital/operation"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the converted file has no %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "signedIn :=") {
		t.Errorf("the operation wrapper is still declared:\n%s", out)
	}
	if !slices.Contains(res.Public, "books-health") || len(res.Routes) != 2 || len(res.Escaped) != 1 {
		t.Errorf("result = %+v", res)
	}
}

// TestRewriteModuleRoutesReportsWhatItCantConvert checks that an operation
// with a field neither the verbs nor operation.Register carry stops the
// module's conversion instead of changing the operation.
func TestRewriteModuleRoutesReportsWhatItCantConvert(t *testing.T) {
	const src = `package delivery

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

type handler struct{}

func Register(api huma.API, h *handler) {
	huma.Register(api, huma.Operation{
		OperationID: "books-hidden", Method: http.MethodGet, Path: "/v1/books/hidden", Hidden: true,
	}, h.hidden)
}
`
	res, err := rewriteModuleRoutes(map[string][]byte{"internal/modules/books/delivery/books.go": []byte(src)}, "example.com/shop-api")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Problems) != 1 || !strings.Contains(res.Problems[0], "Hidden") {
		t.Fatalf("problems = %v, want one naming Hidden", res.Problems)
	}
}

// TestTransplant checks how a change to generated code is moved into the
// library's version of the same file: by the lines around it, or not at all.
func TestTransplant(t *testing.T) {
	base := "package usecase\n\nfunc Register(email string) error {\n\tif err := validate(email); err != nil {\n\t\treturn err\n\t}\n\treturn insert(email)\n}\n"
	ours := "package usecase\n\nfunc Register(email string) error {\n\tif err := validate(email); err != nil {\n\t\treturn err\n\t}\n\tif banned(email) {\n\t\treturn ErrBanned\n\t}\n\treturn insert(email)\n}\n"
	target := "package usecase\n\n// Register creates an account.\nfunc Register(email string) error {\n\tif err := validate(email); err != nil {\n\t\treturn err\n\t}\n\treturn insertWithHooks(email)\n}\n"

	merged, carried, failed := transplant([]byte(base), []byte(ours), []byte(target))
	if len(failed) > 0 || carried != 1 {
		t.Fatalf("transplant = %d changes, failed %v", carried, failed)
	}
	if !strings.Contains(string(merged), "if banned(email) {") || !strings.Contains(string(merged), "insertWithHooks") {
		t.Errorf("merged:\n%s", merged)
	}

	// A change to lines the library's file doesn't have can't be placed.
	otherOurs := strings.Replace(base, "return insert(email)", "return insertTwice(email)", 1)
	if _, _, failed := transplant([]byte(base), []byte(otherOurs), []byte(target)); len(failed) != 1 {
		t.Errorf("failed = %v, want one change that doesn't apply", failed)
	}
}

// TestLayoutFlag checks how orb upgrade finds --layout before parsing.
func TestLayoutFlag(t *testing.T) {
	for _, tt := range []struct {
		args []string
		want string
		ok   bool
	}{
		{[]string{"--layout", "v0.2"}, "v0.2", true},
		{[]string{"--layout=v0.2", "--dry-run"}, "v0.2", true},
		{[]string{"-layout", "v0.2"}, "v0.2", true},
		{[]string{"--dry-run"}, "", false},
		{[]string{"--from", "v0.1.0"}, "", false},
	} {
		got, ok := layoutFlag(tt.args)
		if got != tt.want || ok != tt.ok {
			t.Errorf("layoutFlag(%v) = %q, %v; want %q, %v", tt.args, got, ok, tt.want, tt.ok)
		}
	}
}
