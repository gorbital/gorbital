// Package generate converts a hand-written golden app into recipe templates:
// it replaces the placeholder module path and name, and derives the go.mod
// template from the golden go.mod (ADR-0041).
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
	"strings"
)

// Template delimiters.
const (
	LeftDelim  = "⟦"
	RightDelim = "⟧"
)

// Placeholders used by the golden apps.
const (
	PlaceholderModule = "example.com/acme-api"
	PlaceholderName   = "acme-api"
)

// libraryModule is the apistock library's module path; its modules are
// apistock.dev/modules/...
const libraryModule = "apistock.dev"

// repositoryPath starts a path from a golden app into the rest of the
// apistock repository, which generated apps don't have.
const repositoryPath = "../../"

// Skipped files are produced when an app is created, not copied.
var skipped = map[string]bool{"go.mod": true, "go.sum": true}

// SkippedDirs are build output and version control directories.
var SkippedDirs = map[string]bool{".aps": true, "bin": true, ".git": true}

// Skipped reports whether the golden app file at rel (slash-separated) is
// not turned into a template.
func Skipped(rel string) bool { return skipped[rel] }

// Run writes templates for the golden app at src into dst, replacing dst.
func Run(src, dst string) error {
	goMod, err := os.ReadFile(filepath.Join(src, "go.mod"))
	if err != nil {
		return err
	}
	goModTemplate, err := GoModTemplate(goMod)
	if err != nil {
		return fmt.Errorf("generate: %s: %w", filepath.Join(src, "go.mod"), err)
	}

	tmp, err := os.MkdirTemp(filepath.Dir(filepath.Clean(dst)), ".generate-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	if err := writeTemplates(src, tmp, goModTemplate); err != nil {
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
func writeTemplates(src, dst string, goModTemplate []byte) error {
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
		switch {
		case strings.Contains(text, LeftDelim) || strings.Contains(text, RightDelim):
			return fmt.Errorf("generate: %s contains the template delimiters %s %s", rel, LeftDelim, RightDelim)
		case strings.Contains(text, repositoryPath):
			return fmt.Errorf("generate: %s refers to %q, a path into the apistock repository that generated apps don't have", rel, repositoryPath)
		}
		text = strings.ReplaceAll(text, PlaceholderModule, LeftDelim+".Module"+RightDelim)
		text = strings.ReplaceAll(text, PlaceholderName, LeftDelim+".Name"+RightDelim)
		return dstRoot.WriteFile(rel+".tmpl", []byte(text), 0o644)
	})
	if err != nil {
		return err
	}
	return dstRoot.WriteFile("go.mod.tmpl", goModTemplate, 0o644)
}

// GoModTemplate turns a golden app's go.mod into the go.mod template. The
// module line takes the app's module path; apistock.dev requirements take
// the CLI's library version; the golden replace directives, with the comments
// and blank lines just before them, are dropped; and with a local checkout,
// every apistock.dev requirement is replaced by its directory in it.
//
// Other directives (exclude, retract, tool, godebug) are rejected, so a new
// kind of directive in a golden go.mod is handled on purpose rather than
// copied by accident.
func GoModTemplate(goMod []byte) ([]byte, error) {
	var (
		out       strings.Builder
		pending   []string // comments and blank lines, kept unless a replace follows
		libraries []string
		inRequire bool
		inReplace bool
		sawModule bool
	)
	emit := func(line string) {
		for _, p := range pending {
			out.WriteString(p + "\n")
		}
		pending = nil
		out.WriteString(line + "\n")
	}
	requirement := func(fields []string) (string, error) {
		if len(fields) < 2 {
			return "", fmt.Errorf("malformed requirement %q", strings.Join(fields, " "))
		}
		if fields[0] != libraryModule && !strings.HasPrefix(fields[0], libraryModule+"/") {
			return strings.Join(fields, " "), nil
		}
		libraries = append(libraries, fields[0])
		fields[1] = LeftDelim + ".LibraryVersion" + RightDelim
		return strings.Join(fields, " "), nil
	}

	for i, line := range strings.Split(strings.TrimRight(string(goMod), "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case inReplace:
			inReplace = trimmed != ")"
		case inRequire && trimmed == ")":
			inRequire = false
			emit(line)
		case inRequire && (trimmed == "" || strings.HasPrefix(trimmed, "//")):
			emit(line)
		case inRequire:
			req, err := requirement(strings.Fields(trimmed))
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", i+1, err)
			}
			emit("\t" + req)
		case trimmed == "" || strings.HasPrefix(trimmed, "//"):
			pending = append(pending, line)
		case trimmed == "replace (":
			inReplace, pending = true, nil
		case strings.HasPrefix(trimmed, "replace "):
			pending = nil
		case strings.HasPrefix(trimmed, "module "):
			if module := strings.TrimSpace(strings.TrimPrefix(trimmed, "module")); module != PlaceholderModule {
				return nil, fmt.Errorf("line %d: the module must be %s, got %s", i+1, PlaceholderModule, module)
			}
			sawModule = true
			emit("module " + LeftDelim + ".Module" + RightDelim)
		case strings.HasPrefix(trimmed, "go "), strings.HasPrefix(trimmed, "toolchain "):
			emit(line)
		case trimmed == "require (":
			inRequire = true
			emit(line)
		case strings.HasPrefix(trimmed, "require "):
			req, err := requirement(strings.Fields(strings.TrimPrefix(trimmed, "require ")))
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", i+1, err)
			}
			emit("require " + req)
		default:
			return nil, fmt.Errorf("line %d: unsupported directive %q; teach generate.GoModTemplate how to template it", i+1, trimmed)
		}
	}
	switch {
	case !sawModule:
		return nil, errors.New("no module line")
	case inRequire || inReplace:
		return nil, errors.New("unterminated block")
	case len(libraries) == 0:
		return nil, errors.New("no apistock.dev requirement")
	}

	out.WriteString(LeftDelim + "- if .Local" + RightDelim + "\n\nreplace (\n")
	for _, library := range libraries {
		dir := strings.TrimPrefix(library, libraryModule)
		fmt.Fprintf(&out, "\t%s => %s.LocalDir %q%s\n", library, LeftDelim, dir, RightDelim)
	}
	out.WriteString(")\n" + LeftDelim + "- end" + RightDelim + "\n")
	return []byte(out.String()), nil
}
