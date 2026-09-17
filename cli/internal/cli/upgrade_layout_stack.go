package cli

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/types"
	"maps"
	"slices"
	"strings"
)

// routesPath is the v0.1 composition root's middleware stack.
const routesPath = "internal/app/routes.go"

// stackSteps maps each middleware of a v0.1 app's routes.go to the
// gorbital.Stack field that runs it in the v0.2 layout. An empty field is a
// step the Stack's Auth already includes.
var stackSteps = []struct{ contains, field string }{
	{"httpx.Recover(", "Recover"},
	{"httpx.TrustedProxies(", "TrustedProxies"},
	{"httpx.RequestIDFrom(", "RequestID"},
	{"HTTPMiddleware()", "Telemetry"},
	{"observabilityMiddleware(", "Observability"},
	{"httpx.AccessLog(", "AccessLog"},
	{"httpx.SecureHeaders(", "SecureHeaders"},
	{"exceptCrossSitePosts(", "CrossOrigin"},
	{"httpx.BodyLimit(", "BodyLimit"},
	{"a.maintenance()", "Maintenance"},
	{"cors", "CORS"},
	{".Middleware(a.logger)", "Auth"},
	{"a.console.Operator(", ""}, // part of the Auth step (ADR-0066)
	{"ratelimit.Middleware(", "RateLimit"},
	{"idempotencyMiddleware(", "Idempotency"},
}

// stackCarry is the app's own middleware, carried from the v0.1
// composition root into main.go's options.
type stackCarry struct {
	// Option is the gorbital option main.go gains, such as
	// gorbital.WithStack(stack).
	Option string `json:"option"`
	// Comment explains the option in main.go.
	Comment string `json:"-"`
	// Files are the files to write: cmd/api/stack.go and the app's
	// middleware moved into package main.
	Files map[string][]byte `json:"-"`
	// Moved are the internal/app files that became files of cmd/api.
	Moved []string `json:"moved,omitempty"`
	// Added are the app's middleware expressions, in the order they run.
	Added []string `json:"added,omitempty"`
	// Removed are default steps routes.go left out.
	Removed []string `json:"removed,omitempty"`
	// Problems say why the middleware can't be carried over.
	Problems []string `json:"problems,omitempty"`
}

