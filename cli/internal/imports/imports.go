// Package imports changes the import paths of Go files, keeping everything
// else byte for byte. orb eject uses it to point an app at its copy of a
// built-in module, and go generate to point a golden app's copy back at the
// library (ADR-0083).
package imports

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"path"
	"slices"
	"strconv"
	"strings"
)

// A Mapping returns the path an import moves to and the name of the package
// found there, or ok false to leave the import as it is. name is empty when
// the package's name is the last element of to.
type Mapping func(imp string) (to, name string, ok bool)

// Rewrite changes src's imports that m maps, then formats it. The import
// gets the package's name when that differs from its new path's last
// element, and loses a name that the new path makes redundant, so the file
// keeps compiling and reads the same; and it moves to the group of imports
// it belongs with, such as the app's own. changed is false, and src is returned
// as it is, when m maps none of src's imports.
func Rewrite(filename string, src []byte, m Mapping) (out []byte, changed bool, err error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.ImportsOnly|parser.ParseComments)
	if err != nil {
		return nil, false, fmt.Errorf("parse %s: %w", filename, err)
	}
	type edit struct {
		start, end int
		text       string
	}
	var edits []edit
	moved := map[string]bool{}
	for _, spec := range file.Imports {
		imp, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		to, name, ok := m(imp)
		if !ok {
			continue
		}
		start := fset.Position(spec.Path.Pos()).Offset
		text := strconv.Quote(to)
		switch {
		case spec.Name == nil && name != "" && name != path.Base(to):
			text = name + " " + text
		case spec.Name != nil && spec.Name.Name == name && name == path.Base(to):
			start = fset.Position(spec.Name.Pos()).Offset
		}
		edits = append(edits, edit{start, fset.Position(spec.Path.End()).Offset, text})
		moved[to] = true
	}
	if len(edits) == 0 {
		return src, false, nil
	}
	out = slices.Clone(src)
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		out = slices.Concat(out[:e.start], []byte(e.text), out[e.end:])
	}
	formatted, err := format.Source(out)
	if err != nil {
		return nil, false, fmt.Errorf("format %s: %w", filename, err)
	}
	if formatted, err = regroup(filename, formatted, moved); err != nil {
		return nil, false, err
	}
	return formatted, true, nil
}

// regroup moves each import of src whose path is in moved into the group of
// imports (separated by blank lines) whose paths it shares the longest
// start with, or else to a group of its kind as goimports sees it (see
// affinity), such as an app's copy of a library package into the group of
// the app's own imports, and back, then formats src again. An import stays
// where it is when no other group suits it better.
func regroup(filename string, src []byte, moved map[string]bool) ([]byte, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.ImportsOnly|parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", filename, err)
	}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.IMPORT || !gen.Lparen.IsValid() {
			continue
		}
		// Group the specs: a blank line starts a group.
		type item struct {
			path       string
			group      int
			start, end int // the spec's whole lines, as byte offsets
		}
		var items []item
		group, lastLine := 0, 0
		for _, spec := range gen.Specs {
			is := spec.(*ast.ImportSpec)
			imp, _ := strconv.Unquote(is.Path.Value)
			first := is.Pos()
			if is.Doc != nil {
				first = is.Doc.Pos()
			}
			line := fset.Position(first).Line
			if lastLine != 0 && line > lastLine+1 {
				group++
			}
			lastLine = fset.Position(is.End()).Line
			if is.Comment != nil {
				lastLine = fset.Position(is.Comment.End()).Line
			}
			start := fset.Position(first).Offset - (fset.Position(first).Column - 1)
			end := bytes.IndexByte(src[fset.Position(is.End()).Offset:], '\n') + fset.Position(is.End()).Offset + 1
			if is.Comment != nil {
				end = bytes.IndexByte(src[fset.Position(is.Comment.End()).Offset:], '\n') + fset.Position(is.Comment.End()).Offset + 1
			}
			items = append(items, item{imp, group, start, end})
		}
		for i, it := range items {
			if !moved[it.path] {
				continue
			}
			score := make([]int, group+1)
			// Imports still to move don't say where a group belongs.
			for _, other := range items {
				if !moved[other.path] {
					score[other.group] = max(score[other.group], affinity(it.path, other.path))
				}
			}
			best := it.group
			for g, sc := range score {
				if sc > score[best] {
					best = g
				}
			}
			if best == it.group {
				continue
			}
			// Insert the spec's lines after the last spec of the group,
			// then remove them where they were; format sorts the group.
			last := -1
			for j, other := range items {
				if other.group == best && j != i {
					last = j
				}
			}
			line := slices.Clone(src[it.start:it.end])
			at := items[last].end
			var out []byte
			if at < it.start {
				out = slices.Concat(src[:at], line, src[at:it.start], src[it.end:])
			} else {
				out = slices.Concat(src[:it.start], src[it.end:at], line, src[at:])
			}
			formatted, err := format.Source(out)
			if err != nil {
				return nil, fmt.Errorf("format %s: %w", filename, err)
			}
			// Offsets changed: start again with the file as it is now.
			delete(moved, it.path)
			return regroup(filename, formatted, moved)
		}
	}
	return src, nil
}

// affinity says how much import a belongs with import b: most for the
// path elements they start with, then for being the same kind of path as
// goimports groups them, standard library (no dot in the first element,
// such as an app module named shop) or not.
func affinity(a, b string) int {
	n := 2 * sharedElements(a, b)
	if isStandard(a) == isStandard(b) {
		n++
	}
	return n
}

// isStandard reports whether goimports takes imp for the standard library.
func isStandard(imp string) bool {
	first, _, _ := strings.Cut(imp, "/")
	return !strings.Contains(first, ".")
}

// sharedElements counts the path elements a and b start with.
func sharedElements(a, b string) int {
	as, bs := strings.Split(a, "/"), strings.Split(b, "/")
	n := 0
	for n < len(as) && n < len(bs) && as[n] == bs[n] {
		n++
	}
	return n
}
