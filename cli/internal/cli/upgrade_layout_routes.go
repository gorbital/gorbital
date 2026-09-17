package cli

import (
	"fmt"
	"go/ast"
	"go/token"
	"maps"
	"path"
	"slices"
	"sort"
	"strings"
)

// Library packages the conversion reads and writes in a module's code.
const (
	humaImportPath      = "github.com/danielgtaylor/huma/v2"
	httpxImportPath     = "gorbital.dev/httpx"
	openapiImportPath   = "gorbital.dev/modules/openapi"
	guardImportPath     = gorbitalImportPath + "/guard"
	operationImportPath = gorbitalImportPath + "/operation"
)

// humaOperationFields are the huma.Operation fields the conversion carries
// over to route options; operation.Register carries the rest of this list
// (its own `carried`), and anything outside both changes the operation, so
// the module stops the conversion.
var humaOperationFields = []string{"OperationID", "Method", "Path", "Summary", "Description", "Tags", "Security", "Errors", "DefaultStatus"}

// escapeOperationFields are fields gorbital's operation.Register carries but
// the verbs don't, so an operation with one is registered through it.
var escapeOperationFields = []string{"Responses", "MaxBodyBytes", "SkipValidateBody"}

// routeVerbs maps an HTTP method to the gorbital verb that registers it.
var routeVerbs = map[string]string{
	"MethodGet": "Get", "MethodPost": "Post", "MethodPut": "Put", "MethodPatch": "Patch", "MethodDelete": "Delete",
	"GET": "Get", "POST": "Post", "PUT": "Put", "PATCH": "Patch", "DELETE": "Delete",
}

// routeRewrite is what the conversion did to a module's route registrations.
type routeRewrite struct {
	// Files are the module's files that changed, by app-relative path.
	Files map[string][]byte
	// Routes are the operations turned into gorbital routes, such as
	// "projects-create: gorbital.Post /v1/projects".
	Routes []string
	// Escaped are the operations kept as huma.Operation values and
	// registered with operation.Register, with the reason.
	Escaped []string
	// Public are the operations without a security requirement, registered
	// with guard.Public().
	Public []string
	// Problems say why the module's routes couldn't be converted; with any
	// of them, Files is not usable.
	Problems []string
}

// routeFunc is a function that takes huma.API, found while scanning a
// module: v0.1 apps pass the API down from the composition root to each
// module's delivery package.
type routeFunc struct {
	// params are the indices of the function's huma.API parameters.
	params []int
}

// rewriteModuleRoutes turns a v0.1 module's huma.Register calls into
// gorbital routes: every function that takes huma.API takes a
// *gorbital.Router instead, each operation's fields become route options,
// and an operation the verbs can't express is registered through
// gorbital.dev/gorbital/operation, which reads huma.Operation values
// (ADR-0083). files are the module's Go files by app-relative path.
func rewriteModuleRoutes(files map[string][]byte, appModule string) (routeRewrite, error) {
	out := routeRewrite{Files: map[string][]byte{}}
	sources := map[string]*goSource{}
	funcs := map[string]routeFunc{} // "<package path>.<name>" and ".<method name>"
	for _, p := range slices.Sorted(maps.Keys(files)) {
		g, err := parseGoSource(p, files[p])
		if err != nil {
			return out, err
		}
		sources[p] = g
		if _, ok := g.importedAs(humaImportPath); !ok {
			continue
		}
		pkg := appModule + "/" + path.Dir(p)
		for _, decl := range g.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			var params []int
			i := 0
			for _, field := range fn.Type.Params.List {
				names := max(len(field.Names), 1)
				if g.isSelector(field.Type, humaImportPath, "API") {
					for n := range names {
						params = append(params, i+n)
					}
				}
				i += names
			}
			if len(params) == 0 {
				continue
			}
			funcs[pkg+"."+fn.Name.Name] = routeFunc{params: params}
			if fn.Recv != nil {
				funcs["."+fn.Name.Name] = routeFunc{params: params}
			}
		}
	}
	if len(funcs) == 0 {
		return out, nil
	}
	for _, p := range slices.Sorted(maps.Keys(files)) {
		g := sources[p]
		if _, ok := g.importedAs(humaImportPath); !ok {
			continue
		}
		content, err := rewriteRouteFile(g, appModule, funcs, &out)
		if err != nil {
			return out, err
		}
		if content != nil {
			out.Files[p] = content
		}
	}
	sort.Strings(out.Problems)
	return out, nil
}

