package recipes_test

import (
	"bytes"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorbital.dev/cli/internal/recipes"
	"gorbital.dev/cli/internal/recipes/generate"
)

// goldenApps are the hand-written apps the templates are generated from.
// The v0.1 trees have one each; the v0.3.0 tree has three, one per shape
// the guides document, and the manifest and the conditional templates
// decide which of its paths each one gets (ADR-0090).
var goldenApps = []struct {
	name, preset, tenancy, layout, dir string
	auth, scope                        string
	// templates is the tree generate.Run writes from this golden app, for
	// the v0.1 trees, which have one golden app each. The v0.3.0 tree is
	// written from examples/full-multi alone, and TestGoldenApps checks it
	// against all three of its shapes instead.
	templates string
}{
	{"minimal", "minimal", "single", recipes.LayoutV01, "../../../examples/minimal", "", "", "minimal"},
	{"v0.1/full-single", "full", "single", recipes.LayoutV01, "../../../examples/v0.1/full-single", "", "", "full"},
	{"v0.1/full-multi", "full", "multi", recipes.LayoutV01, "../../../examples/v0.1/full-multi", "", "", "full-multi"},
	{"full-single", "full", "single", recipes.LayoutV02, "../../../examples/full-single", recipes.AuthFull, recipes.ScopeSingle, ""},
	{"full-multi", "full", "multi", recipes.LayoutV02, "../../../examples/full-multi", recipes.AuthFull, recipes.DefaultScopeName, ""},
	{"api-basic", "full", "single", recipes.LayoutV02, "../../../examples/api-basic", recipes.AuthBasic, recipes.ScopeNone, ""},
}

// produced are the paths orb new writes into an app rather than rendering
// from a template: the demonstration module it generates with the
// resource generator, and the API artefacts the app itself writes
// (ADR-0090 §5). They are in the golden apps and in no tree.
func produced(rel string) bool {
	switch rel {
	// Rewritten by the demonstration module orb new generates: the module
	// list it adds itself to, and the access rule it records.
	case "internal/modules/modules.gen.go", "gorbital.yaml":
		return true
	case "db/migrations/" + recipes.DemoMigrationVersion + "_projects.sql":
		return true
	}
	return strings.HasPrefix(rel, "internal/modules/projects/") || strings.HasPrefix(rel, "api/")
}

// goldenFiles returns the files of the golden app at dir that git tracks or
// would track, as go generate templates them; nil outside a git work tree.
func goldenFiles(t *testing.T, dir string) map[string]bool {
	t.Helper()
	files, ok, err := generate.GitFiles(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		return nil
	}
	return files
}

// renderInto writes the preset as orb new does.
func renderInto(t *testing.T, preset, tenancy string, d recipes.Data) (string, []recipes.File) {
	t.Helper()
	p, ok := recipes.LookupPreset(preset, tenancy)
	if !ok {
		t.Fatalf("LookupPreset(%q, %q) found nothing", preset, tenancy)
	}
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	files, err := p.Render(root, d)
	if err != nil {
		t.Fatalf("Render(%s) error = %v", preset, err)
	}
	return dir, files
}

