package cli

import (
	"fmt"
	"go/ast"
	"go/token"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// wiringTypeName is what the conversion renames a v0.1 module's Module type
// to, so the package can declare func Module() gorbital.Module, which the
// generated module list calls (ADR-0083).
const wiringTypeName = "Wiring"

// moduleConversion is one of the app's own modules moved to the v0.2
// layout: its Module value, its routes as gorbital routes, and what the
// conversion couldn't do.
type moduleConversion struct {
	// Name is the module's directory under internal/modules.
	Name string `json:"name"`
	// Files are the module's files as the conversion writes them, by
	// app-relative path.
	Files map[string][]byte `json:"-"`
	// Wiring is the internal/app file the module's declarations came from.
	Wiring string `json:"wiring,omitempty"`
	// Routes, Escaped and Public describe the converted operations.
	Routes  []string `json:"routes"`
	Escaped []string `json:"escaped,omitempty"`
	Public  []string `json:"public,omitempty"`
	// Errors and Permissions are what moved into the Module value.
	Errors      int      `json:"errors"`
	Permissions []string `json:"permissions,omitempty"`
	// Notes are what the developer should know about the module.
	Notes []string `json:"notes,omitempty"`
	// Problems say why the module can't be converted mechanically.
	Problems []string `json:"problems,omitempty"`
}

// convertModule converts one of the app's own modules (a module orb gen
// resource generated, or one written by hand) from the v0.1 layout: its
// routes become gorbital routes, the error mappings and permissions its
// internal/app wiring file declares move into a Module value, and the
// module's own Module type, if it has one, becomes Wiring.
func convertModule(name string, app appInfo, module, appPkg map[string][]byte, orgScoped bool) moduleConversion {
	c := moduleConversion{Name: name, Files: map[string][]byte{}}
	dir := "internal/modules/" + name + "/"
	code, tests := map[string][]byte{}, map[string][]byte{}
	for p, content := range module {
		switch {
		case !strings.HasSuffix(p, ".go"):
		case strings.HasSuffix(p, "_test.go"):
			tests[p] = content
		default:
			code[p] = content
		}
	}
	for p, content := range tests {
		if strings.Contains(string(content), strconv.Quote(humaImportPath)) {
			c.Problems = append(c.Problems, fmt.Sprintf("%s registers operations with huma; port it to gorbitaltest (see a new app's internal/modules/<module>/<module>_test.go) and convert again", p))
		}
	}

	routes, err := rewriteModuleRoutes(code, app.module)
	if err != nil {
		c.Problems = append(c.Problems, err.Error())
		return c
	}
	c.Routes, c.Escaped, c.Public = routes.Routes, routes.Escaped, routes.Public
	c.Problems = append(c.Problems, routes.Problems...)
	maps.Copy(c.Files, routes.Files)
	if len(routes.Routes)+len(routes.Escaped) == 0 && len(c.Problems) == 0 {
		c.Notes = append(c.Notes, "the module registers no HTTP operation")
	}

	// The module's own files, with the conversion's changes so far.
	current := map[string][]byte{}
	maps.Copy(current, module)
	maps.Copy(current, c.Files)

	wiring, wiringPath, err := findModuleWiring(name, app, current, appPkg, dir)
	if err != nil {
		c.Problems = append(c.Problems, err.Error())
		return c
	}
	c.Wiring = wiringPath

	renamed, renames, err := renameModuleType(app, name, current)
	if err != nil {
		c.Problems = append(c.Problems, err.Error())
		return c
	}
	maps.Copy(c.Files, renamed)
	maps.Copy(current, renamed)
	if renames {
		c.Notes = append(c.Notes, "the module's Module type is now "+wiringTypeName+", so the package can declare func Module() gorbital.Module")
	}

	value, err := buildModuleValue(name, app, wiring, current, orgScoped, &c)
	if err != nil {
		c.Problems = append(c.Problems, err.Error())
		return c
	}
	rootFile, content, err := addModuleFunc(name, app, current, value)
	if err != nil {
		c.Problems = append(c.Problems, err.Error())
		return c
	}
	c.Files[rootFile] = content
	sort.Strings(c.Problems)
	return c
}

// findModuleWiring returns the app's internal/app file that registers the
// module: the one that imports the module's root package, such as
// internal/app/module_projects.go.
func findModuleWiring(name string, app appInfo, module, appPkg map[string][]byte, dir string) (*goSource, string, error) {
	root := app.module + "/" + strings.TrimSuffix(dir, "/")
	var found []string
	for _, p := range slices.Sorted(maps.Keys(appPkg)) {
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			continue
		}
		if !strings.Contains(string(appPkg[p]), strconv.Quote(root)) {
			continue
		}
		// Other files may use the module, such as the seed command; the
		// wiring file is the one that registers its operations.
		g, err := parseGoSource(p, appPkg[p])
		if err != nil {
			return nil, "", err
		}
		if _, _, err := registerFunc(g); err != nil {
			continue
		}
		found = append(found, p)
	}
	switch len(found) {
	case 0:
		return nil, "", fmt.Errorf("no file in internal/app imports %s, so orb upgrade can't tell what the %s module declares; register it in internal/app as orb gen resource does, or convert it by hand", root, name)
	case 1:
	default:
		return nil, "", fmt.Errorf("%s import %s; orb upgrade reads one wiring file per module, as orb gen resource writes it", strings.Join(found, " and "), root)
	}
	g, err := parseGoSource(found[0], appPkg[found[0]])
	return g, found[0], err
}