// rewriteRouteFile rewrites one file's huma.API parameters and
// huma.Register calls, or returns nil when it has none.
func rewriteRouteFile(g *goSource, appModule string, funcs map[string]routeFunc, out *routeRewrite) ([]byte, error) {
	var edits []srcEdit
	add := map[string]importSpec{}
	changed := false
	for _, decl := range g.file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		params := map[*astObject]*ast.Ident{}
		for _, field := range fn.Type.Params.List {
			if !g.isSelector(field.Type, humaImportPath, "API") {
				continue
			}
			edits = append(edits, srcEdit{g.offset(field.Type.Pos()), g.offset(field.Type.End()), "*gorbital.Router"})
			add[gorbitalImportPath] = importSpec{path: gorbitalImportPath}
			changed = true
			for _, name := range field.Names {
				if name.Obj != nil {
					params[name.Obj] = name
				}
			}
		}
		if len(params) == 0 {
			continue
		}
		fileEdits, wrappers, err := rewriteRegisterCalls(g, fn, params, funcs, add, out)
		if err != nil {
			return nil, err
		}
		edits = append(edits, fileEdits...)
		edits = append(edits, wrappers...)
	}
	if !changed {
		return nil, nil
	}
	src, err := applySrcEdits(g.name, g.src, edits)
	if err != nil {
		return nil, err
	}
	specs := make([]importSpec, 0, len(add))
	for _, p := range slices.Sorted(maps.Keys(add)) {
		specs = append(specs, add[p])
	}
	return fixImports(g.name, src, appModule, specs)
}

// rewriteRegisterCalls rewrites the huma.Register calls of fn that register
// on one of its huma.API parameters, and returns the edits, and the edits
// removing operation wrappers no call uses any more.
func rewriteRegisterCalls(g *goSource, fn *ast.FuncDecl, params map[*astObject]*ast.Ident, funcs map[string]routeFunc, add map[string]importSpec, out *routeRewrite) ([]srcEdit, []srcEdit, error) {
	var edits []srcEdit
	converted := map[*astObject]int{} // operation wrapper -> calls that no longer use it
	var stack []ast.Node
	problem := func(n ast.Node, format string, args ...any) {
		out.Problems = append(out.Problems, fmt.Sprintf("%s:%d: %s", g.name, g.line(n), fmt.Sprintf(format, args...)))
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		defer func() { stack = append(stack, n) }()
		ident, ok := n.(*ast.Ident)
		if !ok || ident.Obj == nil || params[ident.Obj] == nil || len(stack) == 0 {
			return true
		}
		parent := stack[len(stack)-1]
		call, ok := parent.(*ast.CallExpr)
		if !ok {
			problem(ident, "%s is used outside a call; orb upgrade converts modules that pass huma.API to their route registrations", ident.Name)
			return true
		}
		if g.isSelector(call.Fun, humaImportPath, "Register") && len(call.Args) == 3 && call.Args[0] == ast.Expr(ident) {
			edit, wrapper, err := rewriteRegister(g, call, ident, add, out)
			if err != nil {
				problem(call, "%v", err)
				return true
			}
			edits = append(edits, edit)
			if wrapper != nil {
				converted[wrapper]++
			}
			return true
		}
		if !callTakesAPI(g, call, ident, funcs) {
			problem(call, "%s is passed to %s, which orb upgrade can't follow; it converts calls to huma.Register and to the module's own functions that take huma.API", ident.Name, g.text(call.Fun))
		}
		return true
	})
	var removals []srcEdit
	for obj, gone := range converted {
		if stmt, ok := obj.Decl.(*ast.AssignStmt); ok {
			// Its declaration names it too, so one use is the wrapper itself.
			if gone < countUses(fn.Body, obj)-1 {
				continue // another call still passes its operation through it
			}
			// The wrapper is an operation decorator no call uses now.
			start, end := lineRange(g.src, g.offset(stmt.Pos()), g.offset(stmt.End()))
			removals = append(removals, srcEdit{start, end, ""})
		}
	}
	return edits, removals, nil
}

// lineRange widens [start, end) to whole lines, including the newline.
func lineRange(src []byte, start, end int) (int, int) {
	for start > 0 && src[start-1] != '\n' {
		start--
	}
	for end < len(src) && src[end] != '\n' {
		end++
	}
	if end < len(src) {
		end++
	}
	return start, end
}

// countUses counts the identifiers in n that name obj.
func countUses(n ast.Node, obj *astObject) int {
	uses := 0
	ast.Inspect(n, func(n ast.Node) bool {
		if ident, ok := n.(*ast.Ident); ok && ident.Obj == obj {
			uses++
		}
		return true
	})
	return uses
}

