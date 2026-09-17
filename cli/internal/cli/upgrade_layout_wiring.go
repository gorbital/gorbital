package cli

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// servicesFields maps the fields of v0.1's services struct, which the
// composition root passed to every module, to the gorbital.Deps fields that
// hold the same values.
var servicesFields = map[string]string{
	"db":       "d.DB",
	"recorder": "d.Audit",
	"logger":   "d.Logger",
	"jobs":     "d.Jobs",
	"storage":  "d.Storage",
}

// moduleValue is the gorbital.Module value the conversion writes for one of
// the app's modules.
type moduleValue struct {
	errors      []string
	permissions []string
	body        string
	imports     []importSpec
}

// buildModuleValue reads the module's internal/app wiring file — the error
// mappings it adds to the app's mapper, the permissions it declares and the
// code that builds the module — and returns them as the parts of a
// gorbital.Module value whose Routes closure runs what the wiring function
// ran.
func buildModuleValue(name string, app appInfo, wiring *goSource, module map[string][]byte, orgScoped bool, c *moduleConversion) (moduleValue, error) {
	var value moduleValue
	fn, api, err := registerFunc(wiring)
	if err != nil {
		return value, err
	}
	params := map[string]*astObject{"api": api}
	passed := map[*astObject]string{} // values the composition root passed in
	for _, field := range fn.Type.Params.List {
		for _, ident := range field.Names {
			if ident.Obj == nil || ident.Obj == api {
				continue
			}
			passed[ident.Obj] = ident.Name
			switch {
			case wiring.isSelector(stripStar(field.Type), httpxImportPath, "Mapper"):
				params["mapper"] = ident.Obj
			case wiring.text(field.Type) == "services":
				params["svc"] = ident.Obj
			}
			if params["mapper"] == ident.Obj || params["svc"] == ident.Obj {
				delete(passed, ident.Obj)
			}
		}
	}
	rootAlias, _ := wiring.importedAs(app.module + "/internal/modules/" + name)

	var problems []string
	problem := func(n ast.Node, format string, args ...any) {
		problems = append(problems, fmt.Sprintf("%s:%d: %s", wiring.name, wiring.line(n), fmt.Sprintf(format, args...)))
	}
	var edits []srcEdit
	keep := make([]ast.Stmt, 0, len(fn.Body.List))
	var mappingNodes []ast.Expr
	removedErr := false
	for i := 0; i < len(fn.Body.List); i++ {
		stmt := fn.Body.List[i]
		mappings, defines, ok := readMapperAdd(wiring, stmt, params["mapper"])
		if !ok {
			keep = append(keep, stmt)
			continue
		}
		for _, m := range mappings {
			value.errors = append(value.errors, wiring.text(m))
			mappingNodes = append(mappingNodes, m)
		}
		if defines {
			removedErr = true
			// The error check that follows it goes too.
			if i+1 < len(fn.Body.List) && isErrCheck(wiring, fn.Body.List[i+1]) {
				i++
			}
		}
	}
	if len(value.errors) == 0 {
		c.Notes = append(c.Notes, "the wiring file mapped no error of this module")
	}
	c.Errors = len(value.errors)

	usesErr := false
	for _, stmt := range keep {
		ast.Inspect(stmt, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.Ident:
				switch {
				case node.Obj == params["api"]:
					edits = append(edits, srcEdit{wiring.offset(node.Pos()), wiring.offset(node.End()), "r"})
				case node.Obj == params["mapper"]:
					problem(node, "the module uses the app's error mapper outside mapper.Add; move those mappings into the Module's Errors by hand")
				case node.Name == "err" && node.Obj != nil:
					usesErr = true
				case passed[node.Obj] != "":
					problem(node, "the module is built from %s, which the v0.1 composition root passed in; a Module builds what it needs from gorbital.Deps, so move that into the module (its Settings, Flags or Routes) by hand", passed[node.Obj])
				}
			case *ast.SelectorExpr:
				x, ok := node.X.(*ast.Ident)
				if !ok {
					return true
				}
				switch {
				case x.Obj != nil && x.Obj == params["svc"]:
					field, known := servicesFields[node.Sel.Name]
					if !known {
						if node.Sel.Name == "orgs" {
							// v0.1's organisation modules authorise through the
							// organisations module's catalog and memberships;
							// the library's orgshttp doesn't expose them.
							problem(node, "the module authorises through the organisations module's catalog and memberships, which the library's orgshttp doesn't expose; guard its routes with guard.OrgMember instead (docs/guides/guards-and-middleware.md) and convert it again")
							return true
						}
						problem(node, "the module is built from svc.%s, which gorbital.Deps has no field for; build it in the Module's Routes by hand", node.Sel.Name)
						return true
					}
					edits = append(edits, srcEdit{wiring.offset(node.Pos()), wiring.offset(node.End()), field})
					return false
				case x.Obj == nil && rootAlias != "" && x.Name == rootAlias:
					// The module's own package: the Module value is in it.
					to := node.Sel.Name
					if to == "Module" {
						to = wiringTypeName
					}
					edits = append(edits, srcEdit{wiring.offset(node.Pos()), wiring.offset(node.End()), to})
					return false
				}
			case *ast.ReturnStmt:
				edits = append(edits, returnEdit(wiring, node))
			}
			return true
		})
	}
	// Anything the wiring names that internal/app declares stays behind:
	// the module's own package can't reach it.
	for _, ident := range wiring.file.Unresolved {
		if _, isImport := wiring.imports()[ident.Name]; isImport || types.Universe.Lookup(ident.Name) != nil {
			continue
		}
		if !inRanges(wiring, ident, keep, mappingNodes) {
			continue
		}
		problem(ident, "the module's wiring names %s, which internal/app declares; move it into the module (or main.go) and convert again", ident.Name)
	}
	if len(problems) > 0 {
		c.Problems = append(c.Problems, problems...)
		return value, nil
	}

	body, err := statementsText(wiring, keep, edits)
	if err != nil {
		return value, err
	}
	if removedErr && usesErr {
		body = "var err error\n" + body
	}
	value.body = body
	value.imports = wiringImports(wiring, keep, value.errors, rootAlias)
	value.permissions = modulePermissions(wiring, name, orgScoped, c)
	return value, nil
}

