package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/huh"

	"gorbital.dev/cli/internal/genplan"
	"gorbital.dev/cli/internal/recipes"
)

const genModuleUsage = `Usage: orb gen module <Name> <field:type>... [flags]

Generates a module for an app on gorbital.Main (ADR-0083): records that
belong to the signed-in user, in internal/modules/<names>/ with four layers
and one file per operation in each:

  module.go                  name, error codes, permissions, routes
  domain/                    the record, its rules, its errors
  usecase/                   service.go, ports.go, create, get, list, update, delete
  repository/                store.go, then one SQL statement per file
  delivery/                  routes.go (the route table), responses.go, one file per operation

with HTTP tests through gorbitaltest, domain tests, a migration in
db/migrations, internal/modules/architecture_test.go when the app has none,
and internal/modules/modules.gen.go updated. It never overwrites a file. The
code is yours to change.

Field types:
  name:string                1 to 100 characters, required and sortable;
                             name:string:unique is unique per user, ignoring case
  nickname:string?           0 to 100 characters, optional and sortable
  notes:text                 up to 2000 characters, optional
  'status:enum(open,done)'   one of the values, the first by default; filterable

The first required string field is the title. For example:
  orb gen module Shelf name:string:unique description:text 'visibility:enum(private,shared)' --plural Shelves

In an app on the v0.1 layout (internal/app/modules.go), use orb gen resource.
`

type genModuleResult struct {
	Name   string `json:"name"`
	Module string `json:"module"`
	Route  string `json:"route"`
	Table  string `json:"table"`
	Scope  string `json:"scope"`
	// Permissions are the read and write permissions the routes require.
	Permissions []string `json:"permissions"`
	Migration   string   `json:"migration"`
	Files       []string `json:"files"`
	DryRun      bool     `json:"dry_run"`
}

// App layouts orb gen tells apart.
const (
	// layoutMain is an app on gorbital.Main: modules in internal/modules,
	// listed by modules.gen.go (ADR-0083).
	layoutMain = "main"
	// layoutV01 is an app wired by internal/app, as orb new wrote them in
	// v0.1.
	layoutV01 = "v0.1"
)

// appLayout returns the layout of the app in dir, or "" when it is
// neither.
func appLayout(dir string) string {
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(modulesGenPath))); err == nil || isGorbitalApp(dir) {
		return layoutMain
	}
	if _, err := os.Stat(filepath.Join(dir, "internal", "app", "modules.go")); err == nil {
		return layoutV01
	}
	return ""
}

// errV01Layout is orb gen module's answer in a v0.1 app.
var errV01Layout = errors.New("this app uses the v0.1 layout (internal/app/modules.go), and orb gen module writes modules for apps on gorbital.Main; use orb gen resource <Name> <field:type>..., which writes the same layers and registers them in internal/app")

// moduleInput holds orb gen module's answers.
type moduleInput struct {
	name     string
	specs    []string
	plural   string
	idPrefix string
	org      bool
}