// callTakesAPI reports whether call passes api to a function of the module
// that takes huma.API at that position, such as a module's Register method
// or its delivery package's Register.
func callTakesAPI(g *goSource, call *ast.CallExpr, api *ast.Ident, funcs map[string]routeFunc) bool {
	index := slices.IndexFunc(call.Args, func(a ast.Expr) bool { return a == ast.Expr(api) })
	if index < 0 {
		return false
	}
	var keys []string
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		keys = append(keys, "."+fun.Name)
		for pkg := range funcs {
			if strings.HasSuffix(pkg, "."+fun.Name) {
				keys = append(keys, pkg)
			}
		}
	case *ast.SelectorExpr:
		keys = append(keys, "."+fun.Sel.Name)
		if x, ok := fun.X.(*ast.Ident); ok {
			if imp, ok := g.imports()[x.Name]; ok {
				keys = append(keys, imp+"."+fun.Sel.Name)
			}
		}
	}
	for _, key := range keys {
		if f, ok := funcs[key]; ok && slices.Contains(f.params, index) {
			return true
		}
	}
	return false
}

// rewriteRegister turns one huma.Register call into a gorbital verb, or
// into operation.Register when the operation's fields need it. It returns
// the edit and the wrapper the call used, if any.
func rewriteRegister(g *goSource, call *ast.CallExpr, api *ast.Ident, add map[string]importSpec, out *routeRewrite) (srcEdit, *astObject, error) {
	op, wrapper, err := readOperation(g, call.Args[1])
	if err != nil {
		return srcEdit{}, nil, err
	}
	handler := g.text(call.Args[2])
	verbCall, escape, err := op.routeCall(g, api.Name, handler, add)
	if err != nil {
		return srcEdit{}, nil, err
	}
	if escape != "" {
		// The operation keeps its huma.Operation value; operation.Register
		// registers it on the router with the same fields (ADR-0083).
		add[operationImportPath] = importSpec{path: operationImportPath}
		out.Escaped = append(out.Escaped, fmt.Sprintf("%s (%s)", op.id(g), escape))
		text := fmt.Sprintf("operation.Register(%s, %s, %s)", api.Name, g.text(call.Args[1]), handler)
		return srcEdit{g.offset(call.Pos()), g.offset(call.End()), text}, nil, nil
	}
	out.Routes = append(out.Routes, fmt.Sprintf("%s: %s", op.id(g), op.summary(g)))
	if op.public {
		out.Public = append(out.Public, op.id(g))
	}
	return srcEdit{g.offset(call.Pos()), g.offset(call.End()), verbCall}, wrapper, nil
}

// operationValue is a huma.Operation declaration: the expression of each
// field, and the errors an operation wrapper prepends.
type operationValue struct {
	fields   map[string]ast.Expr
	prepend  []ast.Expr // status codes a wrapper puts before Errors
	unknown  []string   // fields the conversion doesn't carry over
	escaping []string   // fields only operation.Register carries
	public   bool
	node     ast.Node
}

func (o operationValue) id(g *goSource) string {
	if e, ok := o.fields["OperationID"]; ok {
		return strings.Trim(g.text(e), `"`)
	}
	return fmt.Sprintf("%s:%d", g.name, g.line(o.node))
}

func (o operationValue) summary(g *goSource) string {
	method, path := "", ""
	if e, ok := o.fields["Method"]; ok {
		method = strings.TrimPrefix(strings.TrimPrefix(g.text(e), "http.Method"), `"`)
	}
	if e, ok := o.fields["Path"]; ok {
		path = strings.Trim(g.text(e), `"`)
	}
	return strings.TrimSpace(strings.ToUpper(strings.Trim(method, `"`)) + " " + path)
}

