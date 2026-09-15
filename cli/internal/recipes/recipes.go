// Package recipes renders the project templates used by aps new.
//
// Each preset's templates are generated from a hand-written golden app by
// `go generate`: minimal/ from examples/minimal and full/ from
// examples/full-single (ADR-0041). Never edit them by hand.
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
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"text/template"
)

//go:embed all:minimal
var minimalFS embed.FS

//go:embed all:full
var fullFS embed.FS

// Recipe identities recorded in apistock.lock.
const (
	MinimalName = "base-minimal"
	FullName    = "base-full"
	// LibraryVersion is the apistock library version generated apps require.
	LibraryVersion = "v0.1.0"
)

// A Preset is an app aps new can create.
type Preset struct {
	// Name is the value of aps new --preset.
	Name string
	// Recipe is the recipe name recorded in apistock.lock.
	Recipe string
	fsys   embed.FS
}

var presets = []Preset{
	{Name: "minimal", Recipe: MinimalName, fsys: minimalFS},
	{Name: "full", Recipe: FullName, fsys: fullFS},
}

// LookupPreset returns the preset aps new --preset name selects.
func LookupPreset(name string) (Preset, bool) {
	for _, p := range presets {
		if p.Name == name {
			return p, true
		}
	}
	return Preset{}, false
}

// PresetNames lists the presets aps new accepts, in the order it offers
// them.
func PresetNames() []string {
	names := make([]string, len(presets))
	for i, p := range presets {
		names[i] = p.Name
	}
	return names
}

// Render writes the preset into root and returns the files written, sorted
// by path. Go files are validated with gofmt before writing.
func (p Preset) Render(root *os.Root, d Data) ([]File, error) {
	return render(p.fsys, p.Name, root, d)
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

func render(fsys fs.FS, base string, root *os.Root, d Data) ([]File, error) {
	var files []File
	err := fs.WalkDir(fsys, base, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(p, base), "/")
		if rel == "" {
			return nil
		}
		if entry.IsDir() {
			return root.MkdirAll(rel, 0o755)
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
		if err := root.WriteFile(target, content, 0o644); err != nil {
			return fmt.Errorf("recipes: write %s: %w", target, err)
		}
		sum := sha256.Sum256(content)
		files = append(files, File{Path: target, SHA256: hex.EncodeToString(sum[:])})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}