// inRanges reports whether ident sits inside one of the kept statements or
// the error mappings the conversion copies.
func inRanges(g *goSource, ident *ast.Ident, stmts []ast.Stmt, exprs []ast.Expr) bool {
	at := g.offset(ident.Pos())
	for _, stmt := range stmts {
		if at >= g.offset(stmt.Pos()) && at < g.offset(stmt.End()) {
			return true
		}
	}
	for _, e := range exprs {
		if at >= g.offset(e.Pos()) && at < g.offset(e.End()) {
			return true
		}
	}
	return false
}

// registerFunc returns the wiring file's function that registers the
// module: the one taking huma.API, such as registerProjects.
func registerFunc(wiring *goSource) (*ast.FuncDecl, *astObject, error) {
	for _, decl := range wiring.file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		for _, field := range fn.Type.Params.List {
			if !wiring.isSelector(field.Type, humaImportPath, "API") || len(field.Names) != 1 {
				continue
			}
			return fn, field.Names[0].Obj, nil
		}
	}
	return nil, nil, fmt.Errorf("%s has no function taking huma.API, so orb upgrade can't tell how the module is registered", wiring.name)
}

func stripStar(e ast.Expr) ast.Expr {
	if star, ok := e.(*ast.StarExpr); ok {
		return star.X
	}
	return e
}

// readMapperAdd reads a statement that adds the module's error mappings to
// the app's mapper, as v0.1's wiring files do with mapper.Add(...), and
// returns the mappings, whether the statement declared err, and whether it
// was such a statement at all.
func readMapperAdd(g *goSource, stmt ast.Stmt, mapper *astObject) ([]ast.Expr, bool, bool) {
	isAdd := func(e ast.Expr) (*ast.CallExpr, bool) {
		call, ok := e.(*ast.CallExpr)
		if !ok {
			return nil, false
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Add" {
			return nil, false
		}
		x, ok := sel.X.(*ast.Ident)
		return call, ok && x.Obj != nil && x.Obj == mapper
	}
	switch s := stmt.(type) {
	case *ast.AssignStmt:
		if len(s.Rhs) != 1 {
			return nil, false, false
		}
		if call, ok := isAdd(s.Rhs[0]); ok {
			return call.Args, s.Tok == token.DEFINE, true
		}
	case *ast.IfStmt:
		assign, ok := s.Init.(*ast.AssignStmt)
		if !ok || len(assign.Rhs) != 1 {
			return nil, false, false
		}
		if call, ok := isAdd(assign.Rhs[0]); ok {
			return call.Args, false, true
		}
	case *ast.ExprStmt:
		if call, ok := isAdd(s.X); ok {
			return call.Args, false, true
		}
	}
	return nil, false, false
}