// readOperation reads the operation an argument of huma.Register declares:
// a huma.Operation literal, or a literal passed through a wrapper the
// function declares, such as v0.1's signedIn, whose assignments to the
// operation's fields it applies.
func readOperation(g *goSource, arg ast.Expr) (operationValue, *astObject, error) {
	op := operationValue{fields: map[string]ast.Expr{}, node: arg}
	lit, ok := arg.(*ast.CompositeLit)
	var wrapper *astObject
	if !ok {
		call, isCall := arg.(*ast.CallExpr)
		if !isCall || len(call.Args) != 1 {
			return op, nil, fmt.Errorf("orb upgrade reads a huma.Operation literal, or one passed through a wrapper the function declares; this one is %s", g.text(arg))
		}
		ident, isIdent := call.Fun.(*ast.Ident)
		if !isIdent || ident.Obj == nil {
			return op, nil, fmt.Errorf("orb upgrade reads a huma.Operation literal, or one passed through a wrapper the function declares; this one is %s", g.text(arg))
		}
		lit, ok = call.Args[0].(*ast.CompositeLit)
		if !ok {
			return op, nil, fmt.Errorf("%s doesn't take a huma.Operation literal", g.text(call.Fun))
		}
		wrapper = ident.Obj
	}
	if !g.isSelector(lit.Type, humaImportPath, "Operation") {
		return op, nil, fmt.Errorf("orb upgrade reads huma.Operation literals; this one is %s", g.text(lit.Type))
	}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			return op, nil, fmt.Errorf("orb upgrade reads huma.Operation literals with field names")
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			return op, nil, fmt.Errorf("orb upgrade reads huma.Operation literals with field names")
		}
		op.fields[key.Name] = kv.Value
	}
	if wrapper != nil {
		if err := applyOperationWrapper(g, wrapper, &op); err != nil {
			return op, nil, err
		}
	}
	for name := range op.fields {
		switch {
		case slices.Contains(humaOperationFields, name):
		case slices.Contains(escapeOperationFields, name):
			op.escaping = append(op.escaping, name)
		default:
			op.unknown = append(op.unknown, name)
		}
	}
	sort.Strings(op.escaping)
	sort.Strings(op.unknown)
	return op, wrapper, nil
}

// applyOperationWrapper applies a wrapper such as v0.1's signedIn, declared
// as op := func(op huma.Operation) huma.Operation { … }, to the operation:
// every field it assigns, and the status codes it puts before Errors.
func applyOperationWrapper(g *goSource, wrapper *astObject, op *operationValue) error {
	assign, ok := wrapper.Decl.(*ast.AssignStmt)
	if !ok || len(assign.Rhs) != 1 {
		return fmt.Errorf("orb upgrade reads an operation wrapper declared as one function literal")
	}
	fn, ok := assign.Rhs[0].(*ast.FuncLit)
	if !ok || len(fn.Type.Params.List) != 1 || len(fn.Type.Params.List[0].Names) != 1 {
		return fmt.Errorf("orb upgrade reads an operation wrapper taking one huma.Operation")
	}
	param := fn.Type.Params.List[0].Names[0].Obj
	uses := func(e ast.Expr) bool {
		found := false
		ast.Inspect(e, func(n ast.Node) bool {
			if ident, ok := n.(*ast.Ident); ok && ident.Obj == param {
				found = true
			}
			return !found
		})
		return found
	}
	for i, stmt := range fn.Body.List {
		if ret, ok := stmt.(*ast.ReturnStmt); ok && i == len(fn.Body.List)-1 {
			if len(ret.Results) == 1 {
				if ident, ok := ret.Results[0].(*ast.Ident); ok && ident.Obj == param {
					continue
				}
			}
			return fmt.Errorf("an operation wrapper must end by returning the operation it changed")
		}
		assign, ok := stmt.(*ast.AssignStmt)
		if !ok || assign.Tok != token.ASSIGN || len(assign.Lhs) != len(assign.Rhs) {
			return fmt.Errorf("orb upgrade reads an operation wrapper that only assigns to the operation's fields (line %d)", g.line(stmt))
		}
		for j, lhs := range assign.Lhs {
			sel, ok := lhs.(*ast.SelectorExpr)
			if !ok {
				return fmt.Errorf("orb upgrade reads an operation wrapper that only assigns to the operation's fields (line %d)", g.line(stmt))
			}
			if ident, ok := sel.X.(*ast.Ident); !ok || ident.Obj != param {
				return fmt.Errorf("orb upgrade reads an operation wrapper that only assigns to the operation's fields (line %d)", g.line(stmt))
			}
			value := assign.Rhs[j]
			if !uses(value) {
				op.fields[sel.Sel.Name] = value
				continue
			}
			prepend, err := readErrorsAppend(g, sel.Sel.Name, value, param)
			if err != nil {
				return err
			}
			op.prepend = append(op.prepend, prepend...)
		}
	}
	return nil
}

// readErrorsAppend reads an operation wrapper's
// op.Errors = append([]int{…}, op.Errors...) and returns the status codes
// it puts first, as v0.1's signedIn does with 401 and 403.
func readErrorsAppend(g *goSource, field string, value ast.Expr, param *astObject) ([]ast.Expr, error) {
	fail := fmt.Errorf("orb upgrade reads an operation wrapper that changes %s only as append([]int{…}, op.Errors...)", field)
	if field != "Errors" {
		return nil, fail
	}
	call, ok := value.(*ast.CallExpr)
	if !ok || len(call.Args) != 2 || !call.Ellipsis.IsValid() {
		return nil, fail
	}
	if ident, ok := call.Fun.(*ast.Ident); !ok || ident.Name != "append" {
		return nil, fail
	}
	lit, ok := call.Args[0].(*ast.CompositeLit)
	if !ok {
		return nil, fail
	}
	sel, ok := call.Args[1].(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Errors" {
		return nil, fail
	}
	if ident, ok := sel.X.(*ast.Ident); !ok || ident.Obj != param {
		return nil, fail
	}
	return lit.Elts, nil
}

