package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorbital.dev/cli/internal/genplan"
	"gorbital.dev/cli/internal/recipes"
)

// The generators plan before they write (ADR-0021, ADR-0066): planJob,
// planResource and planMigration read the app and return every file change
// as a genplan.Plan, which orb gen prints as a dry run or applies, and the
// Dev Portal shows as a diff before applying. Both paths run the same code.

// planJob returns what orb gen job would write for in, or an error naming
// what is wrong with in or the app.
func planJob(app appInfo, in jobInput) (genplan.Plan, error) {
	data, err := jobData(app.module, in)
	if err != nil {
		return genplan.Plan{}, err
	}
	files, err := recipes.RenderJob(data)
	if err != nil {
		return genplan.Plan{}, err
	}
	jobsGo := filepath.Join("internal", "app", "jobs.go")
	src, err := os.ReadFile(filepath.Join(app.dir, jobsGo))
	if errors.Is(err, fs.ErrNotExist) {
		return genplan.Plan{}, fmt.Errorf("%s has no %s: orb gen job works in apps created with the Full preset", app.dir, jobsGo)
	} else if err != nil {
		return genplan.Plan{}, err
	}
	callLine := "define" + data.Ident + "Job(defs, deps)"
	updated, err := recipes.InsertAfterAnchor(src, recipes.JobAnchor, callLine)
	if errors.Is(err, recipes.ErrAnchorMissing) {
		return genplan.Plan{}, fmt.Errorf("%s has no %q line; add it inside defineJobs, then run orb gen job again", jobsGo, recipes.JobAnchor)
	} else if err != nil {
		return genplan.Plan{}, fmt.Errorf("%s: job %s is already registered: %w", jobsGo, data.Name, err)
	}

	root, err := os.OpenRoot(app.dir)
	if err != nil {
		return genplan.Plan{}, err
	}
	defer root.Close()
	plan := genplan.Plan{Generator: "job", Name: data.Ident}
	for _, f := range files {
		if _, err := root.Stat(f.Path); err == nil {
			return genplan.Plan{}, fmt.Errorf("%s already exists; choose another job name", f.Path)
		}
		plan.Changes = append(plan.Changes, genplan.Change{Path: f.Path, Kind: genplan.Create, Content: f.Content})
	}
	plan.Changes = append(plan.Changes, genplan.Change{Path: filepath.ToSlash(jobsGo), Kind: genplan.Modify, Before: src, Content: updated})
	plan.Summary = jobSummary(data, plan.Paths())
	plan.Next = []string{
		jobNextStep(data),
		"go test ./internal/app -run TestPublicSurface -update (records the job name)",
		"go test ./...",
		"go run ./cmd/api",
	}
	plan.Result = genJobResult{Name: data.Ident, Definition: data.Name, Files: plan.Paths()}
	return plan, nil
}

// planMigration returns what orb gen migration would write for name: one
// empty migration versioned after every existing one.
func planMigration(app appInfo, name string, now time.Time) (genplan.Plan, error) {
	words, err := migrationName(name)
	if err != nil {
		return genplan.Plan{}, usageError(err.Error())
	}
	version, err := nextMigrationVersion(app.dir, now)
	if err != nil {
		return genplan.Plan{}, err
	}
	file := "db/migrations/" + version + "_" + strings.Join(words, "_") + ".sql"
	root, err := os.OpenRoot(app.dir)
	if err != nil {
		return genplan.Plan{}, err
	}
	defer root.Close()
	if _, err := root.Stat(filepath.FromSlash(file)); err == nil {
		return genplan.Plan{}, fmt.Errorf("%s already exists", file)
	}
	plan := genplan.Plan{
		Generator: "migration",
		Name:      strings.Join(words, "_"),
		Changes:   []genplan.Change{{Path: file, Kind: genplan.Create, Content: migrationContent(words)}},
		Next: []string{
			"Write the SQL under -- +goose Up",
			"go run ./cmd/migrate",
			"go test ./...",
		},
		Result: genMigrationResult{Name: strings.Join(words, "_"), Version: version, File: file},
	}
	plan.Summary = "  Migration: " + file + "\n"
	return plan, nil
}

// resourceInput holds orb gen resource's answers.
type resourceInput struct {
	name     string
	specs    []string // field specs, such as name:string:unique
	plural   string
	idPrefix string
	scope    string // user, org, or "" for the app's default
}

