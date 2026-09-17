package cli

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// goSource is a parsed Go file orb rewrites by editing byte ranges, so
// everything it doesn't change stays as the developer wrote it, comments
// included (ADR-0021, and orb eject's import rewriting).
type goSource struct {
	name string
	src  []byte
	fset *token.FileSet
	file *ast.File
}

func parseGoSource(name string, src []byte) (*goSource, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}
	return &goSource{name: name, src: src, fset: fset, file: file}, nil
}

// offset returns p's byte offset in the source.
func (g *goSource) offset(p token.Pos) int { return g.fset.Position(p).Offset }

// text returns the source of n, byte for byte.
func (g *goSource) text(n ast.Node) string {
	return string(g.src[g.offset(n.Pos()):g.offset(n.End())])
}

// line returns n's line number, for messages.
func (g *goSource) line(n ast.Node) int { return g.fset.Position(n.Pos()).Line }

// imports returns the file's imports by the name they are used under.
func (g *goSource) imports() map[string]string {
	out := map[string]string{}
	for _, spec := range g.file.Imports {
		imp, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		name := packageName(imp)
		if spec.Name != nil {
			name = spec.Name.Name
		}
		out[name] = imp
	}
	return out
}

// packageName guesses the name a package is imported under from its path:
// its last element, or the one before a major version suffix (huma/v2 is
// imported as huma).
func packageName(imp string) string {
	base := path.Base(imp)
	if majorVersion.MatchString(base) {
		if dir := path.Dir(imp); dir != "." {
			return path.Base(dir)
		}
	}
	return base
}

// majorVersion matches a module path's major version element, such as v2.
var majorVersion = regexp.MustCompile(`^v[0-9]+$`)

// importedAs returns the name the file imports imp under, and whether it
// imports it at all.
func (g *goSource) importedAs(imp string) (string, bool) {
	for name, p := range g.imports() {
		if p == imp {
			return name, true
		}
	}
	return "", false
}

// isSelector reports whether e is pkg.name, where pkg is the file's name
// for the import path imp.
func (g *goSource) isSelector(e ast.Expr, imp, name string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return g.imports()[x.Name] == imp
}

// astObject is what go/parser resolves an identifier to inside one file,
// without type information: the conversion reads an app's own files, not
// the packages they import, so parser resolution is what it has.
type astObject = ast.Object //nolint:staticcheck // no type information: the conversion reads source, not packages

// srcEdit replaces the bytes in [start, end) with text.
type srcEdit struct {
	start, end int
	text       string
}

// applySrcEdits applies edits to src, last first, and formats the result.
// Overlapping edits are a bug in the caller and return an error.
func applySrcEdits(name string, src []byte, edits []srcEdit) ([]byte, error) {
	sorted := slices.Clone(edits)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].start < sorted[j].start })
	for i := 1; i < len(sorted); i++ {
		if sorted[i].start < sorted[i-1].end {
			return nil, fmt.Errorf("%s: overlapping edits at %d and %d", name, sorted[i-1].start, sorted[i].start)
		}
	}
	out := slices.Clone(src)
	for i := len(sorted) - 1; i >= 0; i-- {
		e := sorted[i]
		out = slices.Concat(out[:e.start], []byte(e.text), out[e.end:])
	}
	formatted, err := format.Source(out)
	if err != nil {
		return nil, fmt.Errorf("format %s after rewriting it: %w", name, err)
	}
	return formatted, nil
}

// importSpec is an import to add to a file.
type importSpec struct {
	name string // "" for the path's own name
	path string
}