// routeCall returns the gorbital verb call registering the operation on
// router, or the reason it has to go through operation.Register.
func (o *operationValue) routeCall(g *goSource, router, handler string, add map[string]importSpec) (string, string, error) {
	switch {
	case len(o.unknown) > 0:
		return "", "", fmt.Errorf("operation %s sets %s, which neither gorbital's route options nor operation.Register carry over; change the operation by hand and convert the module again", o.id(g), strings.Join(o.unknown, ", "))
	case len(o.escaping) > 0:
		return "", strings.Join(o.escaping, ", "), nil
	}
	method, ok := o.fields["Method"]
	if !ok {
		return "", "", fmt.Errorf("operation %s has no Method", o.id(g))
	}
	verb := ""
	switch m := method.(type) {
	case *ast.SelectorExpr:
		if g.imports()[selectorPackage(m)] == "net/http" {
			verb = routeVerbs[m.Sel.Name]
		}
	case *ast.BasicLit:
		verb = routeVerbs[strings.Trim(m.Value, `"`)]
	}
	if verb == "" {
		return "", "", fmt.Errorf("operation %s has the method %s, which gorbital's verbs don't cover", o.id(g), g.text(method))
	}
	route, ok := o.fields["Path"]
	if !ok {
		return "", "", fmt.Errorf("operation %s has no Path", o.id(g))
	}

	opts := []string{}
	option := func(name, arg string) { opts = append(opts, fmt.Sprintf("gorbital.%s(%s)", name, arg)) }
	for _, field := range []string{"OperationID", "Summary", "Description"} {
		if e, ok := o.fields[field]; ok {
			option(field, g.text(e))
		}
	}
	if e, ok := o.fields["Tags"]; ok {
		args, err := listArgs(g, e)
		if err != nil {
			return "", "", fmt.Errorf("operation %s: Tags: %w", o.id(g), err)
		}
		option("Tags", args)
	}
	if e, ok := o.fields["DefaultStatus"]; ok {
		option("Status", g.text(e))
	}
	var errorCodes []string
	for _, e := range o.prepend {
		errorCodes = append(errorCodes, g.text(e))
	}
	if e, ok := o.fields["Errors"]; ok {
		args, err := listArgs(g, e)
		if err != nil {
			return "", "", fmt.Errorf("operation %s: Errors: %w", o.id(g), err)
		}
		if args != "" {
			errorCodes = append(errorCodes, args)
		}
	}
	if len(errorCodes) > 0 {
		option("Errors", strings.Join(errorCodes, ", "))
	}
	security, hasSecurity := o.fields["Security"]
	switch {
	case !hasSecurity:
		// v0.1 registered operations without a security requirement for
		// anyone; gorbital routes deny by default, so they become public.
		o.public = true
		opts = append(opts, "guard.Public()")
		add[guardImportPath] = importSpec{path: guardImportPath}
	case g.isSelector(security, openapiImportPath, "Bearer"):
		// Signed-in operations: the router requires an authenticated actor.
	default:
		return "", fmt.Sprintf("the security requirement %s", g.text(security)), nil
	}
	add[gorbitalImportPath] = importSpec{path: gorbitalImportPath}
	return fmt.Sprintf("gorbital.%s(%s, %s, %s,\n%s)", verb, router, g.text(route), handler, strings.Join(opts, ",\n")), "", nil
}

// selectorPackage returns the name a selector qualifies with, such as http
// in http.MethodGet.
func selectorPackage(sel *ast.SelectorExpr) string {
	if x, ok := sel.X.(*ast.Ident); ok {
		return x.Name
	}
	return ""
}

// listArgs returns the elements of a slice literal as call arguments, or
// the expression itself spread with ... .
func listArgs(g *goSource, e ast.Expr) (string, error) {
	lit, ok := e.(*ast.CompositeLit)
	if !ok {
		return g.text(e) + "...", nil
	}
	var args []string
	for _, elt := range lit.Elts {
		args = append(args, g.text(elt))
	}
	return strings.Join(args, ", "), nil
}