// carryMiddleware reads the middleware a v0.1 app added to its stack in
// internal/app/routes.go and returns the option main.go needs to run it in
// the same place, with the files it moves into package main.
func carryMiddleware(app appInfo, base, ours map[string][]byte, cmdDecls map[string]bool) (stackCarry, error) {
	carry := stackCarry{Files: map[string][]byte{}}
	baseSrc, ok := base[routesPath]
	current, hasOurs := ours[routesPath]
	if !ok || !hasOurs {
		carry.Problems = append(carry.Problems, routesPath+" is missing; orb upgrade reads the app's middleware there")
		return carry, nil
	}
	baseList, baseSkeleton, err := readMiddlewareList(routesPath, baseSrc)
	if err != nil {
		return carry, err
	}
	ourList, ourSkeleton, err := readMiddlewareList(routesPath, current)
	if err != nil {
		return carry, err
	}
	if baseSkeleton != ourSkeleton {
		carry.Problems = append(carry.Problems, routesPath+" changed outside the middleware list; move those changes into main.go's options by hand (gorbital.WithStack, gorbital.WithMiddleware) and convert again")
		return carry, nil
	}

	steps := make([]string, 0, len(baseList)) // the Stack field of each base step
	for _, text := range baseList {
		field, ok := stackField(text)
		if !ok {
			carry.Problems = append(carry.Problems, fmt.Sprintf("%s: orb upgrade doesn't know the step %s of a v0.1 stack", routesPath, text))
			return carry, nil
		}
		steps = append(steps, field)
	}

	// Line up the app's list with the generated one: every step it kept,
	// and every middleware of its own, in order.
	var entries []string // "s.Recover" or the app's expression
	var added []string
	appended := false // the app's middleware comes after every default step
	i := 0
	for _, text := range ourList {
		if j := slices.Index(baseList[i:], text); j >= 0 {
			i += j + 1
			if steps[i-1] != "" {
				entries = append(entries, "s."+steps[i-1])
				if steps[i-1] == "AccessLog" {
					entries = append(entries, "s.Timeout") // added in v0.2 (ADR-0085)
				}
			}
			continue
		}
		added = append(added, text)
		entries = append(entries, text)
		appended = i >= len(baseList)
	}
	for _, text := range baseList[i:] {
		if field, _ := stackField(text); field != "" {
			carry.Removed = append(carry.Removed, field)
		}
	}
	carry.Added = added
	if len(added) == 0 && len(carry.Removed) == 0 {
		return carry, nil // the list only moved; the default stack runs it
	}

	files, moved, problems := moveAppFiles(app, added, base, ours, cmdDecls)
	carry.Problems = append(carry.Problems, problems...)
	if len(carry.Problems) > 0 {
		return carry, nil
	}
	maps.Copy(carry.Files, files)
	carry.Moved = moved

	if appended && len(carry.Removed) == 0 {
		// gorbital.WithMiddleware runs after the whole stack, which is where
		// these already ran.
		carry.Option = fmt.Sprintf("gorbital.WithMiddleware(%s)", strings.Join(added, ", "))
		carry.Comment = "the app's middleware, as in the v0.1 internal/app/routes.go"
		return carry, nil
	}
	content, err := renderStackFile(app, entries, base[routesPath], ours[routesPath])
	if err != nil {
		return carry, err
	}
	carry.Files["cmd/api/stack.go"] = content
	carry.Option = "gorbital.WithStack(stack)"
	carry.Comment = "stack.go: the middleware order of the v0.1 internal/app/routes.go"
	return carry, nil
}

// stackField returns the gorbital.Stack field a v0.1 middleware expression
// becomes.
func stackField(text string) (string, bool) {
	for _, s := range stackSteps {
		if strings.Contains(text, s.contains) {
			return s.field, true
		}
	}
	return "", false
}

// readMiddlewareList returns the middleware expressions of a v0.1
// routes.go, in the order they run, and the rest of the file with the list
// blanked out, so two versions can be compared without it.
func readMiddlewareList(name string, src []byte) ([]string, string, error) {
	g, err := parseGoSource(name, src)
	if err != nil {
		return nil, "", err
	}
	var list []string
	var cuts [][2]int
	ast.Inspect(g.file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			if len(node.Lhs) != 1 || len(node.Rhs) != 1 {
				return true
			}
			ident, ok := node.Lhs[0].(*ast.Ident)
			if !ok || ident.Name != "middlewares" {
				return true
			}
			switch rhs := node.Rhs[0].(type) {
			case *ast.CompositeLit:
				for _, elt := range rhs.Elts {
					list = append(list, g.text(elt))
				}
				// The whole list is cut from the skeleton, so adding or
				// removing a step doesn't look like a change to the file.
				cuts = append(cuts, [2]int{g.offset(rhs.Lbrace) + 1, g.offset(rhs.Rbrace)})
			case *ast.CallExpr:
				if fun, ok := rhs.Fun.(*ast.Ident); !ok || fun.Name != "append" {
					return true
				}
				if len(rhs.Args) < 2 {
					return true
				}
				for _, arg := range rhs.Args[1:] {
					list = append(list, g.text(arg))
				}
				cuts = append(cuts, [2]int{g.offset(rhs.Args[1].Pos()), g.offset(rhs.Rparen)})
			}
		}
		return true
	})
	// The skeleton is the file without the list and without comments, so
	// two versions compare on their code alone.
	for _, group := range g.file.Comments {
		cuts = append(cuts, [2]int{g.offset(group.Pos()), g.offset(group.End())})
	}
	merged := mergeRanges(cuts)
	skeleton := string(src)
	for i := len(merged) - 1; i >= 0; i-- {
		skeleton = skeleton[:merged[i][0]] + "<cut>" + skeleton[merged[i][1]:]
	}
	return list, skeleton, nil
}