// fixImports adds add's imports to src, drops imports nothing in the file
// uses any more, and formats it. Blank and dot imports are left alone.
func fixImports(name string, src []byte, appModule string, add []importSpec) ([]byte, error) {
	g, err := parseGoSource(name, src)
	if err != nil {
		return nil, err
	}
	used := usedPackageNames(g.file)
	var edits []srcEdit
	var groups [][]*ast.ImportSpec
	for _, decl := range g.file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.IMPORT {
			continue
		}
		var group []*ast.ImportSpec
		var lastLine int
		for _, spec := range gen.Specs {
			imp := spec.(*ast.ImportSpec)
			line := g.fset.Position(imp.Pos()).Line
			if lastLine != 0 && line > lastLine+1 && len(group) > 0 {
				groups = append(groups, group)
				group = nil
			}
			lastLine = g.fset.Position(imp.End()).Line
			group = append(group, imp)
		}
		if len(group) > 0 {
			groups = append(groups, group)
		}
	}
	for _, group := range groups {
		for _, spec := range group {
			p, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}
			local := packageName(p)
			if spec.Name != nil {
				local = spec.Name.Name
			}
			if local == "_" || local == "." || used[local] {
				continue
			}
			start, end := g.offset(spec.Pos()), g.offset(spec.End())
			// Take the whole line, including its comment and newline.
			for start > 0 && src[start-1] != '\n' {
				start--
			}
			for end < len(src) && src[end] != '\n' {
				end++
			}
			if end < len(src) {
				end++
			}
			edits = append(edits, srcEdit{start, end, ""})
		}
	}

	added := map[string]bool{}
	for _, spec := range add {
		if _, ok := g.importedAs(spec.path); ok || added[spec.path] {
			continue
		}
		added[spec.path] = true
		text := strconv.Quote(spec.path)
		if spec.name != "" && spec.name != packageName(spec.path) {
			text = spec.name + " " + text
		}
		at, before := importInsertion(g, groups, spec.path, appModule)
		if at < 0 {
			return nil, fmt.Errorf("%s has no import block to add %s to", name, spec.path)
		}
		if before {
			edits = append(edits, srcEdit{at, at, "\t" + text + "\n"})
		} else {
			edits = append(edits, srcEdit{at, at, "\n\t" + text + "\n"})
		}
	}
	out, err := applySrcEdits(name, src, edits)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// importGroup classifies an import path, so added imports join the group
// apps keep them in: the standard library, other modules, gorbital and the
// app's own packages.
func importGroup(imp, appModule string) int {
	switch {
	case appModule != "" && (imp == appModule || strings.HasPrefix(imp, appModule+"/")):
		return 3
	case imp == "gorbital.dev" || strings.HasPrefix(imp, "gorbital.dev/"):
		return 2
	case strings.Contains(strings.SplitN(imp, "/", 2)[0], "."):
		return 1
	}
	return 0
}

// importInsertion returns the offset to add imp at, and whether the text
// goes before that offset's line or starts a new group after it.
func importInsertion(g *goSource, groups [][]*ast.ImportSpec, imp, appModule string) (int, bool) {
	want := importGroup(imp, appModule)
	var last *ast.ImportSpec
	for _, group := range groups {
		if len(group) == 0 {
			continue
		}
		first, _ := strconv.Unquote(group[0].Path.Value)
		last = group[len(group)-1]
		if importGroup(first, appModule) != want {
			continue
		}
		for _, spec := range group {
			p, _ := strconv.Unquote(spec.Path.Value)
			if p > imp {
				start := g.offset(spec.Pos())
				for start > 0 && g.src[start-1] != '\n' {
					start--
				}
				return start, true
			}
		}
		end := g.offset(group[len(group)-1].End())
		for end < len(g.src) && g.src[end] != '\n' {
			end++
		}
		return end + 1, true
	}
	if last == nil {
		return -1, false
	}
	end := g.offset(last.End())
	for end < len(g.src) && g.src[end] != '\n' {
		end++
	}
	return end, false
}

// usedPackageNames returns the names the file uses as a package qualifier,
// such as huma in huma.Register.
func usedPackageNames(file *ast.File) map[string]bool {
	used := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if x, ok := sel.X.(*ast.Ident); ok && x.Obj == nil {
			used[x.Name] = true
		}
		return true
	})
	return used
}