// profileOf returns the profile of a golden app on gorbital.Main, and the
// zero profile for a v0.1 one.
func profileOf(t *testing.T, layout, auth, scope string) recipes.Profile {
	t.Helper()
	if layout != recipes.LayoutV02 {
		return recipes.Profile{}
	}
	p, err := recipes.ParseProfile(auth, scope)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// renderProfile writes the profile's app as orb new does.
func renderProfile(t *testing.T, auth, scope string, d recipes.Data) (string, []recipes.File) {
	t.Helper()
	p, err := recipes.ParseProfile(auth, scope)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	files, err := p.Render(root, d)
	if err != nil {
		t.Fatalf("Render(%s) error = %v", p, err)
	}
	return dir, files
}

// renderLayout writes the preset's tree in layout, as orb upgrade rebuilds
// it, and returns the directory and the files' paths.
func renderLayout(t *testing.T, preset, tenancy, layout string, d recipes.Data) (string, []string) {
	t.Helper()
	tree, err := recipes.Embedded().Tree(preset, tenancy, layout, "", d)
	if err != nil {
		t.Fatalf("Tree(%s, %s, %s) error = %v", preset, tenancy, layout, err)
	}
	dir := t.TempDir()
	var paths []string
	for p, content := range tree {
		target := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, content, 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	return dir, paths
}

// unejected copies the golden app at dir, as git tracks it, and takes out
// the built-in modules orb new copies into apps of the preset
// (generate.Uneject), returning the copy: what the templates hold. It
// returns dir itself for presets orb new copies nothing into.
func unejected(t *testing.T, dir, preset, tenancy, layout, auth, scope string) string {
	t.Helper()
	copies := goldenCopies(t, preset, tenancy, layout, auth, scope)
	if len(copies) == 0 {
		return dir
	}
	tracked := goldenFiles(t, dir)
	out := t.TempDir()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		rel = filepath.ToSlash(rel)
		if generate.Skipped(rel) && rel != "go.mod" || (tracked != nil && !tracked[rel] && rel != "go.mod") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		target := filepath.Join(out, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	var modules []generate.Ejected
	for _, e := range copies {
		modules = append(modules, generate.Ejected{Dir: e.Dir(), Package: e.Package})
	}
	if err := generate.Uneject(out, generate.PlaceholderModule, modules); err != nil {
		t.Fatal(err)
	}
	return out
}

// goldenCopies are the built-in modules orb copies into an app of the
// golden app's shape: the profile's for the v0.2 layout, the preset's for
// v0.1.
func goldenCopies(t *testing.T, preset, tenancy, layout, auth, scope string) []recipes.EjectedModule {
	t.Helper()
	if layout == recipes.LayoutV02 {
		p, err := recipes.ParseProfile(auth, scope)
		if err != nil {
			t.Fatal(err)
		}
		return p.Copies()
	}
	p, _ := recipes.LookupPreset(preset, tenancy)
	if layout != p.Layout() {
		return nil
	}
	return p.Ejects()
}

// TestGoldenApps: rendering each preset with the placeholder name reproduces
// its hand-written golden app exactly, file for file. The golden Full apps
// hold sign-in and organisations as orb new copies them; the templates are
// the golden apps without them, and api/surface.json of the app's own code
// (TestNewAppIsTheGoldenApp checks the copies).
func TestGoldenApps(t *testing.T) {
	for _, golden := range goldenApps {
		t.Run(golden.name, func(t *testing.T) {
			dir, files := renderLayout(t, golden.preset, golden.tenancy, golden.layout, recipes.Data{
				Name:           generate.PlaceholderName,
				Module:         generate.PlaceholderModule,
				LibraryVersion: recipes.LibraryVersion,
				Profile:        profileOf(t, golden.layout, golden.auth, golden.scope),
			})
			goldenDir := unejected(t, golden.dir, golden.preset, golden.tenancy, golden.layout, golden.auth, golden.scope)
			tracked := goldenFiles(t, goldenDir)
			copied := goldenDir != golden.dir
			if copied {
				tracked = nil // the copy holds only tracked files
			}
			golden := golden
			golden.dir = goldenDir
			rendered := map[string]bool{}
			for _, f := range files {
				rendered[f] = true
			}
			err := filepath.WalkDir(golden.dir, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				rel, _ := filepath.Rel(golden.dir, p)
				rel = filepath.ToSlash(rel)
				if d.IsDir() {
					if generate.SkippedDirs[d.Name()] {
						return filepath.SkipDir
					}
					return nil
				}
				if generate.Skipped(rel) || (tracked != nil && !tracked[rel]) {
					return nil
				}
				if golden.layout == recipes.LayoutV02 && produced(rel) {
					return nil // orb new writes it, no template does
				}
				if rel == "api/surface.json" && copied {
					delete(rendered, rel)
					return nil // recorded by go generate, not by Uneject
				}
				want, _ := os.ReadFile(p)
				got, readErr := os.ReadFile(filepath.Join(dir, rel))
				switch {
				case readErr != nil:
					t.Errorf("golden file %s was not rendered: %v", rel, readErr)
				case !bytes.Equal(got, want):
					t.Errorf("rendered %s differs from %s/%s", rel, golden.dir, rel)
				}
				delete(rendered, rel)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			delete(rendered, "go.mod")
			for extra := range rendered {
				if golden.layout == recipes.LayoutV02 && produced(extra) {
					continue // orb new rewrites it after rendering
				}
				t.Errorf("rendered %s, which is not in %s", extra, golden.dir)
			}
		})
	}
}

// TestGoModMatchesGolden: with the golden app's own module path, library
// version and relative checkout, the rendered go.mod requires and replaces
// exactly what the golden go.mod does.
func TestGoModMatchesGolden(t *testing.T) {
	for _, golden := range goldenApps {
		t.Run(golden.name, func(t *testing.T) {
			local := "../.."
			if golden.layout == recipes.LayoutV01 && golden.preset == "full" {
				local = "../../.." // examples/v0.1/<app>
			}
			dir, _ := renderLayout(t, golden.preset, golden.tenancy, golden.layout, recipes.Data{
				Name: generate.PlaceholderName, Module: generate.PlaceholderModule, LibraryVersion: recipes.LibraryVersion, Local: local,
				Profile: profileOf(t, golden.layout, golden.auth, golden.scope),
			})
			gotRequires, gotReplaces := parseGoMod(t, filepath.Join(dir, "go.mod"))
			wantRequires, wantReplaces := parseGoMod(t, filepath.Join(golden.dir, "go.mod"))
			// The golden app requires directly what its copies of sign-in
			// and organisations import, which go mod tidy marks after orb
			// new copies them: compare modules and versions.
			if len(goldenCopies(t, golden.preset, golden.tenancy, golden.layout, golden.auth, golden.scope)) > 0 {
				for _, m := range []map[string]string{gotRequires, wantRequires} {
					for k, v := range m {
						m[k] = strings.TrimSuffix(v, " // indirect")
					}
				}
			}
			if !maps.Equal(gotRequires, wantRequires) {
				t.Errorf("rendered requirements = %v, want %v", gotRequires, wantRequires)
			}
			if !maps.Equal(gotReplaces, wantReplaces) {
				t.Errorf("rendered replacements = %v, want %v", gotReplaces, wantReplaces)
			}
		})
	}
}

// parseGoMod returns a go.mod's requirements (path → version and any
// comment) and replacements (path → target).
func parseGoMod(t *testing.T, path string) (requires, replaces map[string]string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	requires, replaces = map[string]string{}, map[string]string{}
	add := func(directive, entry string) {
		switch directive {
		case "require":
			module, version, _ := strings.Cut(entry, " ")
			requires[module] = version
		case "replace":
			module, target, _ := strings.Cut(entry, " => ")
			replaces[module] = target
		}
	}
	block := ""
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "" || strings.HasPrefix(line, "//"):
		case line == "require (" || line == "replace (":
			block = strings.TrimSuffix(line, " (")
		case line == ")":
			block = ""
		case block != "":
			add(block, line)
		case strings.HasPrefix(line, "require "), strings.HasPrefix(line, "replace "):
			directive, entry, _ := strings.Cut(line, " ")
			add(directive, entry)
		}
	}
	return requires, replaces
}

func TestRenderGoMod(t *testing.T) {
	dir, _ := renderInto(t, "minimal", "single", recipes.Data{Name: "shop-api", Module: "github.com/acme/shop-api", LibraryVersion: "v0.1.0"})
	goMod, _ := os.ReadFile(filepath.Join(dir, "go.mod"))
	if !strings.HasPrefix(string(goMod), "module github.com/acme/shop-api\n") || !strings.Contains(string(goMod), "gorbital.dev v0.1.0") || strings.Contains(string(goMod), "replace") {
		t.Errorf("go.mod without Local:\n%s", goMod)
	}
	main, _ := os.ReadFile(filepath.Join(dir, "cmd", "api", "main.go"))
	if !strings.Contains(string(main), `"github.com/acme/shop-api/internal/app"`) || strings.Contains(string(main), "acme-api") {
		t.Errorf("cmd/api/main.go not rendered for the new module:\n%s", main)
	}

	dir, _ = renderProfile(t, recipes.AuthFull, recipes.ScopeSingle, recipes.Data{Name: "shop-api", Module: "shop-api", LibraryVersion: "v0.1.0", Local: "/src/gorbital"})
	goMod, _ = os.ReadFile(filepath.Join(dir, "go.mod"))
	for _, want := range []string{
		"gorbital.dev/gorbital v0.1.0",
		"github.com/riverqueue/river ",
		"gorbital.dev => /src/gorbital\n",
		"gorbital.dev/gorbital => /src/gorbital/gorbital\n",
		"gorbital.dev/modules/auth => /src/gorbital/modules/auth\n",
	} {
		if !strings.Contains(string(goMod), want) {
			t.Errorf("full go.mod with Local lacks %q:\n%s", want, goMod)
		}
	}

	dir, _ = renderInto(t, "minimal", "single", recipes.Data{Name: "shop-api", Module: "shop-api", LibraryVersion: "v0.1.0", Local: "/Users/me/My Code/gorbital"})
	goMod, _ = os.ReadFile(filepath.Join(dir, "go.mod"))
	if !strings.Contains(string(goMod), `gorbital.dev/modules/openapi => "/Users/me/My Code/gorbital/modules/openapi"`) {
		t.Errorf("go.mod with a Local path containing a space doesn't quote it:\n%s", goMod)
	}
}

func TestLayouts(t *testing.T) {
	// orb new writes the v0.2 layout for the Full preset: main.go on
	// gorbital.Main, no internal/app.
	dir, _ := renderProfile(t, recipes.AuthFull, recipes.DefaultScopeName, recipes.Data{Name: "shop-api", Module: "shop-api", LibraryVersion: "v0.1.0"})
	main, _ := os.ReadFile(filepath.Join(dir, "cmd", "api", "main.go"))
	if !strings.Contains(string(main), "gorbital.Main(") || !strings.Contains(string(main), "orgshttp.Module(auth)") {
		t.Errorf("new Full app's main.go isn't on gorbital.Main:\n%s", main)
	}
	for _, gone := range []string{"internal/app", "cmd/migrate", "cmd/seed"} {
		if _, err := os.Stat(filepath.Join(dir, gone)); err == nil {
			t.Errorf("new Full app has %s", gone)
		}
	}
	for _, tt := range []struct{ preset, tenancy, want string }{
		{"minimal", "single", recipes.LayoutV01}, {"full", "single", recipes.LayoutV02}, {"full", "multi", recipes.LayoutV02},
	} {
		if p, _ := recipes.LookupPreset(tt.preset, tt.tenancy); p.Layout() != tt.want {
			t.Errorf("%s %s Layout() = %s, want %s", tt.preset, tt.tenancy, p.Layout(), tt.want)
		}
	}
	// v0.1 apps keep their layout's templates.
	v01, err := recipes.Embedded().Tree("full", "single", recipes.LayoutV01, "", recipes.Data{Name: "shop-api", Module: "shop-api", LibraryVersion: "v0.1.0"})
	if err != nil || v01["internal/app/app.go"] == nil {
		t.Errorf("v0.1 tree has no internal/app/app.go (%v)", err)
	}
	if _, err := recipes.Embedded().Tree("minimal", "single", recipes.LayoutV02, "", recipes.Data{Name: "shop-api", Module: "shop-api"}); err == nil {
		t.Error("Tree(minimal, v0.2) error = nil; Minimal has no v0.2 layout")
	}
	if _, err := recipes.Embedded().Tree("full", "single", "v9", "", recipes.Data{Name: "shop-api", Module: "shop-api"}); err == nil {
		t.Error("Tree with an unknown layout error = nil")
	}
}

func TestPresets(t *testing.T) {
	if got := strings.Join(recipes.PresetNames(), ","); got != "minimal,full" {
		t.Errorf("PresetNames() = %s", got)
	}
	for _, tt := range []struct{ name, tenancy, recipe string }{
		{"minimal", "single", recipes.MinimalName},
		{"full", "single", recipes.FullName},
		{"full", "multi", recipes.FullMultiName},
	} {
		if p, ok := recipes.LookupPreset(tt.name, tt.tenancy); !ok || p.Recipe != tt.recipe || p.Tenancy != tt.tenancy {
			t.Errorf("LookupPreset(%q, %q) = %+v, %v", tt.name, tt.tenancy, p, ok)
		}
	}
	for _, tt := range []struct{ name, tenancy string }{{"custom", "single"}, {"minimal", "multi"}, {"full", "several"}} {
		if _, ok := recipes.LookupPreset(tt.name, tt.tenancy); ok {
			t.Errorf("LookupPreset(%q, %q) found a preset", tt.name, tt.tenancy)
		}
	}
	if !recipes.SupportsTenancy("full") || recipes.SupportsTenancy("minimal") {
		t.Error("SupportsTenancy() is wrong: only the Full preset has a multi-tenant variant")
	}
}

// TestTemplatesUpToDate fails when a v0.1 golden app changed but
// `go generate ./...` wasn't run. The v0.3.0 tree is generated from one
// golden app and rendered back over all three, so TestGoldenApps checks
// it from the other end, and CI's diff after go generate catches the
// rest.
func TestTemplatesUpToDate(t *testing.T) {
	for _, golden := range goldenApps {
		if golden.templates == "" {
			continue
		}
		t.Run(golden.templates, func(t *testing.T) {
			fresh := filepath.Join(t.TempDir(), golden.templates)
			if err := os.MkdirAll(filepath.Dir(fresh), 0o755); err != nil {
				t.Fatal(err)
			}
			var opts []generate.Option
			if golden.layout == recipes.LayoutV02 {
				opts = append(opts, generate.KeepLibraryLiterals())
			}
			// go generate records api/surface.json and tidies go.mod of the
			// golden app without its copied modules, which needs the go
			// command; CI's diff after go generate checks those two.
			src := unejected(t, golden.dir, golden.preset, golden.tenancy, golden.layout, golden.auth, golden.scope)
			skip := map[string]bool{}
			if src != golden.dir {
				skip = map[string]bool{"api/surface.json.tmpl": true, "go.mod.tmpl": true}
			} else if tracked := goldenFiles(t, golden.dir); tracked != nil {
				opts = append(opts, generate.OnlyFiles(tracked))
			}
			if err := generate.Run(src, fresh, opts...); err != nil {
				t.Fatalf("generate.Run() error = %v", err)
			}
			compareTrees(t, fresh, golden.templates, skip)
			compareTrees(t, golden.templates, fresh, skip)
		})
	}
}

func compareTrees(t *testing.T, a, b string, skip map[string]bool) {
	t.Helper()
	_ = filepath.WalkDir(a, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(a, p)
		if skip[filepath.ToSlash(rel)] {
			return nil
		}
		want, _ := os.ReadFile(p)
		got, err := os.ReadFile(filepath.Join(b, rel))
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("template %s is out of date; run: go generate ./... (in cli/)", rel)
		}
		return nil
	})
}
