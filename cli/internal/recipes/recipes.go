// Package recipes renders the project templates used by orb new.
//
// Each tree of templates is generated from a hand-written golden app by
// `go generate`: minimal/ from examples/minimal; v0.2.2/, the layout orb
// new writes (apps on gorbital.Main, ADR-0083), from examples/full-multi;
// and full/ and full-multi/, the v0.1 layout that v0.1 apps keep, from
// examples/v0.1/full-single and examples/v0.1/full-multi (ADR-0041,
// ADR-0048). Never edit them by hand — except the hand-written part of
// v0.2.2/, which lives in gen/tree/ and is copied over the generated one.
//
// One tree serves every shape: manifest.yaml says which features each
// path belongs to, and Profile.Render writes the ones the app asked for
// (ADR-0090).
//
// The golden Full apps show what orb new writes, sign-in and organisations
// included (v0.2.1); the templates hold neither, and orb new copies
// them in from the library version the app requires (Profile.Copies). go
// generate takes them out of the golden apps and puts them back from the
// checkout (./gen).
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
//go:embed all:minimal all:full all:full-multi all:v0.2.2 mail/*.tmpl
var templatesFS embed.FS

// Recipe identities: the names of the preset template trees.
const (
	MinimalName   = "base-minimal"
	FullName      = "base-full"
	FullMultiName = "base-full-multi"
	// LibraryVersion is the gorbital library version generated apps require.
	LibraryVersion = "v0.2.1"
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

// An EjectedModule is a built-in module of gorbital.dev/gorbital that orb
// new copies into an app as the app's own code, as orb eject does.
type EjectedModule struct {
	// Name is the module's directory under internal/modules, and what orb
	// eject takes, such as auth.
	Name string
	// Package is the library package it is copied from, such as
	// gorbital.dev/gorbital/authhttp.
	Package string
}

// Dir returns the module's directory in the app, slash-separated.
func (m EjectedModule) Dir() string { return "internal/modules/" + m.Name }

// Ejects returns the built-in modules orb copies into an app of the
// preset, in the order it copies them. orb new works from a Profile
// instead (Profile.Copies); this stays for orb upgrade --layout v0.2 and
// orb add orgs, which rebuild an app from what its lock records: since v0.2.1 a Full app holds its
// sign-in, and a multi-tenant one its organisations, in internal/modules
// (orb new --no-eject keeps them in the library). Organisations come first:
// the library's orgshttp takes the library's sign-in, so sign-in can only
// be copied once organisations are. Templates hold neither: the golden apps
// show them copied, and go generate takes them out again (generate.Uneject).
func (p Preset) Ejects() []EjectedModule {
	if p.Layout() != LayoutV02 {
		return nil
	}
	auth := EjectedModule{Name: "auth", Package: "gorbital.dev/gorbital/authhttp"}
	if p.Tenancy == TenancyMulti {
		return []EjectedModule{{Name: "orgs", Package: "gorbital.dev/gorbital/orgshttp"}, auth}
	}
	return []EjectedModule{auth}
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
	// Profile is the app's shape, which the v0.2.2 tree's conditional
	// templates read (ADR-0090 §6). It is the zero Profile for the v0.1
	// trees, which have no conditionals of their own.
	Profile Profile
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
	return renderTreeFunc(fsys, base, d, func(string) bool { return true })
}

// renderTreeFunc renders the templates under base that keep reports, keyed
// by the slash-separated path of each file they produce. keep takes the
// template's path relative to base.
func renderTreeFunc(fsys fs.FS, base string, d Data, keep func(rel string) bool) (map[string][]byte, error) {
	tree := map[string][]byte{}
	err := fs.WalkDir(fsys, base, func(p string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel := strings.TrimPrefix(p, base+"/")
		if !keep(rel) {
			return nil
		}

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