// mergeRanges sorts ranges and joins the ones that overlap.
func mergeRanges(ranges [][2]int) [][2]int {
	slices.SortFunc(ranges, func(a, b [2]int) int { return a[0] - b[0] })
	var out [][2]int
	for _, r := range ranges {
		if r[0] >= r[1] {
			continue
		}
		if len(out) > 0 && r[0] <= out[len(out)-1][1] {
			out[len(out)-1][1] = max(out[len(out)-1][1], r[1])
			continue
		}
		out = append(out, r)
	}
	return out
}

// moveAppFiles moves the files declaring the app's own middleware from
// internal/app, which the v0.2 layout doesn't have, into package main
// beside main.go. A file that needs anything else of internal/app stays
// where it is and is reported.
func moveAppFiles(app appInfo, exprs []string, base, ours map[string][]byte, cmdDecls map[string]bool) (map[string][]byte, []string, []string) {
	owned := map[string]*goSource{}
	declared := map[string]string{} // declaration -> file
	var problems []string
	for _, p := range slices.Sorted(maps.Keys(ours)) {
		if !strings.HasPrefix(p, "internal/app/") || !strings.HasSuffix(p, ".go") {
			continue
		}
		if _, generated := base[p]; generated {
			continue
		}
		g, err := parseGoSource(p, ours[p])
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		owned[p] = g
		for _, name := range fileDeclarations(g) {
			declared[name] = p
		}
	}

	routes, err := parseGoSource(routesPath, ours[routesPath])
	if err != nil {
		return nil, nil, []string{err.Error()}
	}
	routesImports := routes.imports()
	need := map[string]bool{}
	for _, expr := range exprs {
		e, err := parser.ParseExpr(expr)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: can't read the middleware %s: %v", routesPath, expr, err))
			continue
		}
		for name := range exprIdents(e) {
			if _, isImport := routesImports[name]; !isImport {
				need[name] = true
			}
		}
	}
	files := map[string][]byte{}
	var moved []string
	seen := map[string]bool{}
	for len(need) > 0 {
		name := slices.Sorted(maps.Keys(need))[0]
		delete(need, name)
		p, ok := declared[name]
		if !ok {
			if types.Universe.Lookup(name) == nil {
				problems = append(problems, fmt.Sprintf("%s: the middleware uses %s, which internal/app declares outside the files orb upgrade can move; move it into cmd/api by hand and add gorbital.WithMiddleware or gorbital.WithStack to main.go", routesPath, name))
			}
			continue
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		g := owned[p]
		for _, decl := range fileDeclarations(g) {
			if cmdDecls[decl] {
				problems = append(problems, fmt.Sprintf("%s declares %s, which cmd/api also declares; rename one and convert again", p, decl))
			}
		}
		imports := g.imports()
		for _, ident := range g.file.Unresolved {
			if _, isImport := imports[ident.Name]; !isImport {
				need[ident.Name] = true
			}
		}
		target := "cmd/api/" + pathBase(p)
		content, err := movedFile(g, target)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		files[target] = content
		moved = append(moved, p+" → "+target)
	}
	slices.Sort(moved)
	return files, moved, problems
}

// fileDeclarations returns the package-level names a file declares.
func fileDeclarations(g *goSource) []string {
	var names []string
	for _, decl := range g.file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil {
				names = append(names, d.Name.Name)
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					names = append(names, s.Name.Name)
				case *ast.ValueSpec:
					for _, ident := range s.Names {
						names = append(names, ident.Name)
					}
				}
			}
		}
	}
	return names
}

// exprIdents returns the identifiers an expression names, package
// qualifiers included.
func exprIdents(e ast.Expr) map[string]bool {
	out := map[string]bool{}
	ast.Inspect(e, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.SelectorExpr:
			if x, ok := node.X.(*ast.Ident); ok {
				out[x.Name] = true
			}
			return false
		case *ast.Ident:
			out[node.Name] = true
		}
		return true
	})
	return out
}

