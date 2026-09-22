package cli

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"

	"gorbital.dev/cli/internal/recipes"
	"gorbital.dev/cli/internal/routes"
)

// orb explain.
//
// orb routes answers "what routes are there". orb explain answers "what
// happens when this one is called": the guards a request passes, the scope
// its records belong to, the code that serves it, and the files to read
// next. It joins the route's own record — the same one orb routes prints,
// from the app's OpenAPI document and its source — with the files of the
// module that serves it.
//
// It reads and prints; it changes nothing. And it reports only what it can
// see: the guards the document records, the positions the scan found, and
// the files the generators wrote. A handler that reaches further at run
// time is not in the output, and the output says as much rather than
// implying the list is the whole story.

const explainUsage = `Usage: orb explain <subject> [flags]

Explains one thing about the app in the current directory.

Subjects:
  orb explain route <METHOD> <PATH>
        what a request to this route passes through: sign-in, guards, the
        scope its records belong to, middleware, the handler, the files
        that serve it and the tests that cover it.

  orb explain permission <name>
        every route that requires this permission, and where the app
        declares it.

  orb explain scope [<module>]
        what a tenant is called here (ADR-0088), the column its records
        carry, and the access rule each generated module was created with
        (ADR-0091). With a module, only that one.

Routes come from the app's OpenAPI document, built with
go run ./cmd/api openapi, or read with --openapi. Guards come from
x-gorbital-guards, so an app on the v0.1 layout lists none.

What it does not do. It does not run the app, read the database or follow a
handler's calls, so it describes the route as the app declares it, not every
effect a request has. Run "orb doctor" for whether the app matches what it
declares, and "orb doctor --security" for the code it owns.
`

func runExplain(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("orb explain", flag.ContinueOnError)
	flags.SetOutput(stderr)
	asJSON := flags.Bool("json", false, "print the explanation as JSON")
	file := flags.String("openapi", "", "read this OpenAPI document, such as api/openapi.json, instead of building the app")
	flags.Bool("no-input", false, "never prompt (orb explain never does)")
	flags.Usage = func() {
		fmt.Fprint(stderr, explainUsage+"\nFlags:\n")
		flags.PrintDefaults()
	}
	// A subject's arguments come before its flags — "orb explain route GET
	// /v1/projects --json" is how it reads — and flag.Parse stops at the
	// first argument that isn't a flag, so the two are split here first.
	subjectArgs, flagArgs := splitFlags(args)
	if err := flags.Parse(flagArgs); err != nil {
		return err
	}
	if len(subjectArgs) == 0 {
		return usageError("orb explain needs a subject: route, permission or scope")
	}
	app, err := findApp()
	if err != nil {
		return err
	}
	rest := subjectArgs[1:]
	switch subject := subjectArgs[0]; subject {
	case "route":
		return explainRoute(ctx, app, *file, rest, *asJSON, stdout)
	case "permission":
		return explainPermission(ctx, app, *file, rest, *asJSON, stdout)
	case "scope":
		return explainScope(app, rest, *asJSON, stdout)
	default:
		return usageError(fmt.Sprintf("unknown subject %q (want route, permission or scope)", subject))
	}
}

// routeExplanation is orb explain route --json. Its shape is public API.
type routeExplanation struct {
	Route routes.Route `json:"route"`
	// SignIn is "required" or "public".
	SignIn string `json:"sign_in"`
	// Permissions are the permissions the route's guards require.
	Permissions []string `json:"permissions"`
	// Membership reports a guard.OrgMember route: the caller must belong to
	// the tenant in the path, not only hold the permission.
	Membership bool `json:"membership"`
	// ScopeColumn is the column a tenant module's rows carry, such as
	// "org_id"; empty for a route whose records belong to no tenant.
	ScopeColumn string `json:"scope_column,omitempty"`
	// RowLevelSecurity reports gorbital.yaml recording orb add rls.
	RowLevelSecurity bool        `json:"row_level_security"`
	Files            moduleFiles `json:"files"`
	// Notes are what the explanation could not determine, in the words it
	// prints them.
	Notes []string `json:"notes,omitempty"`
}

// moduleFiles are the files of the module serving a route that the reader
// has not already been given, relative to the app: where its rows are
// queried, who decides access to them, and what covers that in tests. The
// route's own delivery files are not here — Code names them with the line
// the route is registered on. Every field is empty for a library module, or
// one whose files the generators did not write.
type moduleFiles struct {
	Policy     string   `json:"policy,omitempty"`
	Repository []string `json:"repository,omitempty"`
	Tests      []string `json:"tests,omitempty"`
}

