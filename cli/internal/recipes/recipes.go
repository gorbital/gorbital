// Package recipes renders the project templates used by orb new.
//
// Each preset's templates are generated from a hand-written golden app by
// `go generate`: minimal/ from examples/minimal; v0.2/full/ and
// v0.2/full-multi/, the layout orb new writes (apps on gorbital.Main,
// ADR-0083), from examples/full-single and examples/full-multi; and full/
// and full-multi/, the v0.1 layout that v0.1 apps keep, from
// examples/v0.1/full-single and examples/v0.1/full-multi (ADR-0041,
// ADR-0048). Never edit them by hand.
package recipes

//go:generate go run ./gen

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"go/format"
	"io/fs"
	"maps"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"text/template"
)

// templatesFS holds this directory's preset and email templates, laid out
// like an older release's cli/internal/recipes (ADR-0050).
//
//go:embed all:minimal all:full all:full-multi all:v0.2 mail/*.tmpl
var templatesFS embed.FS

// Recipe identities: the names of the preset template trees.
const (
	MinimalName   = "base-minimal"
	FullName      = "base-full"
	FullMultiName = "base-full-multi"
	// LibraryVersion is the gorbital library version generated apps require.
	LibraryVersion = "v0.2.0"
)

// App layouts. A layout is how an app's code is organised, and which
// templates wrote it: orb upgrade and orb add rebuild and merge an app from
// the templates of its own layout.
const (
	// LayoutV01 is the layout of v0.1: a composition root in internal/app
	// wiring generated modules, with cmd/migrate and cmd/seed. Minimal apps
	// keep it (roadmap decision D17). gorbital.lock records it as no layout.
	LayoutV01 = "v0.1"
	// LayoutV02 is the layout of v0.2: cmd/api/main.go on gorbital.Main, the
	// app's modules in internal/modules and its migrations in db/migrations
	// (ADR-0083). orb new writes it for the Full preset.
	LayoutV02 = "v0.2"
)

// Tenancy values of orb new --tenancy (ADR-0023).
const (
	TenancySingle = "single"
	TenancyMulti  = "multi"
)

// A Preset is an app orb new can create.
type Preset struct {
	// Name is the value of orb new --preset.
	Name string
	// Tenancy is the value of orb new --tenancy: single, or multi for
	// organisations.
	Tenancy string
	// Recipe names the preset's template tree.
	Recipe string
	// dir is the v0.1 layout's tree, in the same directory in every
	// release.
	dir string
	// mainDir is the v0.2 layout's tree, in releases from v0.2 on; "" for
	// presets without one.
	mainDir string
}

var presets = []Preset{
	{Name: "minimal", Tenancy: TenancySingle, Recipe: MinimalName, dir: "minimal"},
	{Name: "full", Tenancy: TenancySingle, Recipe: FullName, dir: "full", mainDir: "v0.2/full"},
	{Name: "full", Tenancy: TenancyMulti, Recipe: FullMultiName, dir: "full-multi", mainDir: "v0.2/full-multi"},
}

// Layout returns the layout orb new writes for the preset: LayoutV02 for
// the Full preset, LayoutV01 for Minimal, which keeps composing core
// packages directly.
func (p Preset) Layout() string {
	if p.mainDir != "" {
		return LayoutV02
	}
	return LayoutV01
}

// treeDir returns the directory of the preset's templates in layout.
func (p Preset) treeDir(layout string) (string, error) {
	switch {
	case layout == LayoutV01:
		return p.dir, nil
	case layout == LayoutV02 && p.mainDir != "":
		return p.mainDir, nil
	case layout == LayoutV02:
		return "", fmt.Errorf("recipes: the %s preset has no %s layout", p.Name, LayoutV02)
	}
	return "", fmt.Errorf("recipes: unknown layout %q (want %s or %s)", layout, LayoutV01, LayoutV02)
}