// isErrCheck reports whether stmt is if err != nil { … }.
func isErrCheck(g *goSource, stmt ast.Stmt) bool {
	s, ok := stmt.(*ast.IfStmt)
	if !ok || s.Init != nil {
		return false
	}
	bin, ok := s.Cond.(*ast.BinaryExpr)
	if !ok || bin.Op != token.NEQ {
		return false
	}
	x, ok := bin.X.(*ast.Ident)
	y, ok2 := bin.Y.(*ast.Ident)
	return ok && ok2 && x.Name == "err" && y.Name == "nil"
}

// returnEdit turns a wiring function's return into what a Routes closure
// does: nothing, or a panic gorbital.Mount reports naming the module.
func returnEdit(g *goSource, ret *ast.ReturnStmt) srcEdit {
	start, end := g.offset(ret.Pos()), g.offset(ret.End())
	if len(ret.Results) != 1 {
		return srcEdit{start, end, "return"}
	}
	if ident, ok := ret.Results[0].(*ast.Ident); ok && ident.Name == "nil" {
		return srcEdit{start, end, "return"}
	}
	return srcEdit{start, end, "panic(" + g.text(ret.Results[0]) + ")"}
}

// statementsText returns the source of the statements with the edits
// applied, as the body of the Module's Routes closure.
func statementsText(g *goSource, stmts []ast.Stmt, edits []srcEdit) (string, error) {
	var b strings.Builder
	for _, stmt := range stmts {
		start, end := g.offset(stmt.Pos()), g.offset(stmt.End())
		if doc := stmtDoc(g, stmt); doc > 0 {
			start = doc
		}
		var inside []srcEdit
		for _, e := range edits {
			if e.start >= start && e.end <= end {
				inside = append(inside, srcEdit{e.start - start, e.end - start, e.text})
			}
		}
		text, err := applyEditsRaw(g.src[start:end], inside)
		if err != nil {
			return "", err
		}
		b.Write(text)
		b.WriteString("\n")
	}
	return b.String(), nil
}

// stmtDoc returns the offset of the comment lines directly above stmt, or
// 0 when it has none, so the conversion keeps them with their code.
func stmtDoc(g *goSource, stmt ast.Stmt) int {
	start := g.offset(stmt.Pos())
	line := g.line(stmt)
	best := 0
	for _, group := range g.file.Comments {
		end := g.fset.Position(group.End()).Line
		if g.offset(group.End()) < start && end == line-1 {
			best = g.offset(group.Pos())
			line = g.fset.Position(group.Pos()).Line
		}
	}
	return best
}

// applyEditsRaw applies edits to src without formatting it.
func applyEditsRaw(src []byte, edits []srcEdit) ([]byte, error) {
	sorted := slices.Clone(edits)
	slices.SortStableFunc(sorted, func(a, b srcEdit) int { return a.start - b.start })
	out := slices.Clone(src)
	for i := len(sorted) - 1; i >= 0; i-- {
		e := sorted[i]
		if e.start < 0 || e.end > len(out) || e.start > e.end {
			return nil, fmt.Errorf("edit out of range")
		}
		out = slices.Concat(out[:e.start], []byte(e.text), out[e.end:])
	}
	return out, nil
}

// wiringImports returns the imports the Module value's code needs: the
// wiring file's, for every package the kept code and the error mappings
// name, and gorbital itself.
func wiringImports(g *goSource, stmts []ast.Stmt, errors []string, rootAlias string) []importSpec {
	used := map[string]bool{}
	for _, stmt := range stmts {
		ast.Inspect(stmt, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if x, ok := sel.X.(*ast.Ident); ok && x.Obj == nil {
					used[x.Name] = true
				}
			}
			return true
		})
	}
	for _, e := range errors {
		for name := range g.imports() {
			if strings.Contains(e, name+".") {
				used[name] = true
			}
		}
	}
	specs := []importSpec{{path: gorbitalImportPath}}
	if len(errors) > 0 {
		specs = append(specs, importSpec{path: httpxImportPath})
	}
	imports := g.imports()
	for _, name := range slices.Sorted(maps.Keys(imports)) {
		if !used[name] || name == rootAlias {
			continue
		}
		spec := importSpec{path: imports[name]}
		if name != pathBase(imports[name]) {
			spec.name = name
		}
		specs = append(specs, spec)
	}
	return specs
}

