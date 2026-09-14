// Package recipes renders the project templates used by aps new.
//
// The Minimal templates are generated from examples/minimal, the
// hand-written golden app, by `go generate`. Never edit them by hand.
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
	"strings"
	"text/template"
)

//go:embed all:minimal
var minimalFS embed.FS

// Recipe identity recorded in apistock.lock.
const (
	MinimalName = "base-minimal"
	// LibraryVersion is the apistock library version generated apps require.
	LibraryVersion = "v0.1.0"
)

// Data fills the templates.
type Data struct {
	Name           string // app name, for example "my-api"
	Module         string // Go module path
	LibraryVersion string
	// Local, when set, is a path to an apistock checkout used through
	// replace directives (development before a release is published).
	Local string
}

// A File is one rendered file.
type File struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// RenderMinimal writes the Minimal preset into root and returns the files
// written, sorted by path. Go files are validated with gofmt before writing.
func RenderMinimal(root *os.Root, d Data) ([]File, error) {
	return render(minimalFS, "minimal", root, d)
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
