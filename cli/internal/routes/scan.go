package routes

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
)

// Import paths the scan recognises.
const (
	gorbitalPath = "gorbital.dev/gorbital"
	humaPath     = "github.com/danielgtaylor/huma/v2"
)

// verbs are gorbital's route functions and their methods.
var verbs = map[string]string{
	"Get": http.MethodGet, "Post": http.MethodPost, "Put": http.MethodPut,
	"Patch": http.MethodPatch, "Delete": http.MethodDelete,
}

// found is one route registration in the source.
type found struct {
	method, path, operationID string
	module, handler           string
	pos, handlerPos           *Pos
	middleware                []string
}

// Found are the registrations Scan found.
type Found struct {
	byRoute map[string]found // method and path
	byID    map[string]found // literal operation IDs
}

func (f Found) match(method, path, operationID string) (found, bool) {
	if s, ok := f.byRoute[method+" "+path]; ok {
		return s, true
	}
	if operationID != "" {
		if s, ok := f.byID[operationID]; ok {
			return s, true
		}
	}
	return found{}, false
}

// skippedDirs aren't the app's own Go source.
var skippedDirs = map[string]bool{"vendor": true, "testdata": true, "node_modules": true, "bin": true}

// Scan finds route registrations in the Go files of the app in dir (tests
// excluded): gorbital.Get, Post, Put, Patch and Delete calls, resolving the
// prefixes of Router.Group variables in the same function, and huma.Register
// calls with a huma.Operation literal (the v0.1 layout).
func Scan(dir string) (Found, error) {
	f := Found{byRoute: map[string]found{}, byID: map[string]found{}}
	s := &scanner{dir: dir, fset: token.NewFileSet(), packages: map[string]*pkg{}}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != dir && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || skippedDirs[name]) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		return s.parse(path)
	})
	if err != nil {
		return f, err
	}
	for _, p := range s.packages {
		for _, file := range p.files {
			for _, r := range s.registrations(p, file) {
				key := r.method + " " + r.path
				if _, dup := f.byRoute[key]; !dup && r.path != "" {
					f.byRoute[key] = r
				}
				if r.operationID != "" {
					f.byID[r.operationID] = r
				}
			}
		}
	}
	return f, nil
}

// pkg is the parsed files of one directory.
type pkg struct {
	dir   string // slash-separated, relative to the app
	files []*ast.File
	// funcs are the package's functions and methods by name, for handler
	// positions; a name declared twice (methods of two types) has none.
	funcs map[string][]token.Pos
	// moduleName is the Name of a gorbital.Module literal in the package,
	// and moduleMiddleware its Middleware.
	moduleName       string
	moduleMiddleware []string
}

type scanner struct {
	dir      string
	fset     *token.FileSet
	packages map[string]*pkg
}

func (s *scanner) parse(path string) error {
	file, err := parser.ParseFile(s.fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil //nolint:nilerr // a file that doesn't parse doesn't build either; the export reports it
	}
	rel, _ := filepath.Rel(s.dir, filepath.Dir(path))
	rel = filepath.ToSlash(rel)
	p := s.packages[rel]
	if p == nil {
		p = &pkg{dir: rel, funcs: map[string][]token.Pos{}}
		s.packages[rel] = p
	}
	p.files = append(p.files, file)
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			p.funcs[fn.Name.Name] = append(p.funcs[fn.Name.Name], fn.Name.Pos())
			if recv := receiverType(fn); recv != "" {
				p.funcs[recv+"."+fn.Name.Name] = append(p.funcs[recv+"."+fn.Name.Name], fn.Name.Pos())
			}
		}
	}
	if local := importName(file, gorbitalPath, "gorbital"); local != "" {
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok || !isSelector(lit.Type, local, "Module") {
				return true
			}
			for _, elt := range lit.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				switch key, _ := kv.Key.(*ast.Ident); {
				case key == nil:
				case key.Name == "Name":
					if v, ok := stringLit(kv.Value); ok {
						p.moduleName = v
					}
				case key.Name == "Middleware":
					if list, ok := kv.Value.(*ast.CompositeLit); ok {
						for _, e := range list.Elts {
							p.moduleMiddleware = append(p.moduleMiddleware, s.text(e))
						}
					}
				}
			}
			return true
		})
	}
	return nil
}