// movedFile returns a file of package app as a file of cmd/api's package
// main, with a line saying where it came from.
func movedFile(g *goSource, target string) ([]byte, error) {
	start := g.offset(g.file.Name.Pos())
	end := g.offset(g.file.Name.End())
	header := fmt.Sprintf("// Moved from %s by orb upgrade --layout v0.2 (UPGRADE-v0.2.md).\n", g.name)
	src := slices.Concat(g.src[:start], []byte("main"), g.src[end:])
	out, err := format.Source(slices.Concat([]byte(header), src))
	if err != nil {
		return nil, fmt.Errorf("move %s to %s: %w", g.name, target, err)
	}
	return out, nil
}

// renderStackFile writes cmd/api/stack.go: the order of the v0.1 app's
// middleware, with the app's own steps where they ran.
func renderStackFile(app appInfo, entries []string, baseRoutes, ourRoutes []byte) ([]byte, error) {
	g, err := parseGoSource(routesPath, ourRoutes)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("package main\n\nimport (\n\t\"net/http\"\n\n\t\"" + gorbitalImportPath + "\"\n)\n\n")
	b.WriteString("// stack is the middleware of the app, in the order the v0.1 layout's\n" +
		"// internal/app/routes.go ran it, with the app's own steps where they were;\n" +
		"// orb upgrade --layout v0.2 wrote it (UPGRADE-v0.2.md). gorbital.Stack.Default\n" +
		"// is the order a new app runs; every field of gorbital.Stack is one step.\n")
	b.WriteString("func stack(s gorbital.Stack) []func(http.Handler) http.Handler {\n\treturn []func(http.Handler) http.Handler{\n")
	for _, e := range entries {
		b.WriteString("\t\t" + e + ",\n")
	}
	b.WriteString("\t}\n}\n")

	formatted, err := format.Source([]byte(b.String()))
	if err != nil {
		return nil, fmt.Errorf("the generated cmd/api/stack.go doesn't parse: %w", err)
	}
	// The app's own steps may name packages routes.go imported.
	var specs []importSpec
	used := map[string]bool{}
	for _, e := range entries {
		if strings.HasPrefix(e, "s.") {
			continue
		}
		expr, err := parser.ParseExpr(e)
		if err != nil {
			continue
		}
		for name := range exprIdents(expr) {
			used[name] = true
		}
	}
	imports := g.imports()
	for _, name := range slices.Sorted(maps.Keys(imports)) {
		if !used[name] {
			continue
		}
		spec := importSpec{path: imports[name]}
		if name != pathBase(imports[name]) {
			spec.name = name
		}
		specs = append(specs, spec)
	}
	return fixImports("cmd/api/stack.go", formatted, app.module, specs)
}

// addMainOption adds an option to the list cmd/api/main.go returns, with a
// comment saying what it does.
func addMainOption(src []byte, option, comment string) ([]byte, error) {
	g, err := parseGoSource("cmd/api/main.go", src)
	if err != nil {
		return nil, err
	}
	var lit *ast.CompositeLit
	ast.Inspect(g.file, func(n ast.Node) bool {
		ret, ok := n.(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			return true
		}
		c, ok := ret.Results[0].(*ast.CompositeLit)
		if !ok {
			return true
		}
		if arr, ok := c.Type.(*ast.ArrayType); ok && g.isSelector(arr.Elt, gorbitalImportPath, "Option") {
			lit = c
		}
		return true
	})
	if lit == nil {
		return nil, fmt.Errorf("cmd/api/main.go doesn't return a []gorbital.Option list to add %s to", option)
	}
	at := g.offset(lit.Rbrace)
	text := "\t" + option + ","
	if comment != "" {
		text += " // " + comment
	}
	return applySrcEdits("cmd/api/main.go", src, []srcEdit{{at, at, text + "\n"}})
}
