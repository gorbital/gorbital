package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"

	"gorbital.dev/cli/internal/genplan"
	"gorbital.dev/cli/internal/recipes"
)

const genResourceUsage = `Usage: orb gen resource <Name> <field:type>... [flags]

Generates a module for records that belong to the signed-in user, or in a
multi-tenant app to an organisation: domain rules, use cases, a repository
with hand-written SQL, HTTP endpoints under /v1/<names> (or
/v1/orgs/{orgId}/<names>), tests and a migration (ADR-0039, ADR-0048). The
code is yours to change. Run it inside an app created with the Full preset.

Field types:
  name:string                1 to 100 characters, required and sortable;
                             name:string:unique is unique per user, ignoring case
  notes:text                 up to 2000 characters, optional
  'status:enum(open,done)'   one of the values, the first by default; filterable

The first string field is the title. For example:
  orb gen resource Project name:string:unique description:text 'status:enum(active,archived)'
`

type genResourceResult struct {
	Name   string   `json:"name"`
	Module string   `json:"module"`
	Route  string   `json:"route"`
	Table  string   `json:"table"`
	Scope  string   `json:"scope"`
	Files  []string `json:"files"`
	DryRun bool     `json:"dry_run"`
	// RowLevelSecurity reports a migration with the row-level security
	// policy, in apps that ran orb add rls (ADR-0061).
	RowLevelSecurity bool `json:"row_level_security,omitempty"`
}

// resourceRoute is the collection path of a generated resource.
func resourceRoute(d recipes.ResourceData) string {
	if d.Org {
		return "/v1/orgs/{orgId}/" + d.Route
	}
	return "/v1/" + d.Route
}

// appTenancy returns the tenancy recorded in the app's gorbital.yaml, or
// single when it records none.
func appTenancy(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "gorbital.yaml"))
	if err != nil {
		return recipes.TenancySingle
	}
	for line := range strings.Lines(string(data)) {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "tenancy:"); ok && strings.TrimSpace(v) == recipes.TenancyMulti {
			return recipes.TenancyMulti
		}
	}
	return recipes.TenancySingle
}

// appRowLevelSecurity reports whether the app's gorbital.yaml records orb
// add rls (ADR-0061).
func appRowLevelSecurity(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "gorbital.yaml"))
	if err != nil {
		return false
	}
	for line := range strings.Lines(string(data)) {
		if v, ok := strings.CutPrefix(line, recipes.RowLevelSecurityKey+":"); ok && strings.TrimSpace(v) == "true" {
			return true
		}
	}
	return false
}

func runGenResource(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("orb gen resource", flag.ContinueOnError)
	flags.SetOutput(stderr)
	plural := flags.String("plural", "", "plural name when adding -s or -es is wrong, such as People")
	idPrefix := flags.String("id-prefix", "", "2 to 8 lowercase letters that start every ID (default: derived from the name, such as prj)")
	scope := flags.String("scope", "", "who the records belong to: user or org (default: org in multi-tenant apps, user otherwise)")
	dryRun := flags.Bool("dry-run", false, "show what would be generated without writing")
	asJSON := flags.Bool("json", false, "print the result as JSON")
	allowDirty := flags.Bool("allow-dirty", false, "allow uncommitted changes in the git repository")
	var p promptFlags
	flags.BoolVar(&p.yes, "yes", false, "use defaults for everything not given by flags, without prompts")
	flags.BoolVar(&p.noInput, "no-input", false, "never prompt; fail if a required value is missing")
	flags.BoolVar(&p.plain, "plain", false, "plain line-by-line prompts (screen-reader friendly)")
	flags.Usage = func() {
		fmt.Fprint(stderr, genResourceUsage+"\nFlags:\n")
		flags.PrintDefaults()
	}

	positional, err := parseInterspersed(flags, args)
	if err != nil {
		return err
	}
	if *scope != "" && *scope != recipes.ScopeUser && *scope != recipes.ScopeOrg {
		return usageError(fmt.Sprintf("unknown --scope %q (want user or org)", *scope))
	}
	var name string
	var specs []string
	if len(positional) > 0 {
		name, specs = positional[0], positional[1:]
	}

	app, err := findApp()
	if err != nil {
		return err
	}
	if appLayout(app.dir) == layoutMain {
		// In an app on gorbital.Main, orb gen resource is orb gen module
		// (ADR-0083): the same fields and flags, the new layout. As in v0.1,
		// records belong to organisations by default in a multi-tenant app.
		fmt.Fprintln(stderr, "orb: this app is on gorbital.Main, so orb gen resource runs orb gen module")
		org := *scope == recipes.ScopeOrg || (*scope == "" && appTenancy(app.dir) == recipes.TenancyMulti)
		in := moduleInput{name: name, specs: specs, plural: *plural, idPrefix: *idPrefix, org: org}
		return genModule(ctx, app, in, genModuleRun{dryRun: *dryRun, asJSON: *asJSON, allowDirty: *allowDirty, prompts: p}, stdin, stdout, stderr)
	}
	if _, err := checkResourceApp(app, *scope); err != nil {
		return err
	}
	ask := shouldPrompt(p, *asJSON, stdin, stdout)
	if ask {
		if err := promptResource(&name, &specs, p, stdin, stderr); err != nil {
			return err
		}
	}
	plan, data, err := planResource(app, resourceInput{name: name, specs: specs, plural: *plural, idPrefix: *idPrefix, scope: *scope}, time.Now())
	if err != nil {
		return err
	}
	result := plan.Result.(genResourceResult)
	result.DryRun = *dryRun

	if !*dryRun && ask {
		ok, err := confirm("Generate this resource?", plan.Summary, p, stdin, stderr)
		if err != nil {
			return err
		}
		if !ok {
			return errAborted
		}
	}

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
	fmt.Fprintf(stdout, "✓ %s resource %s\n\n%s\n", verb, result.Name, plan.Summary)
	if !*dryRun {
		fmt.Fprintf(stdout, "\nNext:\n%s\n"+
			"The code is yours: change the rules in internal/modules/%s/domain and the SQL in internal/modules/%s/repository.\n",
			numbered(plan.Next), result.Module, result.Module)
		if result.RowLevelSecurity {
			fmt.Fprintf(stdout, "The migration forces row-level security on %s with the organisation policy (orb add rls).\n", result.Table)
		}
		if data.Org {
			fmt.Fprintf(stdout, "Every organisation role gets %s.%s.read and .write; change that in declareOrgPermissions in internal/app/permissions.go.\n", data.Package, data.Snake)
		} else {
			fmt.Fprintf(stdout, "Every user holds %s.%s.read and .write through the user role in internal/app/permissions.go; an API key only when its scopes include them.\n", data.Package, data.Snake)
		}
	}
	return nil
}