// module returns the module of the package in dir: the directory under
// internal/modules, named by its gorbital.Module when it declares one.
func (s *scanner) module(dir string) (name string, middleware []string) {
	rest, ok := strings.CutPrefix(dir, "internal/modules/")
	if !ok {
		if p := s.packages[dir]; p != nil && p.moduleName != "" {
			return p.moduleName, p.moduleMiddleware
		}
		return "", nil
	}
	top, _, _ := strings.Cut(rest, "/")
	name = top
	if p := s.packages["internal/modules/"+top]; p != nil {
		if p.moduleName != "" {
			name = p.moduleName
		}
		middleware = p.moduleMiddleware
	}
	return name, middleware
}

// group is a Router's prefix and options, from r.Group calls.
type group struct {
	prefix string
	known  bool // false when a prefix isn't a string literal
	use    []string
}

// registrations returns the routes file registers.
func (s *scanner) registrations(p *pkg, file *ast.File) []found {
	g := importName(file, gorbitalPath, "gorbital")
	h := importName(file, humaPath, "huma")
	if g == "" && h == "" {
		return nil
	}
	moduleName, moduleMiddleware := s.module(p.dir)
	var out []found
	for _, decl := range file.Decls {
		var bodies []*ast.BlockStmt
		ast.Inspect(decl, func(n ast.Node) bool {
			switch fn := n.(type) {
			case *ast.FuncDecl:
				if fn.Body != nil {
					bodies = append(bodies, fn.Body)
				}
			case *ast.FuncLit:
				bodies = append(bodies, fn.Body)
			}
			return true
		})
		for _, body := range bodies {
			groups := map[string]group{}
			types := map[string]string{} // variable → the type of its composite literal
			ast.Inspect(body, func(n ast.Node) bool {
				if _, nested := n.(*ast.FuncLit); nested {
					return false // its own body, with its own variables
				}
				switch n := n.(type) {
				case *ast.AssignStmt:
					if g != "" && len(n.Lhs) == 1 && len(n.Rhs) == 1 {
						if id, ok := n.Lhs[0].(*ast.Ident); ok {
							if grp, ok := s.group(n.Rhs[0], groups, g); ok {
								groups[id.Name] = grp
							}
						}
					}
					if len(n.Lhs) == 1 && len(n.Rhs) == 1 {
						if id, ok := n.Lhs[0].(*ast.Ident); ok {
							if t := literalType(n.Rhs[0]); t != "" {
								types[id.Name] = t
							}
						}
					}
				case *ast.CallExpr:
					if r, ok := s.gorbitalRoute(n, groups, g); ok {
						r.module, r.middleware = moduleName, append(append([]string{}, moduleMiddleware...), r.middleware...)
						r.handlerPos = s.handlerPos(p, n.Args[2], types)
						out = append(out, r)
					} else if r, ok := s.humaRoute(n, h); ok {
						r.module = moduleName
						r.handlerPos = s.handlerPos(p, n.Args[2], types)
						out = append(out, r)
					}
				}
				return true
			})
		}
	}
	return out
}

// group resolves e when it is a Router.Group call.
func (s *scanner) group(e ast.Expr, groups map[string]group, g string) (group, bool) {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return group{}, false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Group" || len(call.Args) == 0 {
		return group{}, false
	}
	parent := s.router(sel.X, groups, g)
	prefix, known := stringLit(call.Args[0])
	return group{
		prefix: parent.prefix + prefix,
		known:  parent.known && known,
		use:    append(append([]string{}, parent.use...), s.uses(call.Args[1:], g)...),
	}, true
}

// router resolves the Router expression of a route or group: a group
// variable, an inline Group call, or the module's Router (no prefix).
func (s *scanner) router(e ast.Expr, groups map[string]group, g string) group {
	if id, ok := e.(*ast.Ident); ok {
		if grp, ok := groups[id.Name]; ok {
			return grp
		}
	}
	if grp, ok := s.group(e, groups, g); ok {
		return grp
	}
	return group{known: true}
}

// uses returns the middleware expressions of gorbital.Use options.
func (s *scanner) uses(opts []ast.Expr, g string) []string {
	var out []string
	for _, o := range opts {
		call, ok := o.(*ast.CallExpr)
		if !ok || !isSelector(call.Fun, g, "Use") {
			continue
		}
		for _, a := range call.Args {
			out = append(out, s.text(a))
		}
	}
	return out
}