// checkResourceApp checks that the app can take a resource and returns the
// scope its records belong to: scope, or the app's default (organisations in
// multi-tenant apps, users otherwise). The command runs it before asking
// questions, so an app problem is reported first.
func checkResourceApp(app appInfo, scope string) (string, error) {
	if scope != "" && scope != recipes.ScopeUser && scope != recipes.ScopeOrg {
		return "", usageError(fmt.Sprintf("unknown --scope %q (want user or org)", scope))
	}
	modulesGo := filepath.Join("internal", "app", "modules.go")
	if _, err := os.Stat(filepath.Join(app.dir, modulesGo)); errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("%s has no %s: orb gen resource works in apps created with the Full preset", app.dir, modulesGo)
	} else if err != nil {
		return "", err
	}
	if info, err := os.Stat(filepath.Join(app.dir, "internal", "modules", "auth")); err != nil || !info.IsDir() {
		return "", fmt.Errorf("%s has no internal/modules/auth: resources belong to signed-in users, so orb gen resource needs the Full preset's auth module", app.dir)
	}
	// Records belong to organisations in multi-tenant apps unless the scope says otherwise.
	_, orgsErr := os.Stat(filepath.Join(app.dir, "internal", "modules", "orgs"))
	if scope == "" {
		scope = recipes.ScopeUser
		if appTenancy(app.dir) == recipes.TenancyMulti {
			scope = recipes.ScopeOrg
		}
	}
	if scope == recipes.ScopeOrg && orgsErr != nil {
		return "", fmt.Errorf("%s has no internal/modules/orgs: --scope org needs organisations; add them with orb add orgs, or create the app with orb new --tenancy multi", app.dir)
	}
	return scope, nil
}

