// Package recipes renders the project templates used by aps new.
//
// Each preset's templates are generated from a hand-written golden app by
// `go generate`: minimal/ from examples/minimal, full/ from
// examples/full-single and full-multi/ from examples/full-multi (ADR-0041,
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
//go:embed all:minimal all:full all:full-multi mail/*.tmpl
var templatesFS embed.FS

// Recipe identities: the names of the preset template trees.
const (
	MinimalName   = "base-minimal"
	FullName      = "base-full"
	FullMultiName = "base-full-multi"
	// LibraryVersion is the apistock library version generated apps require.
	LibraryVersion = "v0.1.0"
)

// Tenancy values of aps new --tenancy (ADR-0023).
const (
	TenancySingle = "single"
	TenancyMulti  = "multi"
)

// A Preset is an app aps new can create.
type Preset struct {
	// Name is the value of aps new --preset.
	Name string
	// Tenancy is the value of aps new --tenancy: single, or multi for
	// organisations.
	Tenancy string
	// Recipe names the preset's template tree.
	Recipe string
	// dir is the tree's directory in every release since v0.2.0.
	dir string
}

var presets = []Preset{
	{Name: "minimal", Tenancy: TenancySingle, Recipe: MinimalName, dir: "minimal"},
	{Name: "full", Tenancy: TenancySingle, Recipe: FullName, dir: "full"},
	{Name: "full", Tenancy: TenancyMulti, Recipe: FullMultiName, dir: "full-multi"},
}

// LookupPreset returns the preset aps new --preset name --tenancy tenancy
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
// variant, so aps new asks about tenancy.
func SupportsTenancy(name string) bool {
	_, ok := LookupPreset(name, TenancyMulti)
	return ok
}

// PresetNames lists the presets aps new accepts, in the order it offers
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

// Render writes the preset into root and returns the files written, sorted
// by path. Go files are validated with gofmt before writing.
func (p Preset) Render(root *os.Root, d Data) ([]File, error) {
	tree, err := renderTree(templatesFS, p.dir, d)
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
	// Local, when set, is a path to an apistock checkout used through
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