// gorbitalRoute resolves a gorbital.Get (Post, …) call.
func (s *scanner) gorbitalRoute(call *ast.CallExpr, groups map[string]group, g string) (found, bool) {
	if g == "" || len(call.Args) < 3 {
		return found{}, false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		// gorbital.Get[I, O](…) with explicit type arguments.
		if idx, isIndex := call.Fun.(*ast.IndexListExpr); isIndex {
			sel, ok = idx.X.(*ast.SelectorExpr)
		}
	}
	if !ok {
		return found{}, false
	}
	method, isVerb := verbs[sel.Sel.Name]
	if !isVerb || !isSelector(sel, g, sel.Sel.Name) {
		return found{}, false
	}
	r := found{method: method, handler: s.text(call.Args[2]), pos: s.pos(call.Pos())}
	parent := s.router(call.Args[0], groups, g)
	if path, known := stringLit(call.Args[1]); known && parent.known {
		r.path = parent.prefix + path
	}
	r.middleware = append(append([]string{}, parent.use...), s.uses(call.Args[3:], g)...)
	for _, o := range call.Args[3:] {
		if c, ok := o.(*ast.CallExpr); ok && isSelector(c.Fun, g, "OperationID") && len(c.Args) == 1 {
			r.operationID, _ = stringLit(c.Args[0])
		}
	}
	return r, true
}

// humaRoute resolves a huma.Register call whose operation is, or wraps, a
// huma.Operation literal.
func (s *scanner) humaRoute(call *ast.CallExpr, h string) (found, bool) {
	if h == "" || len(call.Args) != 3 || !isSelector(call.Fun, h, "Register") {
		return found{}, false
	}
	r := found{handler: s.text(call.Args[2]), pos: s.pos(call.Pos())}
	ast.Inspect(call.Args[1], func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok || !isSelector(lit.Type, h, "Operation") {
			return true
		}
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok {
				continue
			}
			switch key.Name {
			case "OperationID":
				r.operationID, _ = stringLit(kv.Value)
			case "Path":
				r.path, _ = stringLit(kv.Value)
			case "Method":
				r.method = methodValue(kv.Value)
			}
		}
		return false
	})
	if r.method == "" || (r.path == "" && r.operationID == "") {
		return found{}, false
	}
	return r, true
}

// handlerPos returns the declaration of a handler named by a method value
// (h.createBook, where h's type is known from types) or a function
// (createBook) in the package.
func (s *scanner) handlerPos(p *pkg, e ast.Expr, types map[string]string) *Pos {
	var name string
	switch e := e.(type) {
	case *ast.SelectorExpr:
		name = e.Sel.Name
		if x, ok := e.X.(*ast.Ident); ok && types[x.Name] != "" {
			if at := p.funcs[types[x.Name]+"."+name]; len(at) == 1 {
				return s.pos(at[0])
			}
		}
	case *ast.Ident:
		name = e.Name
	case *ast.FuncLit:
		return s.pos(e.Pos())
	default:
		return nil
	}
	if at := p.funcs[name]; len(at) == 1 {
		return s.pos(at[0])
	}
	return nil
}

func (s *scanner) pos(at token.Pos) *Pos {
	position := s.fset.Position(at)
	rel, err := filepath.Rel(s.dir, position.Filename)
	if err != nil {
		rel = position.Filename
	}
	return &Pos{File: filepath.ToSlash(rel), Line: position.Line}
}

// text returns an expression as gofmt prints it, on one line; a function
// literal is func(…).
func (s *scanner) text(e ast.Expr) string {
	if _, ok := e.(*ast.FuncLit); ok {
		return "func(…)"
	}
	var b bytes.Buffer
	if err := format.Node(&b, s.fset, e); err != nil {
		return ""
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// importName returns the name file uses for the package at path, or "" when
// it doesn't import it (or imports it as _ or .).
func importName(file *ast.File, path, name string) string {
	for _, imp := range file.Imports {
		if p, err := strconv.Unquote(imp.Path.Value); err == nil && p == path {
			if imp.Name == nil {
				return name
			}
			if imp.Name.Name == "_" || imp.Name.Name == "." {
				return ""
			}
			return imp.Name.Name
		}
	}
	return ""
}

func isSelector(e ast.Expr, x, name string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == x
}

func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	v, err := strconv.Unquote(lit.Value)
	return v, err == nil
}

// methodValue reads http.MethodPost or "POST".
func methodValue(e ast.Expr) string {
	if v, ok := stringLit(e); ok {
		return strings.ToUpper(v)
	}
	if sel, ok := e.(*ast.SelectorExpr); ok {
		if m, ok := strings.CutPrefix(sel.Sel.Name, "Method"); ok {
			return strings.ToUpper(m)
		}
	}
	return ""
}

// receiverType returns the name of a method's receiver type, without *.
func receiverType(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) != 1 {
		return ""
	}
	t := fn.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// literalType returns T for T{…} and &T{…}.
func literalType(e ast.Expr) string {
	if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.AND {
		e = u.X
	}
	if lit, ok := e.(*ast.CompositeLit); ok {
		if id, ok := lit.Type.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}
