package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"gorbital.dev/cli/internal/merge"
	"gorbital.dev/cli/internal/recipes"
)

// addOrgsBranch is the branch orb add orgs works on.
const addOrgsBranch = "orb-add-orgs"

// runAddOrgs turns a single-tenant Full app into a multi-tenant one: the
// same 3-way merge as orb upgrade, from the single-tenant tree to the
// multi-tenant tree at this release, plus new migrations that add the
// organisation tables and move existing data into personal workspaces
// (ADR-0048, ADR-0050).
func runAddOrgs(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("orb add orgs", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dryRun := flags.Bool("dry-run", false, "show what would change, without writing or creating a branch")
	asJSON := flags.Bool("json", false, "print the result as JSON")
	skipTidy := flags.Bool("skip-tidy", false, "don't run go mod tidy")
	skipBuild := flags.Bool("skip-build", false, "don't build, regenerate api/openapi.json, record api/surface.json or commit")
	flags.Usage = func() {
		fmt.Fprint(stderr, "Usage: orb add orgs [flags]\n\nTurns a single-tenant app multi-tenant on branch "+addOrgsBranch+": organisations with\nmembers, roles and invitations, a personal workspace for every account, and\nprojects moved into their owners' workspaces.\n\nFlags:\n")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return usageError(fmt.Sprintf("unexpected arguments: %s", strings.Join(flags.Args(), " ")))
	}

	app, err := findApp()
	if err != nil {
		return err
	}
	lock, err := readLock(app.dir)
	switch {
	case errors.Is(err, errNoLock):
		return fmt.Errorf("%s has no %s: orb add orgs works in apps created with orb new --preset full", app.dir, lockPath)
	case err != nil:
		return err
	case lock.APIVersion != LockAPIVersion:
		return errors.New("gorbital.lock was written by an early development build of orb; run orb upgrade --from <commit that created the app> first")
	case lock.Inputs.Preset != "full":
		return errors.New("orb add orgs needs an app created with the Full preset: organisations need its database and authentication")
	case lock.Inputs.Tenancy == recipes.TenancyMulti:
		fmt.Fprintf(stdout, "✓ %s already has organisations. Nothing to change.\n", filepath.Base(app.dir))
		return nil
	case lock.Orb.Version != Version || (lock.Orb.Revision != "" && lock.Orb.Revision != buildRevision()):
		return fmt.Errorf("%s was last written by orb %s; run orb upgrade first, so organisations are added to this release's files", filepath.Base(app.dir), cmpOr(lock.Orb.Version, "(unknown)"))
	}
	if !insideGitRepo(ctx, app.dir) {
		return errors.New("orb add orgs works on a git branch; put the app in git first: git init && git add -A && git commit -m 'Create app'")
	}
	if !*dryRun {
		if err := requireCleanGit(ctx, app.dir); err != nil {
			return err
		}
	}

	from := lock.Inputs
	to := from
	to.Tenancy = recipes.TenancyMulti
	d := recipes.Data{Name: from.Name, Module: from.Module, LibraryVersion: recipes.LibraryVersion}
	base, unproven, err := rebuildBase(recipes.Embedded(), lock, from, d, Version)
	if err != nil {
		return err
	}
	theirs, err := inputsTree(recipes.Embedded(), to, to.Mail, d)
	if err != nil {
		return err
	}
	nextLock := lockFromTree(to, theirs)
	nextLock.Ejected = lock.Ejected
	theirsGoMod := theirs["go.mod"]
	// In the v0.1 layout the organisations migrations are the app's own
	// files, copied under new versions. In the v0.2 layout they come from
	// the library's organisations module under their released versions
	// (ADR-0083), and only the conversion is the app's.
	v01 := from.layout() == recipes.LayoutV01
	var orgsSQL []byte
	var laterSQL [][]byte
	if v01 {
		var ok bool
		if orgsSQL, ok = theirs[recipes.OrgsMigrationPath]; !ok {
			return fmt.Errorf("recipes: the multi-tenant tree has no %s", recipes.OrgsMigrationPath)
		}
		for _, p := range recipes.OrgsLaterMigrationPaths {
			sql, ok := theirs[p]
			if !ok {
				return fmt.Errorf("recipes: the multi-tenant tree has no %s", p)
			}
			laterSQL = append(laterSQL, sql)
		}
	}
	for _, p := range slices.Concat(untrackedPaths, derivedPaths) {
		delete(base, p)
		delete(theirs, p)
	}
	// A new multi-tenant app creates its tables with full-multi's migrations;
	// an existing database already ran the single-tenant ones, so new
	// migrations add organisations and convert the data instead.
	for p := range theirs {
		if _, inBase := base[p]; !inBase && isMigrationPath(p) {
			delete(theirs, p)
		}
	}

	root, err := os.OpenRoot(app.dir)
	if err != nil {
		return err
	}
	defer root.Close()
	changes, err := merge.Plan(ctx, merge.Input{Base: base, Theirs: theirs, Unproven: unproven, Ours: rootReader(root), Label: "gorbital " + Version + " organisations"})
	if err != nil {
		return err
	}
	first, err := nextMigrationVersion(app.dir, time.Now())
	if err != nil {
		return err
	}
	n, err := strconv.ParseInt(first, 10, 64)
	if err != nil {
		return err
	}
	if v01 {
		changes = append(changes,
			merge.Change{Path: "db/migrations/" + first + "_orgs.sql", Action: merge.Create, Content: orgsSQL},
			merge.Change{Path: "db/migrations/" + strconv.FormatInt(n+1, 10) + "_orgs_convert.sql", Action: merge.Create, Content: recipes.OrgsConversion()},
		)
		for i, p := range recipes.OrgsLaterMigrationPaths {
			_, name, _ := strings.Cut(path.Base(p), "_")
			changes = append(changes, merge.Change{Path: "db/migrations/" + strconv.FormatInt(n+2+int64(i), 10) + "_" + name, Action: merge.Create, Content: laterSQL[i]})
		}
	} else {
		changes = append(changes, merge.Change{Path: "db/migrations/" + first + "_orgs_convert.sql", Action: merge.Create, Content: recipes.OrgsConversion()})
	}
	if !v01 {
		if changes, nextLock, err = ejectOrgsWithAuth(ctx, app, lock, changes, nextLock); err != nil {
			return err
		}
	}
	next, err := nextLock.encode()
	if err != nil {
		return err
	}
	slices.SortFunc(changes, func(a, b merge.Change) int { return strings.Compare(a.Path, b.Path) })

	name := filepath.Base(app.dir)
	res := upgradeResult{
		Name: name, From: Version, To: Version, DryRun: *dryRun, Unproven: len(unproven), Layout: from.layout(),
		Branch: addOrgsBranch, title: "add organisations to " + name, message: "Add organisations",
	}
	res.setChanges(changes)
	res.UserScoped, err = userScopedModules(root, base, theirs)
	if err != nil {
		return err
	}
	// Sign-in and organisations the app holds aren't user-owned records.
	res.UserScoped = slices.DeleteFunc(res.UserScoped, func(m string) bool { _, ejected := nextLock.ejected(m); return ejected })
	if !v01 {
		res.MigrationOrder = orgsMigrationOrderWarning()
	}
	if *dryRun {
		res.Branch = ""
		return reportUpgrade(stdout, *asJSON, res)
	}
	return applyMove(ctx, app.dir, root, &res, next, theirsGoMod, *skipTidy, *skipBuild, stdout, stderr, *asJSON)
}

func isMigrationPath(p string) bool {
	return path.Dir(p) == "db/migrations" && path.Ext(p) == ".sql"
}

// migrationVersionOf returns the version a migration file's name starts
// with, such as 20260916000001 for db/migrations/20260916000001_orgs.sql.
func migrationVersionOf(p string) string {
	version, _, _ := strings.Cut(path.Base(p), "_")
	return version
}

// orgsMigrationOrderWarning describes what an app on gorbital.Main has to
// do about the organisations module's migrations, which keep the versions
// v0.1 apps hold them under (ADR-0083) and are therefore older than the
// built-in migrations every v0.2 database already ran. goose refuses them,
// so an existing database fails at the next migrate; a new one is fine.
// orb add orgs itself only writes files, so this is a warning, not a
// refusal.
func orgsMigrationOrderWarning() *migrationOrderWarning {
	versions := []string{migrationVersionOf(recipes.OrgsMigrationPath)}
	for _, p := range recipes.OrgsLaterMigrationPaths {
		versions = append(versions, migrationVersionOf(p))
	}
	newest := strconv.FormatInt(latestBuiltinMigration, 10)
	return &migrationOrderWarning{
		Versions: versions,
		Newest:   newest,
		Summary: fmt.Sprintf("the organisation tables come from the library module under versions %s, older than %s, which every database of an app on gorbital.Main already has. "+
			"goose refuses a migration older than the database's version, so a database that has been migrated before refuses these two. A database created after this change is fine, and so are the tests.",
			strings.Join(versions, " and "), newest),
		Error: fmt.Sprintf("postgres: migrate: detected %d missing (out-of-order) migrations lower than database version (<the database's newest>): versions %s", len(versions), strings.Join(versions, ",")),
		Options: []migrationOrderOption{
			{
				Label:    "a development database: recreate it, then migrate from scratch",
				Commands: []string{"docker compose down -v && docker compose up -d --wait", "go run ./cmd/api migrate"},
			},
			{
				Label: "a database you have to keep: apply the two migrations by hand, then record them as applied in goose_db_version, so the next migrate sees nothing missing",
				Doc:   "how, and why it is safe for these two: docs/start/organisations.md#adding-organisations-to-a-database-that-already-exists",
			},
		},
	}
}

// userScopedModules lists the modules under internal/modules that neither
// template tree has: the ones the developer generated, which stay owned by
// users.
func userScopedModules(root *os.Root, trees ...map[string][]byte) ([]string, error) {
	f, err := root.Open("internal/modules")
	if err != nil {
		return nil, err
	}
	entries, err := f.ReadDir(-1)
	f.Close()
	if err != nil {
		return nil, err
	}
	var modules []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		prefix := "internal/modules/" + e.Name() + "/"
		inTree := slices.ContainsFunc(trees, func(tree map[string][]byte) bool {
			for p := range tree {
				if strings.HasPrefix(p, prefix) {
					return true
				}
			}
			return false
		})
		if !inTree {
			modules = append(modules, e.Name())
		}
	}
	return modules, nil
}