func explainRoute(ctx context.Context, app appInfo, file string, args []string, asJSON bool, stdout io.Writer) error {
	if len(args) != 2 {
		return usageError(`orb explain route needs a method and a path, as in: orb explain route GET /v1/projects`)
	}
	method, path := strings.ToUpper(args[0]), args[1]
	list, err := appRoutes(ctx, app.dir, file)
	if err != nil {
		return err
	}
	list = list.Scopes(appModuleScopes(app.dir), appVocabulary(app.dir).Name)
	route, err := matchRoute(list, method, path)
	if err != nil {
		return err
	}

	out := routeExplanation{
		Route:            route,
		SignIn:           "required",
		Permissions:      routePermissions(route.Guards),
		Membership:       requiresMembership(route.Guards),
		RowLevelSecurity: appRowLevelSecurity(app.dir),
		Files:            readModuleFiles(app.dir, route.Module, route.Source),
	}
	if route.Public {
		out.SignIn = "public"
	}
	if route.Scope != "" && route.Scope != recipes.ScopeUser && route.Scope != recipes.ScopePublic && route.Scope != recipes.ScopeCustom {
		out.ScopeColumn = appVocabulary(app.dir).Column
	}
	if !list.GuardsKnown {
		out.Notes = append(out.Notes, "this app's OpenAPI document records no x-gorbital-guards, so no guards are listed: apps on the v0.1 layout record none")
	}
	if route.Source == nil {
		out.Notes = append(out.Notes, "this route is registered by a library module, such as authhttp or opshttp, so it has no position in the app's source")
	}
	if route.Scope == recipes.ScopeCustom && out.Files.Policy == "" {
		out.Notes = append(out.Notes, "the module's records are --scope custom and it has no policy.go, so nothing here states who may read and change them")
	}
	if asJSON {
		return writeJSON(stdout, out)
	}
	writeRouteExplanation(stdout, out)
	return nil
}

// matchRoute returns the route for method and path, or an error naming the
// paths that are close to it.
func matchRoute(list routes.List, method, path string) (routes.Route, error) {
	var samePath []string
	for _, r := range list.Routes {
		if r.Path == path {
			if r.Method == method {
				return r, nil
			}
			samePath = append(samePath, r.Method)
		}
	}
	if len(samePath) > 0 {
		slices.Sort(samePath)
		return routes.Route{}, fmt.Errorf("no %s %s: this path serves %s", method, path, strings.Join(samePath, ", "))
	}
	// A path is usually mistyped by leaving out the part in the middle —
	// "/v1/projects" for "/v1/orgs/{orgId}/projects" — so routes are near
	// when they share a segment that isn't a parameter.
	wanted := pathSegments(path)
	var near []string
	for _, r := range list.Routes {
		if slices.ContainsFunc(pathSegments(r.Path), func(s string) bool {
			return slices.ContainsFunc(wanted, func(w string) bool { return nearSegment(s, w) })
		}) {
			near = append(near, r.Method+" "+r.Path)
		}
	}
	slices.Sort(near)
	near = slices.Compact(near)
	msg := fmt.Sprintf("no route %s %s in this app", method, path)
	if len(near) > 0 {
		msg += "\n  did you mean:\n    " + strings.Join(near[:min(len(near), 5)], "\n    ")
		return routes.Route{}, errors.New(msg)
	}
	return routes.Route{}, errors.New(msg + `; run "orb routes" for every route`)
}

// The guard prefixes that carry a permission name: guard.Permission writes
// the first, and guard.OrgMember the second, which requires the permission
// *and* membership of the tenant in the path (gorbital/guard/guard.go).
var permissionGuards = []string{"permission:", "org_member:"}

// routePermissions returns the permissions a route's guards require, from
// either prefix, in the order the guards list them.
func routePermissions(guards []string) []string {
	var out []string
	for _, g := range guards {
		for _, prefix := range permissionGuards {
			if v, ok := strings.CutPrefix(g, prefix); ok {
				out = append(out, v)
				break
			}
		}
	}
	return out
}

// requiresMembership reports a route guarded by guard.OrgMember, which
// refuses a caller who isn't a member of the tenant in the path even when
// they hold the permission.
func requiresMembership(guards []string) bool {
	return slices.ContainsFunc(guards, func(g string) bool {
		return strings.HasPrefix(g, "org_member:")
	})
}