// planResource returns what orb gen resource would write for in: the
// module, its migration, and the lines registering it in modules.go and
// permissions.go, with the rendered data behind them.
func planResource(app appInfo, in resourceInput, now time.Time) (genplan.Plan, recipes.ResourceData, error) {
	scope, err := checkResourceApp(app, in.scope)
	if err != nil {
		return genplan.Plan{}, recipes.ResourceData{}, err
	}
	modulesGo := filepath.Join("internal", "app", "modules.go")
	src, err := os.ReadFile(filepath.Join(app.dir, modulesGo))
	if err != nil {
		return genplan.Plan{}, recipes.ResourceData{}, err
	}
	switch {
	case in.name == "":
		return genplan.Plan{}, recipes.ResourceData{}, usageError("missing resource name: orb gen resource <Name> <field:type>... (or run it in a terminal to be asked)")
	case len(in.specs) == 0:
		return genplan.Plan{}, recipes.ResourceData{}, usageError(fmt.Sprintf("missing fields: orb gen resource %s name:string ... (orb gen resource -h lists the field types)", in.name))
	}
	fields, err := recipes.ParseFields(in.specs)
	if err != nil {
		return genplan.Plan{}, recipes.ResourceData{}, usageError(err.Error())
	}
	version, err := nextMigrationVersion(app.dir, now)
	if err != nil {
		return genplan.Plan{}, recipes.ResourceData{}, err
	}
	data, err := recipes.NewResourceData(app.module, in.name, fields, recipes.ResourceOptions{Plural: in.plural, IDPrefix: in.idPrefix, Migration: version, Scope: scope, RLS: appRowLevelSecurity(app.dir)})
	if err != nil {
		return genplan.Plan{}, recipes.ResourceData{}, usageError(err.Error())
	}

	files, err := recipes.RenderResource(data)
	if err != nil {
		return genplan.Plan{}, recipes.ResourceData{}, err
	}
	updated, err := recipes.InsertAfterAnchor(src, recipes.ModulesAnchor, data.ModulesLine())
	switch {
	case errors.Is(err, recipes.ErrAnchorMissing):
		return genplan.Plan{}, recipes.ResourceData{}, fmt.Errorf("%s has no %q line; add it as the first line inside errors.Join in registerModules, then run orb gen resource again", modulesGo, recipes.ModulesAnchor)
	case errors.Is(err, recipes.ErrLinePresent):
		return genplan.Plan{}, recipes.ResourceData{}, fmt.Errorf("%s: the %s module is already registered", modulesGo, data.PluralHuman)
	case err != nil:
		return genplan.Plan{}, recipes.ResourceData{}, fmt.Errorf("%s: can't register the %s module after %q; registerModules must return errors.Join of the modules, as in examples/full-single: %w",
			modulesGo, data.PluralHuman, recipes.ModulesAnchor, err)
	}

	// The resource's permissions go to the organisation roles, or to the user
	// role every user holds, so API keys can be scoped to them (ADR-0058).
	permissionsGo := filepath.Join("internal", "app", "permissions.go")
	permissionsSrc, err := os.ReadFile(filepath.Join(app.dir, permissionsGo))
	if err != nil {
		return genplan.Plan{}, recipes.ResourceData{}, err
	}
	permissions, err := recipes.InsertAfterAnchor(permissionsSrc, data.PermissionsAnchor(), data.PermissionsLine())
	switch {
	case errors.Is(err, recipes.ErrAnchorMissing) && data.Org:
		return genplan.Plan{}, recipes.ResourceData{}, fmt.Errorf("%s has no %q line; add it as the first line inside orgResourcePermissions, as in examples/full-multi, then run orb gen resource again", permissionsGo, recipes.OrgPermissionsAnchor)
	case errors.Is(err, recipes.ErrAnchorMissing):
		return genplan.Plan{}, recipes.ResourceData{}, fmt.Errorf("%s has no %q line; add userResourcePermissions and the user role as in examples/full-single (see the upgrade notes for ADR-0058), then run orb gen resource again", permissionsGo, recipes.UserPermissionsAnchor)
	case errors.Is(err, recipes.ErrLinePresent):
		return genplan.Plan{}, recipes.ResourceData{}, fmt.Errorf("%s: the %s permissions are already declared", permissionsGo, data.PluralHuman)
	case err != nil:
		return genplan.Plan{}, recipes.ResourceData{}, fmt.Errorf("%s: can't declare the %s permissions after %q: %w", permissionsGo, data.PluralHuman, data.PermissionsAnchor(), err)
	}

	root, err := os.OpenRoot(app.dir)
	if err != nil {
		return genplan.Plan{}, recipes.ResourceData{}, err
	}
	defer root.Close()
	moduleDir := "internal/modules/" + data.Package
	if _, err := root.Stat(filepath.FromSlash(moduleDir)); err == nil {
		return genplan.Plan{}, recipes.ResourceData{}, fmt.Errorf("%s already exists; choose another name or --plural", moduleDir)
	}
	plan := genplan.Plan{Generator: "resource", Name: data.Ident}
	for _, f := range files {
		if _, err := root.Stat(filepath.FromSlash(f.Path)); err == nil {
			return genplan.Plan{}, recipes.ResourceData{}, fmt.Errorf("%s already exists; choose another name or --plural", f.Path)
		}
		plan.Changes = append(plan.Changes, genplan.Change{Path: f.Path, Kind: genplan.Create, Content: f.Content})
	}
	plan.Changes = append(plan.Changes,
		genplan.Change{Path: filepath.ToSlash(modulesGo), Kind: genplan.Modify, Before: src, Content: updated},
		genplan.Change{Path: filepath.ToSlash(permissionsGo), Kind: genplan.Modify, Before: permissionsSrc, Content: permissions},
	)
	plan.Summary = resourceSummary(data, plan.Paths())
	plan.Next = []string{
		"go run ./cmd/migrate",
		"go run ./cmd/api openapi --dir api",
		"go test ./internal/app -run TestPublicSurface -update (records the new error codes, audit actions and permissions)",
		"go test ./...",
		"go run ./cmd/api, sign in, then POST " + resourceRoute(data),
	}
	plan.Result = genResourceResult{
		Name: data.Ident, Module: data.Package, Route: resourceRoute(data), Table: data.Table, Scope: scope,
		Files: plan.Paths(), RowLevelSecurity: data.RLS,
	}
	return plan, data, nil
}

// findAppIn returns the app whose go.mod is at dir, for callers that know
// the directory instead of running in it.
func findAppIn(dir string) (appInfo, error) {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return appInfo{}, fmt.Errorf("%s has no go.mod: %w", dir, err)
	}
	module := modulePath(data)
	if module == "" {
		return appInfo{}, fmt.Errorf("%s/go.mod has no module line", dir)
	}
	return appInfo{dir: dir, module: module}, nil
}

// jobNextStep is the first thing to do after generating a job: for a
// custom job, write it; for the other kinds, review what was written.
func jobNextStep(data recipes.JobData) string {
	if data.Kind == "" || data.Kind == recipes.KindCustom {
		return fmt.Sprintf("Write the job in internal/jobs/%s/%s.go (Work)", data.Package, data.Package)
	}
	return fmt.Sprintf("Review the %s job in internal/jobs/%s/%s.go; editing it makes it a custom job", data.Kind, data.Package, data.Package)
}