func runGenModule(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("orb gen module", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var in moduleInput
	flags.StringVar(&in.plural, "plural", "", "plural name when adding -s or -es is wrong, such as Shelves")
	flags.StringVar(&in.idPrefix, "id-prefix", "", "2 to 8 lowercase letters that start every ID (default: derived from the name, such as shl)")
	flags.BoolVar(&in.org, "org", false, "records belong to an organisation (arrives with v0.2 Phase 7; refused until then)")
	dryRun := flags.Bool("dry-run", false, "show what would be generated without writing")
	diff := flags.Bool("diff", false, "print the plan as a unified diff")
	asJSON := flags.Bool("json", false, "print the result as JSON")
	allowDirty := flags.Bool("allow-dirty", false, "allow uncommitted changes in the git repository")
	var p promptFlags
	flags.BoolVar(&p.yes, "yes", false, "use defaults for everything not given by flags, without prompts")
	flags.BoolVar(&p.noInput, "no-input", false, "never prompt; fail if a required value is missing")
	flags.BoolVar(&p.plain, "plain", false, "plain line-by-line prompts (screen-reader friendly)")
	flags.Usage = func() {
		fmt.Fprint(stderr, genModuleUsage+"\nFlags:\n")
		flags.PrintDefaults()
	}
	positional, err := parseInterspersed(flags, args)
	if err != nil {
		return err
	}
	if len(positional) > 0 {
		in.name, in.specs = positional[0], positional[1:]
	}
	app, err := findApp()
	if err != nil {
		return err
	}
	return genModule(ctx, app, in, genModuleRun{dryRun: *dryRun, diff: *diff, asJSON: *asJSON, allowDirty: *allowDirty, prompts: p}, stdin, stdout, stderr)
}

// genModuleRun holds how orb gen module (or orb gen resource in an app on
// gorbital.Main) runs.
type genModuleRun struct {
	dryRun, diff, asJSON, allowDirty bool
	prompts                          promptFlags
}

func genModule(ctx context.Context, app appInfo, in moduleInput, run genModuleRun, stdin io.Reader, stdout, stderr io.Writer) error {
	if err := checkModuleApp(app, in.org); err != nil {
		return err
	}
	ask := shouldPrompt(run.prompts, run.asJSON, stdin, stdout)
	if ask {
		if err := promptModule(&in, run.prompts, stdin, stderr); err != nil {
			return err
		}
	}
	plan, data, err := planModule(app, in, time.Now())
	if err != nil {
		return err
	}
	result := plan.Result.(genModuleResult)
	result.DryRun = run.dryRun

	if !run.dryRun && ask {
		ok, err := confirm("Generate this module?", plan.Summary, run.prompts, stdin, stderr)
		if err != nil {
			return err
		}
		if !ok {
			return errAborted
		}
	}
	if !run.dryRun {
		if !run.allowDirty {
			if err := requireCleanGit(ctx, app.dir); err != nil {
				return err
			}
		}
		if err := genplan.Apply(app.dir, plan); err != nil {
			return err
		}
	}

	if run.asJSON {
		return writeJSON(stdout, result)
	}
	verb := "Created"
	if run.dryRun {
		verb = "Would create (dry run)"
	}
	fmt.Fprintf(stdout, "✓ %s module %s\n\n%s\n", verb, data.Package, plan.Summary)
	if run.diff {
		fmt.Fprintf(stdout, "\n%s", genplan.Diff(plan))
	}
	if !run.dryRun {
		fmt.Fprintf(stdout, "\nNext:\n%s\nThe code is yours: change the rules in %s/domain and the SQL in %s/repository.\n",
			numbered(plan.Next), data.Dir(), data.Dir())
	}
	return nil
}

// checkModuleApp checks that the app can take a module before any question
// is asked.
func checkModuleApp(app appInfo, org bool) error {
	switch appLayout(app.dir) {
	case layoutV01:
		return errV01Layout
	case "":
		return fmt.Errorf("%s isn't an app on gorbital.Main: orb gen module needs gorbital.dev/gorbital in go.mod or internal/modules/modules.gen.go", app.dir)
	}
	if org {
		return usageError("--org: organisation-scoped modules need guard.OrgMember, which arrives with organisations in v0.2 (Phase 7); generate the module without --org, owned by users, for now")
	}
	return nil
}

// promptModule asks for the name and fields when they weren't given.
func promptModule(in *moduleInput, p promptFlags, stdin io.Reader, stderr io.Writer) error {
	var group []huh.Field
	if in.name == "" {
		group = append(group, huh.NewInput().Title("Record name").
			Description("Singular, such as Shelf, Customer or OrderItem. The module is its plural.").
			Placeholder("Shelf").Value(&in.name).Validate(recipes.ValidateModuleName))
	}
	line := strings.Join(in.specs, " ")
	if len(in.specs) == 0 {
		group = append(group, huh.NewInput().Title("Fields").
			Description("Separated by spaces: title:string (add :unique), nickname:string?, notes:text, status:enum(open,done). The first required string field is the title.").
			Placeholder("name:string:unique description:text status:enum(private,shared)").Value(&line).
			Validate(func(s string) error { _, err := recipes.ParseModuleFields(strings.Fields(s)); return err }))
	}
	if len(group) == 0 {
		return nil
	}
	if err := runForm(huh.NewForm(huh.NewGroup(group...)), p, stdin, stderr); err != nil {
		return err
	}
	in.specs = strings.Fields(line)
	return nil
}

// planModule returns what orb gen module would write for in: the module,
// its migration, the architecture test when the app has none, and
// modules.gen.go listing the new module.
func planModule(app appInfo, in moduleInput, now time.Time) (genplan.Plan, recipes.ModuleData, error) {
	var none recipes.ModuleData
	if err := checkModuleApp(app, in.org); err != nil {
		return genplan.Plan{}, none, err
	}
	switch {
	case in.name == "":
		return genplan.Plan{}, none, usageError("missing record name: orb gen module <Name> <field:type>... (or run it in a terminal to be asked)")
	case len(in.specs) == 0:
		return genplan.Plan{}, none, usageError(fmt.Sprintf("missing fields: orb gen module %s name:string ... (orb gen module -h lists the field types)", in.name))
	}
	fields, err := recipes.ParseModuleFields(in.specs)
	if err != nil {
		return genplan.Plan{}, none, usageError(err.Error())
	}
	if err := checkMigrationsPackage(app.dir); err != nil {
		return genplan.Plan{}, none, err
	}
	version, err := nextMigrationVersion(app.dir, now)
	if err != nil {
		return genplan.Plan{}, none, err
	}
	data, err := recipes.NewModuleData(app.module, in.name, fields, recipes.ResourceOptions{Plural: in.plural, IDPrefix: in.idPrefix, Migration: version})
	if err != nil {
		return genplan.Plan{}, none, usageError(err.Error())
	}
	files, err := recipes.RenderModule(data)
	if err != nil {
		return genplan.Plan{}, none, err
	}

	root, err := os.OpenRoot(app.dir)
	if err != nil {
		return genplan.Plan{}, none, err
	}
	defer root.Close()
	if _, err := root.Stat(filepath.FromSlash(data.Dir())); err == nil {
		return genplan.Plan{}, none, fmt.Errorf("%s already exists; choose another name or --plural", data.Dir())
	}
	plan := genplan.Plan{Generator: "module", Name: data.Ident}
	for _, f := range files {
		if _, err := root.Stat(filepath.FromSlash(f.Path)); err == nil {
			return genplan.Plan{}, none, fmt.Errorf("%s already exists; choose another name or --plural", f.Path)
		}
		plan.Changes = append(plan.Changes, genplan.Change{Path: f.Path, Kind: genplan.Create, Content: f.Content})
	}
	if _, err := root.Stat(filepath.FromSlash(recipes.ArchitectureTestPath)); errors.Is(err, fs.ErrNotExist) {
		arch, err := recipes.RenderArchitectureTest(app.module)
		if err != nil {
			return genplan.Plan{}, none, err
		}
		plan.Changes = append(plan.Changes, genplan.Change{Path: arch.Path, Kind: genplan.Create, Content: arch.Content})
	}
	list, err := moduleListChange(app, appModule{dir: data.Package, pkg: data.Package, importPath: data.ImportPath()})
	if err != nil {
		return genplan.Plan{}, none, err
	}
	plan.Changes = append(plan.Changes, list)

	plan.Summary = moduleSummary(data, plan)
	plan.Next = []string{
		"go run ./cmd/api migrate (orb dev runs it)",
		"go run ./cmd/api openapi --dir api",
		"go test ./" + data.Dir() + "/...",
		"go run ./cmd/api, sign in, then POST " + data.RoutePath(),
	}
	if !mainUsesModuleList(app.dir) {
		plan.Next = append([]string{"Add gorbital.WithModules(modules.All()...) to gorbital.Main in cmd/api/main.go, importing " + app.module + "/internal/modules"}, plan.Next...)
	}
	plan.Result = genModuleResult{
		Name: data.Ident, Module: data.Package, Route: data.RoutePath(), Table: data.Table, Scope: recipes.ScopeUser,
		Permissions: []string{data.PermRead(), data.PermWrite()}, Migration: data.MigrationPath(), Files: plan.Paths(),
	}
	return plan, data, nil
}

// moduleListChange returns the change to modules.gen.go that lists added
// with the app's other modules: a new file, or the current one rewritten.
func moduleListChange(app appInfo, added appModule) (genplan.Change, error) {
	var modules []appModule
	if _, err := os.Stat(filepath.Join(app.dir, "internal", "modules")); err == nil {
		if modules, err = findModules(app); err != nil {
			return genplan.Change{}, err
		}
	}
	modules = append(modules, added)
	slices.SortFunc(modules, func(a, b appModule) int { return strings.Compare(a.dir, b.dir) })
	content, err := renderModules(modules)
	if err != nil {
		return genplan.Change{}, err
	}
	before, err := os.ReadFile(filepath.Join(app.dir, filepath.FromSlash(modulesGenPath)))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return genplan.Change{Path: modulesGenPath, Kind: genplan.Create, Content: content}, nil
	case err != nil:
		return genplan.Change{}, err
	}
	return genplan.Change{Path: modulesGenPath, Kind: genplan.Modify, Before: before, Content: content}, nil
}

