package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gorbital.dev/cli/internal/genplan"
	"gorbital.dev/cli/internal/recipes"
)

const genMiddlewareUsage = `Usage: orb gen middleware <Name> (--module <name> [--guard] | --global) [flags]

Generates HTTP middleware, or a guard, with a table-driven test, for an app
on gorbital.Main (ADR-0082). It writes new files only: add the middleware
where it belongs with the line it prints.

  --module books             internal/modules/books/delivery/<name>.go:
                             func(http.Handler) http.Handler for gorbital.Use
                             in routes.go or Middleware in module.go
  --module books --guard     internal/modules/books/delivery/<name>.go: a
                             guard.New guard and the error it refuses with,
                             to add to routes in routes.go and map in module.go
  --global                   internal/middleware/<name>.go: middleware for
                             every route, added in main.go with
                             gorbital.WithMiddleware

For example:
  orb gen middleware RequireClientVersion --module books
  orb gen middleware ActiveSubscription --module books --guard
  orb gen middleware TenantHeader --global
`

type genMiddlewareResult struct {
	Name string `json:"name"`
	// Kind is module, global or guard.
	Kind string `json:"kind"`
	// Module is the module the file belongs to; empty for global middleware.
	Module  string `json:"module"`
	Package string `json:"package"`
	File    string `json:"file"`
	Test    string `json:"test"`
	// Files lists every file written, including internal/middleware/doc.go
	// when the package has none.
	Files []string `json:"files"`
	// Wire is the Go code that puts it to use; orb doesn't add it.
	Wire   string `json:"wire"`
	DryRun bool   `json:"dry_run"`
}

// middlewareInput holds orb gen middleware's answers.
type middlewareInput struct {
	name   string
	module string
	global bool
	guard  bool
}

