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

// goldenApps are the hand-written apps each preset is generated from, by
// template directory.
var goldenApps = []struct{ templates, preset, tenancy, layout, dir string }{
	{"minimal", "minimal", "single", recipes.LayoutV01, "../../../examples/minimal"},
	{"full", "full", "single", recipes.LayoutV01, "../../../examples/v0.1/full-single"},
	{"full-multi", "full", "multi", recipes.LayoutV01, "../../../examples/v0.1/full-multi"},
	{"v0.2/full", "full", "single", recipes.LayoutV02, "../../../examples/full-single"},
	{"v0.2/full-multi", "full", "multi", recipes.LayoutV02, "../../../examples/full-multi"},
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

// TestGoldenApps: rendering each preset with the placeholder name reproduces
// its hand-written golden app exactly, file for file.
func TestGoldenApps(t *testing.T) {
	for _, golden := range goldenApps {
		t.Run(golden.templates, func(t *testing.T) {
			dir, files := renderLayout(t, golden.preset, golden.tenancy, golden.layout, recipes.Data{
				Name:           generate.PlaceholderName,
				Module:         generate.PlaceholderModule,
				LibraryVersion: recipes.LibraryVersion,
			})
			tracked := goldenFiles(t, golden.dir)
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
		t.Run(golden.templates, func(t *testing.T) {
			local := "../.."
			if golden.layout == recipes.LayoutV01 && golden.preset == "full" {
				local = "../../.." // examples/v0.1/<app>
			}
			dir, _ := renderLayout(t, golden.preset, golden.tenancy, golden.layout, recipes.Data{
				Name: generate.PlaceholderName, Module: generate.PlaceholderModule, LibraryVersion: recipes.LibraryVersion, Local: local,
			})
			gotRequires, gotReplaces := parseGoMod(t, filepath.Join(dir, "go.mod"))
			wantRequires, wantReplaces := parseGoMod(t, filepath.Join(golden.dir, "go.mod"))
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

	dir, _ = renderInto(t, "full", "single", recipes.Data{Name: "shop-api", Module: "shop-api", LibraryVersion: "v0.1.0", Local: "/src/gorbital"})
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
	dir, _ := renderInto(t, "full", "multi", recipes.Data{Name: "shop-api", Module: "shop-api", LibraryVersion: "v0.1.0"})
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

// TestTemplatesUpToDate fails when a golden app changed but
// `go generate ./...` wasn't run.
func TestTemplatesUpToDate(t *testing.T) {
	for _, golden := range goldenApps {
		t.Run(golden.templates, func(t *testing.T) {
			fresh := filepath.Join(t.TempDir(), golden.templates)
			if err := os.MkdirAll(filepath.Dir(fresh), 0o755); err != nil {
				t.Fatal(err)
			}
			var opts []generate.Option
			if golden.layout == recipes.LayoutV02 {
				opts = append(opts, generate.KeepLibraryLiterals())
			}
			if tracked := goldenFiles(t, golden.dir); tracked != nil {
				opts = append(opts, generate.OnlyFiles(tracked))
			}
			if err := generate.Run(golden.dir, fresh, opts...); err != nil {
				t.Fatalf("generate.Run() error = %v", err)
			}
			compareTrees(t, fresh, golden.templates)
			compareTrees(t, golden.templates, fresh)
		})
	}
}

func compareTrees(t *testing.T, a, b string) {
	t.Helper()
	_ = filepath.WalkDir(a, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(a, p)
		want, _ := os.ReadFile(p)
		got, err := os.ReadFile(filepath.Join(b, rel))
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("template %s is out of date; run: go generate ./... (in cli/)", rel)
		}
		return nil
	})
}