// parseInterspersed parses flags given before, between or after the
// positional arguments, and returns the positional arguments in order.
func parseInterspersed(flags *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for len(args) > 0 {
		if args[0] == "-" || !strings.HasPrefix(args[0], "-") {
			positional = append(positional, args[0])
			args = args[1:]
			continue
		}
		if err := flags.Parse(args); err != nil {
			return nil, err
		}
		args = flags.Args()
	}
	return positional, nil
}

// promptResource asks for the name and fields when they weren't given.
func promptResource(name *string, specs *[]string, p promptFlags, stdin io.Reader, stderr io.Writer) error {
	var group []huh.Field
	if *name == "" {
		group = append(group, huh.NewInput().Title("Resource name").
			Description("Singular, such as Project, Customer or OrderItem.").
			Placeholder("Project").Value(name).Validate(recipes.ValidateResourceName))
	}
	line := strings.Join(*specs, " ")
	if len(*specs) == 0 {
		group = append(group, huh.NewInput().Title("Fields").
			Description("Separated by spaces: title:string (add :unique), notes:text, status:enum(open,done). The first string field is the title.").
			Placeholder("name:string:unique description:text status:enum(active,archived)").Value(&line).
			Validate(func(s string) error { _, err := recipes.ParseFields(strings.Fields(s)); return err }))
	}
	if len(group) == 0 {
		return nil
	}
	if err := runForm(huh.NewForm(huh.NewGroup(group...)), p, stdin, stderr); err != nil {
		return err
	}
	*specs = strings.Fields(line)
	return nil
}

// latestBuiltinMigration is the newest version of the migrations gorbital's
// built-in modules and frozen table serve (gorbital.Migrate), checked
// against the library by TestLatestBuiltinMigration.
const latestBuiltinMigration int64 = 20260918000070

// nextMigrationVersion returns now as a migration version, or one more than
// the newest migration's version when that isn't earlier, so the new
// migration always runs last.
func nextMigrationVersion(dir string, now time.Time) (string, error) {
	version := now.UTC().Format("20060102150405")
	// An app on gorbital.Main doesn't hold the built-in modules' migrations,
	// which run in the same history: a new migration must still come after
	// the newest of them, or goose refuses it on a database that ran them.
	if isGorbitalApp(dir) {
		if floor := strconv.FormatInt(latestBuiltinMigration+1, 10); version < floor {
			version = floor
		}
	}
	entries, err := os.ReadDir(filepath.Join(dir, "db", "migrations"))
	if errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("%s has no db/migrations: migrations and resources are generated in apps created with the Full preset", dir)
	} else if err != nil {
		return "", err
	}
	for _, e := range entries {
		prefix, _, ok := strings.Cut(e.Name(), "_")
		if !ok || len(prefix) != len(version) || prefix < version {
			continue
		}
		if n, err := strconv.ParseInt(prefix, 10, 64); err == nil {
			version = strconv.FormatInt(n+1, 10)
		}
	}
	return version, nil
}

func resourceSummary(d recipes.ResourceData, files []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "  Resource:  %s (table %s, IDs like %s_…)\n", d.Ident, d.Table, d.IDPrefix)
	if d.Org {
		fmt.Fprintf(&b, "  API:       %s, for an organisation's %s\n", resourceRoute(d), d.PluralHuman)
	} else {
		fmt.Fprintf(&b, "  API:       %s, for the signed-in user's %s\n", resourceRoute(d), d.PluralHuman)
	}
	b.WriteString("  Fields:\n")
	for _, f := range d.Fields {
		var kind string
		switch f.Kind {
		case recipes.KindString:
			kind = fmt.Sprintf("string, 1 to %d characters", f.MaxLength())
			if f.Unique {
				kind += ", unique"
			}
		case recipes.KindText:
			kind = fmt.Sprintf("text, up to %d characters", f.MaxLength())
		default:
			kind = "one of " + strings.ReplaceAll(f.EnumTag(), ",", ", ") + " (default " + f.FirstValue().Value + ")"
		}
		fmt.Fprintf(&b, "    %-20s %s\n", f.Name, kind)
	}
	b.WriteString("  Files:\n")
	for _, f := range files {
		fmt.Fprintf(&b, "    %s\n", f)
	}
	return strings.TrimRight(b.String(), "\n")
}