// LookupPreset returns the preset orb new --preset name --tenancy tenancy
// selects.
func LookupPreset(name, tenancy string) (Preset, bool) {
	for _, p := range presets {
		if p.Name == name && p.Tenancy == tenancy {
			return p, true
		}
	}
	return Preset{}, false
}

// SupportsTenancy reports whether the preset name has a multi-tenant
// variant, so orb new asks about tenancy.
func SupportsTenancy(name string) bool {
	_, ok := LookupPreset(name, TenancyMulti)
	return ok
}

// PresetNames lists the presets orb new accepts, in the order it offers
// them.
func PresetNames() []string {
	var names []string
	for _, p := range presets {
		if !slices.Contains(names, p.Name) {
			names = append(names, p.Name)
		}
	}
	return names
}

// Render writes the preset, in the layout orb new writes (Layout), into
// root and returns the files written, sorted by path. Go files are
// validated with gofmt before writing.
func (p Preset) Render(root *os.Root, d Data) ([]File, error) {
	dir, err := p.treeDir(p.Layout())
	if err != nil {
		return nil, err
	}
	tree, err := renderTree(templatesFS, dir, d)
	if err != nil {
		return nil, err
	}
	return writeTree(root, tree)
}

// Data fills the templates.
type Data struct {
	Name           string // app name, for example "my-api"
	Module         string // Go module path
	LibraryVersion string
	// Local, when set, is a path to a gorbital checkout used through
	// replace directives (development before a release is published).
	Local string
}

// LocalDir is the directory of a library module in the local checkout, as a
// go.mod path: dir is "" for the core module or "/modules/<name>". Paths go.mod
// can't hold bare, such as ones with spaces, are quoted.
func (d Data) LocalDir(dir string) string {
	p := d.Local + dir
	if p == "" || strings.ContainsAny(p, " \t\r\n\"'`\\") || strings.Contains(p, "//") || strings.Contains(p, "/*") {
		return strconv.Quote(p)
	}
	return p
}

// A File is one rendered file.
type File struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// renderTree renders the templates under base in fsys, keyed by the
// slash-separated path of each file they produce.
func renderTree(fsys fs.FS, base string, d Data) (map[string][]byte, error) {
	tree := map[string][]byte{}
	err := fs.WalkDir(fsys, base, func(p string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel := strings.TrimPrefix(p, base+"/")

		src, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		tmpl, err := template.New(rel).Delims("⟦", "⟧").Option("missingkey=error").Parse(string(src))
		if err != nil {
			return fmt.Errorf("recipes: parse %s: %w", rel, err)
		}
		var out bytes.Buffer
		if err := tmpl.Execute(&out, d); err != nil {
			return fmt.Errorf("recipes: render %s: %w", rel, err)
		}

		target := strings.TrimSuffix(rel, ".tmpl")
		content := out.Bytes()
		if path.Ext(target) == ".go" {
			formatted, err := format.Source(content)
			if err != nil {
				return fmt.Errorf("recipes: %s is not valid Go after rendering: %w", target, err)
			}
			content = formatted
		}
		tree[target] = content
		return nil
	})
	if err != nil {
		return nil, err
	}
	return tree, nil
}

// writeTree writes tree into root and returns the files written, sorted by
// path.
func writeTree(root *os.Root, tree map[string][]byte) ([]File, error) {
	files := make([]File, 0, len(tree))
	for _, p := range slices.Sorted(maps.Keys(tree)) {
		if dir := path.Dir(p); dir != "." {
			if err := root.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("recipes: create %s: %w", dir, err)
			}
		}
		if err := root.WriteFile(p, tree[p], 0o644); err != nil {
			return nil, fmt.Errorf("recipes: write %s: %w", p, err)
		}
		sum := sha256.Sum256(tree[p])
		files = append(files, File{Path: p, SHA256: hex.EncodeToString(sum[:])})
	}
	return files, nil
}