func pathBase(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// modulePermissions reads the permissions the wiring file declares with
// v0.1's resourcePermissions value and returns them as gorbital.Permission
// literals: user-scoped ones through the user role every signed-in user
// holds, organisation ones through the organisation roles (ADR-0058,
// ADR-0048).
func modulePermissions(g *goSource, name string, orgScoped bool, c *moduleConversion) []string {
	var out []string
	for _, decl := range g.file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Values) != 1 {
				continue
			}
			lit, ok := vs.Values[0].(*ast.CompositeLit)
			if !ok {
				continue
			}
			if ident, ok := lit.Type.(*ast.Ident); !ok || ident.Name != "resourcePermissions" {
				continue
			}
			fields := map[string]ast.Expr{}
			for _, elt := range lit.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if key, ok := kv.Key.(*ast.Ident); ok {
					fields[key.Name] = kv.Value
				}
			}
			read, write, human := fields["read"], fields["write"], fields["name"]
			if read == nil || write == nil || human == nil {
				continue
			}
			label, err := strconv.Unquote(g.text(human))
			if err != nil {
				label = name
			}
			roles := `Roles: []string{"user"}`
			see, change := "See your "+label, "Create, change and delete your "+label
			if orgScoped {
				roles = `OrgRoles: []string{"owner", "admin", "member"}`
				see, change = "See "+label, "Create, change and delete "+label
			}
			out = append(out,
				fmt.Sprintf("{Name: %s, Description: %q, %s},", g.text(read), see, roles),
				fmt.Sprintf("{Name: %s, Description: %q, %s},", g.text(write), change, roles))
			c.Permissions = append(c.Permissions, g.text(read), g.text(write))
		}
	}
	return out
}

// addModuleFunc adds func Module() gorbital.Module, built from value, to
// the module's root package, and returns the file it wrote.
func addModuleFunc(name string, app appInfo, module map[string][]byte, value moduleValue) (string, []byte, error) {
	root := "internal/modules/" + name
	path := root + "/module.go"
	src, ok := module[path]
	if !ok {
		for _, p := range slices.Sorted(maps.Keys(module)) {
			if pathDir(p) == root && strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
				path, src, ok = p, module[p], true
				break
			}
		}
	}
	if !ok {
		return "", nil, fmt.Errorf("internal/modules/%s has no Go file in its root package, so orb upgrade has nowhere to declare func Module() gorbital.Module", name)
	}
	g, err := parseGoSource(path, src)
	if err != nil {
		return "", nil, err
	}
	for _, decl := range g.file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == "Module" {
			return "", nil, fmt.Errorf("%s already declares func Module, so the module looks converted already", path)
		}
	}

	var b strings.Builder
	b.Write(src)
	fmt.Fprintf(&b, "\n// Module returns the %s module. main.go adds it with every other module\n"+
		"// through modules.All. orb upgrade --layout v0.2 moved the error mappings and\n"+
		"// permissions of the v0.1 composition root here; error codes and permission\n"+
		"// names are public API: add new ones, never change existing ones.\n", name)
	b.WriteString("func Module() gorbital.Module {\n\treturn gorbital.Module{\n")
	fmt.Fprintf(&b, "\t\tName: %q,\n", name)
	if len(value.errors) > 0 {
		b.WriteString("\t\tErrors: []httpx.Mapping{\n")
		for _, e := range value.errors {
			b.WriteString("\t\t\t" + e + ",\n")
		}
		b.WriteString("\t\t},\n")
	}
	if len(value.permissions) > 0 {
		b.WriteString("\t\tPermissions: []gorbital.Permission{\n")
		for _, p := range value.permissions {
			b.WriteString("\t\t\t" + p + "\n")
		}
		b.WriteString("\t\t},\n")
	}
	b.WriteString("\t\tRoutes: func(r *gorbital.Router, d gorbital.Deps) {\n")
	b.WriteString(value.body)
	b.WriteString("\t\t},\n\t}\n}\n")

	formatted, err := format.Source([]byte(b.String()))
	if err != nil {
		return "", nil, fmt.Errorf("the %s module's generated Module function doesn't parse: %w", name, err)
	}
	out, err := fixImports(path, formatted, app.module, value.imports)
	if err != nil {
		return "", nil, err
	}
	return path, out, nil
}