// checkMigrationsPackage checks that db/migrations is a Go package, as the
// generated tests import it for gorbital.WithMigrations.
func checkMigrationsPackage(dir string) error {
	matches, _ := filepath.Glob(filepath.Join(dir, "db", "migrations", "*.go"))
	for _, m := range matches {
		if !strings.HasSuffix(m, "_test.go") {
			return nil
		}
	}
	return fmt.Errorf("%s/db/migrations isn't a Go package: add migrations.go embedding the files as FS (//go:embed *.sql), which gorbital.WithMigrations and the generated tests use; see examples/apps/shelfie/db/migrations/migrations.go", dir)
}

// mainUsesModuleList reports whether cmd/api passes modules.All to
// gorbital.Main.
func mainUsesModuleList(dir string) bool {
	matches, _ := filepath.Glob(filepath.Join(dir, "cmd", "api", "*.go"))
	for _, m := range matches {
		if src, err := os.ReadFile(m); err == nil && bytes.Contains(src, []byte("modules.All()")) {
			return true
		}
	}
	return false
}

func moduleSummary(d recipes.ModuleData, plan genplan.Plan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "  Module:      %s (table %s, IDs like %s_…)\n", d.Package, d.Table, d.IDPrefix)
	fmt.Fprintf(&b, "  API:         %s, for the signed-in user's %s\n", d.RoutePath(), d.PluralHuman)
	fmt.Fprintf(&b, "  Permissions: %s, %s (the user role)\n", d.PermRead(), d.PermWrite())
	b.WriteString("  Fields:\n")
	for _, f := range d.Fields {
		var kind string
		switch {
		case f.Kind == recipes.KindString && f.Optional:
			kind = fmt.Sprintf("string, up to %d characters, optional", f.MaxLength())
		case f.Kind == recipes.KindString:
			kind = fmt.Sprintf("string, 1 to %d characters", f.MaxLength())
			if f.Unique {
				kind += ", unique"
			}
		case f.Kind == recipes.KindText:
			kind = fmt.Sprintf("text, up to %d characters", f.MaxLength())
		default:
			kind = "one of " + strings.ReplaceAll(f.EnumTag(), ",", ", ") + " (default " + f.FirstValue().Value + ")"
		}
		fmt.Fprintf(&b, "    %-20s %s\n", f.Name, kind)
	}
	b.WriteString("  Files:\n")
	for line := range strings.Lines(genplan.Describe(plan)) {
		b.WriteString("  " + line)
	}
	return strings.TrimRight(b.String(), "\n")
}
