// Package generate converts the hand-written golden app into recipe
// templates by replacing its placeholder name and module path.
//
// Templates use ⟦ and ⟧ as delimiters, because Go source routinely contains
// "{{" and "}}" (for example nested composite literals).
package generate

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Template delimiters.
const (
	LeftDelim  = "⟦"
	RightDelim = "⟧"
)

// Placeholders used by the golden app.
const (
	PlaceholderModule = "example.com/acme-api"
	PlaceholderName   = "acme-api"
)

// Skipped files are produced when an app is created, not copied.
var skipped = map[string]bool{"go.mod": true, "go.sum": true}

// SkippedDirs are build output and version control directories.
var SkippedDirs = map[string]bool{".aps": true, "bin": true, ".git": true}

const goModTemplate = `module ⟦.Module⟧

go 1.26.0

require (
	apistock.dev ⟦.LibraryVersion⟧
	apistock.dev/modules/openapi ⟦.LibraryVersion⟧
	apistock.dev/modules/telemetry ⟦.LibraryVersion⟧
	github.com/danielgtaylor/huma/v2 %s
)
⟦- if .Local⟧

replace (
	apistock.dev => ⟦.Local⟧
	apistock.dev/modules/openapi => ⟦.Local⟧/modules/openapi
	apistock.dev/modules/telemetry => ⟦.Local⟧/modules/telemetry
)
⟦- end⟧
`

// Skipped reports whether the golden app file at rel (slash-separated) is
// not turned into a template.
func Skipped(rel string) bool { return skipped[rel] }

// Run writes templates for the golden app at src into dst, replacing dst.
func Run(src, dst string) error {
	humaVersion, err := requiredVersion(filepath.Join(src, "go.mod"), "github.com/danielgtaylor/huma/v2")
	if err != nil {
		return err
	}

	tmp, err := os.MkdirTemp(filepath.Dir(filepath.Clean(dst)), ".generate-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	if err := writeTemplates(src, tmp, humaVersion); err != nil {
		return err
	}

	// Replace dst only after every template was written, so a failure never
	// leaves partial templates behind.
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// writeTemplates reads the golden app and writes templates through os.Root,
// so symlinks can't redirect reads or writes outside src and dst.
func writeTemplates(src, dst, humaVersion string) error {
	srcRoot, err := os.OpenRoot(src)
	if err != nil {
		return err
	}
	defer srcRoot.Close()
	dstRoot, err := os.OpenRoot(dst)
	if err != nil {
		return err
	}
	defer dstRoot.Close()

	err = fs.WalkDir(srcRoot.FS(), ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil || rel == "." {
			return err
		}
		if d.IsDir() {
			if SkippedDirs[d.Name()] {
				return fs.SkipDir
			}
			return dstRoot.MkdirAll(rel, 0o755)
		}
		if Skipped(rel) {
			return nil
		}
		content, err := srcRoot.ReadFile(rel)
		if err != nil {
			return err
		}
		text := string(content)
		if strings.Contains(text, LeftDelim) || strings.Contains(text, RightDelim) {
			return fmt.Errorf("generate: %s contains the template delimiters %s %s", rel, LeftDelim, RightDelim)
		}
		text = strings.ReplaceAll(text, PlaceholderModule, LeftDelim+".Module"+RightDelim)
		text = strings.ReplaceAll(text, PlaceholderName, LeftDelim+".Name"+RightDelim)
		return dstRoot.WriteFile(rel+".tmpl", []byte(text), 0o644)
	})
	if err != nil {
		return err
	}
	return dstRoot.WriteFile("go.mod.tmpl", fmt.Appendf(nil, goModTemplate, humaVersion), 0o644)
}

func requiredVersion(goMod, module string) (string, error) {
	b, err := os.ReadFile(goMod)
	if err != nil {
		return "", err
	}
	m := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(module) + `\s+(v\S+)`).FindSubmatch(b)
	if m == nil {
		return "", errors.New("generate: " + module + " is not required in " + goMod)
	}
	return string(m[1]), nil
}