// readModuleFiles returns the files of module worth reading after a route
// of it, relative to the app, and the zero value for a library module or
// one with no directory. source is where the route is registered, so the
// tests beside it can be told from the module's others.
func readModuleFiles(dir, module string, source *routes.Pos) moduleFiles {
	if module == "" {
		return moduleFiles{}
	}
	root := filepath.Join(dir, filepath.FromSlash(recipes.ModulesDir), module)
	if _, err := os.Stat(root); err != nil {
		return moduleFiles{}
	}
	// routeDir is the directory the route is registered in: its tests are
	// the ones that cover this route, rather than the whole module's.
	routeDir := ""
	if source != nil {
		routeDir = filepath.ToSlash(filepath.Dir(source.File)) + "/"
	}
	var files moduleFiles
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil //nolint:nilerr // a file orb can't read is one it says nothing about
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return nil //nolint:nilerr // a path orb can't name relative to the app is one it leaves out, not one that stops the walk
		}
		rel = filepath.ToSlash(rel)
		base := filepath.Base(path)
		switch {
		case base == "policy.go":
			files.Policy = rel
		case base == "policy_test.go", routeDir != "" && strings.HasPrefix(rel, routeDir) && strings.HasSuffix(base, "_test.go"):
			files.Tests = append(files.Tests, rel)
		case strings.HasSuffix(base, "_test.go"):
			// a test elsewhere in the module: not this route's.
		case strings.Contains(rel, "/repository/"):
			files.Repository = append(files.Repository, rel)
		}
		return nil
	})
	slices.Sort(files.Repository)
	slices.Sort(files.Tests)
	return files
}

func writeRouteExplanation(w io.Writer, e routeExplanation) {
	r := e.Route
	fmt.Fprintf(w, "%s %s\n", r.Method, r.Path)
	if r.Summary != "" {
		fmt.Fprintf(w, "  %s\n", r.Summary)
	}
	if r.Deprecated {
		fmt.Fprintln(w, "  deprecated")
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	section(tw, "Access")
	fmt.Fprintf(tw, "  sign-in\t%s\n", e.SignIn)
	fmt.Fprintf(tw, "  guards\t%s\n", dash(strings.Join(r.Guards, ", ")))
	if len(e.Permissions) > 0 {
		fmt.Fprintf(tw, "  permissions\t%s\n", strings.Join(e.Permissions, ", "))
	}
	if e.Membership {
		fmt.Fprintf(tw, "  membership\trequired: the caller must belong to the %s in the path\n", cmp.Or(e.Route.Scope, "tenant"))
	}
	fmt.Fprintf(tw, "  scope\t%s\n", dash(r.Scope))
	if e.ScopeColumn != "" {
		fmt.Fprintf(tw, "  scope column\t%s (every query of this module's rows should carry it)\n", e.ScopeColumn)
		rls := "off — the query is the only thing keeping one tenant's rows from another's"
		if e.RowLevelSecurity {
			rls = "on (orb add rls)"
		}
		fmt.Fprintf(tw, "  row-level security\t%s\n", rls)
	}

	section(tw, "Request")
	fmt.Fprintf(tw, "  middleware\t%s\n", dash(strings.Join(r.Middleware, " -> ")))

	section(tw, "Code")
	fmt.Fprintf(tw, "  module\t%s\n", dash(r.Module))
	fmt.Fprintf(tw, "  operation\t%s\n", dash(r.OperationID))
	fmt.Fprintf(tw, "  handler\t%s\n", dash(r.Handler))
	fmt.Fprintf(tw, "  registered at\t%s\n", dash(r.Source.String()))
	fmt.Fprintf(tw, "  handler at\t%s\n", dash(r.HandlerSource.String()))
	_ = tw.Flush()

	writeFileList(w, "Files", e.Files)
	for _, note := range e.Notes {
		fmt.Fprintf(w, "\nnote: %s\n", note)
	}
}

func writeFileList(w io.Writer, heading string, f moduleFiles) {
	if f.Policy == "" && len(f.Repository) == 0 && len(f.Tests) == 0 {
		return
	}
	fmt.Fprintf(w, "\n%s\n", heading)
	for _, group := range []struct {
		label string
		paths []string
	}{
		{"policy", []string{f.Policy}},
		{"repository", f.Repository},
		{"tests", f.Tests},
	} {
		for _, p := range group.paths {
			if p != "" {
				fmt.Fprintf(w, "  %-11s %s\n", group.label, p)
			}
		}
	}
}

func section(w io.Writer, name string) { fmt.Fprintf(w, "\n%s\n", name) }

// permissionExplanation is orb explain permission --json.
type permissionExplanation struct {
	Permission string `json:"permission"`
	// Routes are the routes whose guards require it.
	Routes []routes.Route `json:"routes"`
	// DeclaredAt is where the app names the permission, as file:line.
	DeclaredAt []string `json:"declared_at,omitempty"`
	Notes      []string `json:"notes,omitempty"`
}

func explainPermission(ctx context.Context, app appInfo, file string, args []string, asJSON bool, stdout io.Writer) error {
	if len(args) != 1 {
		return usageError(`orb explain permission needs a name, as in: orb explain permission projects.project.read`)
	}
	name := args[0]
	list, err := appRoutes(ctx, app.dir, file)
	if err != nil {
		return err
	}
	list = list.Scopes(appModuleScopes(app.dir), appVocabulary(app.dir).Name)

	out := permissionExplanation{Permission: name, DeclaredAt: findLiteral(app.dir, name)}
	for _, r := range list.Routes {
		if slices.Contains(routePermissions(r.Guards), name) {
			out.Routes = append(out.Routes, r)
		}
	}
	if !list.GuardsKnown {
		out.Notes = append(out.Notes, "this app's OpenAPI document records no x-gorbital-guards, so no route can be matched to a permission")
	} else if len(out.Routes) == 0 {
		out.Notes = append(out.Notes, "no route's guards require this permission: check the name with \"orb routes\"")
	}
	if len(out.DeclaredAt) == 0 {
		out.Notes = append(out.Notes, "the app's source doesn't name this permission as a literal, so orb can't say where it is declared")
	}
	if asJSON {
		return writeJSON(stdout, out)
	}

	fmt.Fprintf(stdout, "%s\n", name)
	if len(out.DeclaredAt) > 0 {
		fmt.Fprintln(stdout, "\nDeclared at")
		for _, p := range out.DeclaredAt {
			fmt.Fprintf(stdout, "  %s\n", p)
		}
	}
	if len(out.Routes) > 0 {
		fmt.Fprintln(stdout, "\nRequired by")
		tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "  METHOD\tPATH\tMODULE\tSCOPE\tSOURCE")
		for _, r := range out.Routes {
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n", r.Method, r.Path, dash(r.Module), dash(r.Scope), dash(r.Source.String()))
		}
		_ = tw.Flush()
	}
	for _, note := range out.Notes {
		fmt.Fprintf(stdout, "\nnote: %s\n", note)
	}
	return nil
}