func runGenMiddleware(ctx context.Context, args []string, _ io.Reader, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("orb gen middleware", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var in middlewareInput
	flags.StringVar(&in.module, "module", "", "the module whose delivery package gets the middleware, such as books")
	flags.BoolVar(&in.global, "global", false, "middleware for every route, in internal/middleware")
	flags.BoolVar(&in.guard, "guard", false, "a guard (guard.New) with its error, instead of middleware; needs --module")
	dryRun := flags.Bool("dry-run", false, "show what would be generated without writing")
	diff := flags.Bool("diff", false, "print the plan as a unified diff")
	asJSON := flags.Bool("json", false, "print the result as JSON")
	allowDirty := flags.Bool("allow-dirty", false, "allow uncommitted changes in the git repository")
	flags.Bool("no-input", false, "never prompt (orb gen middleware never does)")
	flags.Usage = func() {
		fmt.Fprint(stderr, genMiddlewareUsage+"\nFlags:\n")
		flags.PrintDefaults()
	}
	positional, err := parseInterspersed(flags, args)
	if err != nil {
		return err
	}
	switch len(positional) {
	case 0:
		return usageError("missing name: orb gen middleware <Name> --module <name> or --global")
	case 1:
		in.name = positional[0]
	default:
		return usageError(fmt.Sprintf("unexpected arguments: %s", strings.Join(positional[1:], " ")))
	}
	app, err := findApp()
	if err != nil {
		return err
	}
	plan, err := planMiddleware(app, in)
	if err != nil {
		return err
	}
	result := plan.Result.(genMiddlewareResult)
	result.DryRun = *dryRun
	if !*dryRun {
		if !*allowDirty {
			if err := requireCleanGit(ctx, app.dir); err != nil {
				return err
			}
		}
		if err := genplan.Apply(app.dir, plan); err != nil {
			return err
		}
	}
	if *asJSON {
		return writeJSON(stdout, result)
	}
	verb := "Created"
	if *dryRun {
		verb = "Would create (dry run)"
	}
	what := "middleware"
	if result.Kind == recipes.MiddlewareGuard {
		what = "guard"
	}
	fmt.Fprintf(stdout, "✓ %s %s %s\n\n%s\n", verb, what, result.Name, plan.Summary)
	if *diff {
		fmt.Fprintf(stdout, "\n%s", genplan.Diff(plan))
	}
	if !*dryRun {
		fmt.Fprintf(stdout, "\nNext:\n%s\n", numbered(plan.Next))
	}
	return nil
}

// middlewareKind returns the kind in's flags ask for, or a usage error.
func middlewareKind(in middlewareInput) (string, error) {
	switch {
	case in.global && in.module != "":
		return "", usageError("--global and --module exclude each other: global middleware runs for every route, module middleware in one module")
	case in.guard && in.global:
		return "", usageError("--guard needs --module, not --global: a guard is a route option, and its error is mapped by a module")
	case in.global:
		return recipes.MiddlewareGlobal, nil
	case in.module == "":
		return "", usageError("say where the middleware goes: --module <name> (a module's routes) or --global (every route)")
	case in.guard:
		return recipes.MiddlewareGuard, nil
	}
	return recipes.MiddlewareModule, nil
}

// planMiddleware returns what orb gen middleware would write for in.
func planMiddleware(app appInfo, in middlewareInput) (genplan.Plan, error) {
	kind, err := middlewareKind(in)
	if err != nil {
		return genplan.Plan{}, err
	}
	switch appLayout(app.dir) {
	case layoutV01:
		return genplan.Plan{}, errors.New("this app uses the v0.1 layout (internal/app): orb gen middleware writes middleware for apps on gorbital.Main; in a v0.1 app, add middleware to the stack in internal/app/routes.go (docs/guides/middleware-stack.md)")
	case "":
		return genplan.Plan{}, fmt.Errorf("%s isn't an app on gorbital.Main: orb gen middleware needs gorbital.dev/gorbital in go.mod", app.dir)
	}
	data, err := recipes.NewMiddlewareData(app.module, in.name, kind, in.module)
	if err != nil {
		return genplan.Plan{}, usageError(err.Error())
	}
	if kind != recipes.MiddlewareGlobal {
		if info, err := os.Stat(filepath.Join(app.dir, filepath.FromSlash(data.Dir()))); err != nil || !info.IsDir() {
			return genplan.Plan{}, fmt.Errorf("%s doesn't exist: --module names a module with a delivery package, such as one orb gen module wrote", data.Dir())
		}
	}
	declared, err := packageDeclarations(filepath.Join(app.dir, filepath.FromSlash(data.Dir())))
	if err != nil {
		return genplan.Plan{}, err
	}
	for _, name := range data.Declared() {
		if declared[name] {
			return genplan.Plan{}, fmt.Errorf("package %s already declares %s; choose another name", data.Dir(), name)
		}
	}
	files, err := recipes.RenderMiddleware(data)
	if err != nil {
		return genplan.Plan{}, err
	}
	if _, err := os.Stat(filepath.Join(app.dir, filepath.FromSlash(recipes.MiddlewareDocPath))); kind == recipes.MiddlewareGlobal && errors.Is(err, fs.ErrNotExist) {
		doc, err := recipes.RenderMiddlewareDoc()
		if err != nil {
			return genplan.Plan{}, err
		}
		files = append(files, doc)
	}
	plan := genplan.Plan{Generator: "middleware", Name: data.Ident}
	root, err := os.OpenRoot(app.dir)
	if err != nil {
		return genplan.Plan{}, err
	}
	defer root.Close()
	for _, f := range files {
		if _, err := root.Stat(filepath.FromSlash(f.Path)); err == nil {
			return genplan.Plan{}, fmt.Errorf("%s already exists; choose another name", f.Path)
		}
		plan.Changes = append(plan.Changes, genplan.Change{Path: f.Path, Kind: genplan.Create, Content: f.Content})
	}

	test := "go test ./" + data.Dir()
	switch kind {
	case recipes.MiddlewareGlobal:
		plan.Next = []string{
			fmt.Sprintf("Write the rule in %s (%s)", data.File(), data.Check()),
			fmt.Sprintf("Add %s to gorbital.Main in cmd/api/main.go, importing %q", data.Wire(), app.module+"/"+recipes.MiddlewareDir),
			test,
		}
	case recipes.MiddlewareGuard:
		plan.Next = []string{
			fmt.Sprintf("Write the rule in %s (%s)", data.File(), data.Check()),
			fmt.Sprintf("Map its error in %s/%s/module.go's Errors: %s", recipes.ModulesDir, data.ModuleName, data.ErrorMapping()),
			fmt.Sprintf("Add %s to the routes it protects in %s/routes.go", data.Wire(), data.Dir()),
			test,
		}
	default:
		plan.Next = []string{
			fmt.Sprintf("Write the rule in %s (%s)", data.File(), data.Check()),
			fmt.Sprintf("Add %s to a group or route in %s/routes.go, or delivery.%s to Middleware in %s/%s/module.go", data.Wire(), data.Dir(), data.Ident, recipes.ModulesDir, data.ModuleName),
			test,
		}
	}
	plan.Summary = middlewareSummary(data, plan)
	plan.Result = genMiddlewareResult{
		Name: data.Ident, Kind: kind, Module: data.ModuleName, Package: data.Package(),
		File: data.File(), Test: data.Test(), Files: plan.Paths(), Wire: data.Wire(),
	}
	return plan, nil
}

// packageDeclarations returns the package-level names the Go files in dir
// declare, tests included, or none when dir doesn't exist.
func packageDeclarations(dir string) (map[string]bool, error) {
	names := map[string]bool{}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return names, nil
	} else if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", filepath.Join(dir, e.Name()), err)
		}
		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				if decl.Recv == nil {
					names[decl.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						names[spec.Name.Name] = true
					case *ast.ValueSpec:
						for _, n := range spec.Names {
							names[n.Name] = true
						}
					}
				}
			}
		}
	}
	return names, nil
}

func middlewareSummary(d recipes.MiddlewareData, plan genplan.Plan) string {
	var b strings.Builder
	switch d.Kind {
	case recipes.MiddlewareGlobal:
		fmt.Fprintf(&b, "  Middleware:  %s, for every route (package middleware)\n", d.Ident)
	case recipes.MiddlewareGuard:
		fmt.Fprintf(&b, "  Guard:       %s() in the %s module, named %s, refusing with %s (403 %s)\n", d.Ident, d.ModuleName, d.Snake, d.Err(), d.Code())
	default:
		fmt.Fprintf(&b, "  Middleware:  %s in the %s module (package delivery)\n", d.Ident, d.ModuleName)
	}
	fmt.Fprintf(&b, "  Use it:      %s\n", d.Wire())
	b.WriteString("  Files:\n")
	for line := range strings.Lines(genplan.Describe(plan)) {
		b.WriteString("  " + line)
	}
	return strings.TrimRight(b.String(), "\n")
}