// renameModuleType renames a v0.1 module's Module type to Wiring, in the
// module's own files and wherever the app names it, so the package can
// declare func Module() gorbital.Module.
func renameModuleType(app appInfo, name string, module map[string][]byte) (map[string][]byte, bool, error) {
	root := "internal/modules/" + name
	out := map[string][]byte{}
	found := false
	for _, p := range slices.Sorted(maps.Keys(module)) {
		if !strings.HasSuffix(p, ".go") || pathDir(p) != root {
			continue
		}
		g, err := parseGoSource(p, module[p])
		if err != nil {
			return nil, false, err
		}
		for _, decl := range g.file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok && ts.Name.Name == "Module" {
					found = true
				}
			}
		}
	}
	if !found {
		return out, false, nil
	}
	for _, p := range slices.Sorted(maps.Keys(module)) {
		if !strings.HasSuffix(p, ".go") || pathDir(p) != root {
			continue
		}
		content, err := renameIdent(p, module[p], "Module", wiringTypeName)
		if err != nil {
			return nil, false, err
		}
		if content != nil {
			out[p] = content
		}
	}
	return out, true, nil
}

// pathDir returns the directory of a slash-separated path.
func pathDir(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[:i]
	}
	return "."
}

// renameIdent renames a package-level identifier in one file: its
// declaration, every use in the file, and the same name in the file's doc
// comments of the declaration. Selectors (pkg.Module) are left alone.
func renameIdent(name string, src []byte, from, to string) ([]byte, error) {
	g, err := parseGoSource(name, src)
	if err != nil {
		return nil, err
	}
	unresolved := map[*ast.Ident]bool{}
	for _, ident := range g.file.Unresolved {
		unresolved[ident] = true
	}
	var edits []srcEdit
	var stack []ast.Node
	ast.Inspect(g.file, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		defer func() { stack = append(stack, n) }()
		ident, ok := n.(*ast.Ident)
		if !ok || ident.Name != from {
			return true
		}
		if len(stack) > 0 {
			switch parent := stack[len(stack)-1].(type) {
			case *ast.SelectorExpr:
				if parent.Sel == ident {
					return true // pkg.Module, renamed by the caller
				}
			case *ast.KeyValueExpr:
				if parent.Key == ident {
					return true // a field name in a literal
				}
			}
		}
		if ident.Obj == nil && !unresolved[ident] {
			return true
		}
		edits = append(edits, srcEdit{g.offset(ident.Pos()), g.offset(ident.End()), to})
		return true
	})
	// The declaration's doc comment starts with the name (Go doc style).
	for _, group := range g.file.Comments {
		for _, comment := range group.List {
			if strings.HasPrefix(comment.Text, "// "+from+" ") {
				start := g.offset(comment.Pos()) + len("// ")
				edits = append(edits, srcEdit{start, start + len(from), to})
			}
		}
	}
	if len(edits) == 0 {
		return nil, nil
	}
	return applySrcEdits(name, src, edits)
}