// findLiteral returns the positions, as file:line, where the app's own Go
// source and gorbital.yaml name literal as a quoted string. It reads the
// app's files, not its dependencies: internal/, cmd/ and the manifest.
func findLiteral(dir, literal string) []string {
	quoted := `"` + literal + `"`
	var found []string
	for _, sub := range []string{"internal", "cmd"} {
		root := filepath.Join(dir, sub)
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil //nolint:nilerr // a file orb can't read is one it says nothing about
			}
			src, readErr := os.ReadFile(path) //nolint:gosec // the app's own source
			if readErr != nil {
				return nil //nolint:nilerr // a file orb can't read is one it says nothing about, not one that stops the walk
			}
			rel, relErr := filepath.Rel(dir, path)
			if relErr != nil {
				return nil //nolint:nilerr // as above: skip the file, keep walking
			}
			for i, line := range strings.Split(string(src), "\n") {
				if strings.Contains(line, quoted) {
					found = append(found, fmt.Sprintf("%s:%d", filepath.ToSlash(rel), i+1))
				}
			}
			return nil
		})
	}
	slices.Sort(found)
	return found
}

// scopeExplanation is orb explain scope --json.
type scopeExplanation struct {
	// Tenant is what this app calls a tenant, and the words its routes,
	// columns and code use for it (ADR-0088).
	Tenant scopeVocabulary `json:"tenant"`
	// RowLevelSecurity reports gorbital.yaml recording orb add rls.
	RowLevelSecurity bool `json:"row_level_security"`
	// Modules is the access rule each generated module was created with,
	// keyed by module name (ADR-0091).
	Modules map[string]string `json:"modules"`
	Notes   []string          `json:"notes,omitempty"`
}

type scopeVocabulary struct {
	Name      string `json:"name"`
	Plural    string `json:"plural"`
	Segment   string `json:"segment"`
	PathParam string `json:"path_param"`
	Column    string `json:"column"`
}

// scopeMeaning is what each --scope value means, in one line.
var scopeMeaning = map[string]string{
	recipes.ScopeUser:   "the signed-in user's own records: nobody else reads or changes them",
	recipes.ScopeTenant: "a tenant's records, reached through a member's role",
	recipes.ScopePublic: "world-readable records; writes are permission-guarded",
	recipes.ScopeCustom: "the module decides, in its own policy.go",
}

func explainScope(app appInfo, args []string, asJSON bool, stdout io.Writer) error {
	if len(args) > 1 {
		return usageError("orb explain scope takes at most one module name")
	}
	vocab := appVocabulary(app.dir)
	out := scopeExplanation{
		Tenant: scopeVocabulary{
			Name:      vocab.Name,
			Plural:    vocab.Plural,
			Segment:   vocab.Segment,
			PathParam: vocab.PathParam,
			Column:    vocab.Column,
		},
		RowLevelSecurity: appRowLevelSecurity(app.dir),
		Modules:          appModuleScopes(app.dir),
	}
	if len(args) == 1 {
		scope, ok := out.Modules[args[0]]
		if !ok {
			return fmt.Errorf("gorbital.yaml records no module %q; run \"orb explain scope\" for the modules it does record", args[0])
		}
		out.Modules = map[string]string{args[0]: scope}
	}
	if len(out.Modules) == 0 {
		out.Notes = append(out.Notes, "gorbital.yaml records no module scopes: apps up to v0.2.1 record none, and orb gen module records one from v0.3.0")
	}
	if !out.RowLevelSecurity {
		out.Notes = append(out.Notes, "row-level security is off, so a tenant module's query is the only thing keeping one tenant's rows from another's: see \"orb add rls\"")
	}
	out.Notes = append(out.Notes, "this is what gorbital.yaml records, not what the code does: \"orb doctor\" checks the two against each other")
	if asJSON {
		return writeJSON(stdout, out)
	}

	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "A tenant here is %s %s.\n", article(out.Tenant.Name), out.Tenant.Name)
	fmt.Fprintln(tw, "\nVocabulary")
	fmt.Fprintf(tw, "  name\t%s\n", out.Tenant.Name)
	fmt.Fprintf(tw, "  plural\t%s\n", out.Tenant.Plural)
	fmt.Fprintf(tw, "  path segment\t/%s/{%s}\n", out.Tenant.Segment, out.Tenant.PathParam)
	fmt.Fprintf(tw, "  table column\t%s\n", out.Tenant.Column)
	_ = tw.Flush()

	if len(out.Modules) > 0 {
		fmt.Fprintln(stdout, "\nModules")
		mw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(mw, "  MODULE\tSCOPE\tMEANING")
		for _, name := range slices.Sorted(maps.Keys(out.Modules)) {
			scope := out.Modules[name]
			fmt.Fprintf(mw, "  %s\t%s\t%s\n", name, scope, scopeMeaning[scope])
		}
		_ = mw.Flush()
	}
	for _, note := range out.Notes {
		fmt.Fprintf(stdout, "\nnote: %s\n", note)
	}
	return nil
}

// splitFlags returns the leading arguments that aren't flags, and the rest
// starting at the first one that is. A path argument starts with "/" and a
// method with a letter, so neither is mistaken for a flag.
func splitFlags(args []string) (leading, flags []string) {
	for i, a := range args {
		if strings.HasPrefix(a, "-") {
			return args[:i], args[i:]
		}
	}
	return args, nil
}

// pathSegments returns a path's segments that aren't parameters, so
// "/v1/orgs/{orgId}/projects" is v1, orgs, projects. Version segments are
// left out: every route shares them, so they make everything look near.
func pathSegments(path string) []string {
	var out []string
	for _, s := range strings.Split(path, "/") {
		if s == "" || strings.HasPrefix(s, "{") || isVersionSegment(s) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// isVersionSegment reports "v1", "v2" and the like.
func isVersionSegment(s string) bool {
	rest, ok := strings.CutPrefix(s, "v")
	return ok && rest != "" && strings.IndexFunc(rest, func(r rune) bool { return r < '0' || r > '9' }) < 0
}

// nearSegment reports two path segments close enough that one is probably
// the other misremembered. Whole words are compared by the start they
// share, not by one being a prefix of the other: the singular of "shelves"
// is "shelf", which is not its prefix, and neither is the plural of
// "person". Four characters is the shortest start that isn't noise —
// "profiles" and "projects" share three.
const nearSegmentPrefix = 4

func nearSegment(a, b string) bool {
	if len(a) < nearSegmentPrefix || len(b) < nearSegmentPrefix {
		return a == b
	}
	for i := range min(len(a), len(b)) {
		if a[i] != b[i] {
			return i >= nearSegmentPrefix
		}
	}
	return true // one is a prefix of the other, as "book" is of "books"
}
