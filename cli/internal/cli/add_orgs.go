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

	"apistock.dev/cli/internal/merge"
	"apistock.dev/cli/internal/recipes"
)

// addOrgsBranch is the branch aps add orgs works on.
const addOrgsBranch = "aps-add-orgs"

// runAddOrgs turns a single-tenant Full app into a multi-tenant one: the
// same 3-way merge as aps upgrade, from the single-tenant tree to the
// multi-tenant tree at this release, plus new migrations that add the
// organisation tables and move existing data into personal workspaces
// (ADR-0048, ADR-0050).
func runAddOrgs(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("aps add orgs", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dryRun := flags.Bool("dry-run", false, "show what would change, without writing or creating a branch")
	asJSON := flags.Bool("json", false, "print the result as JSON")
	skipTidy := flags.Bool("skip-tidy", false, "don't run go mod tidy")
	skipBuild := flags.Bool("skip-build", false, "don't build, regenerate api/openapi.json or commit")
	flags.Usage = func() {
		fmt.Fprint(stderr, "Usage: aps add orgs [flags]\n\nTurns a single-tenant app multi-tenant on branch "+addOrgsBranch+": organisations with\nmembers, roles and invitations, a personal workspace for every account, and\nprojects moved into their owners' workspaces.\n\nFlags:\n")
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
		return fmt.Errorf("%s has no %s: aps add orgs works in apps created with aps new --preset full", app.dir, lockPath)
	case err != nil:
		return err
	case lock.APIVersion != LockAPIVersion:
		return errors.New("apistock.lock was written before v0.5; run aps upgrade --from <release that created the app> first")
	case lock.Inputs.Preset != "full":
		return errors.New("aps add orgs needs an app created with the Full preset: organisations need its database and authentication")
	case lock.Inputs.Tenancy == recipes.TenancyMulti:
		fmt.Fprintf(stdout, "✓ %s already has organisations. Nothing to change.\n", filepath.Base(app.dir))
		return nil
	case lock.Aps.Version != Version || (lock.Aps.Revision != "" && lock.Aps.Revision != buildRevision()):
		return fmt.Errorf("%s was last written by aps %s; run aps upgrade first, so organisations are added to this release's files", filepath.Base(app.dir), cmpOr(lock.Aps.Version, "(unknown)"))
	}
	if !insideGitRepo(ctx, app.dir) {
		return errors.New("aps add orgs works on a git branch; put the app in git first: git init && git add -A && git commit -m 'Create app'")
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
	theirs, err := recipes.Embedded().Tree(to.Preset, to.Tenancy, to.Mail, d)
	if err != nil {
		return err
	}
	next, err := lockFromTree(to, theirs).encode()
	if err != nil {
		return err
	}
	theirsGoMod := theirs["go.mod"]
	orgsSQL, ok := theirs[recipes.OrgsMigrationPath]
	if !ok {
		return fmt.Errorf("recipes: the multi-tenant tree has no %s", recipes.OrgsMigrationPath)
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
	changes, err := merge.Plan(ctx, merge.Input{Base: base, Theirs: theirs, Unproven: unproven, Ours: rootReader(root), Label: "apistock " + Version + " organisations"})
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
	changes = append(changes,
		merge.Change{Path: "db/migrations/" + first + "_orgs.sql", Action: merge.Create, Content: orgsSQL},
		merge.Change{Path: "db/migrations/" + strconv.FormatInt(n+1, 10) + "_orgs_convert.sql", Action: merge.Create, Content: recipes.OrgsConversion()},
	)
	slices.SortFunc(changes, func(a, b merge.Change) int { return strings.Compare(a.Path, b.Path) })

	name := filepath.Base(app.dir)
	res := upgradeResult{
		Name: name, From: Version, To: Version, DryRun: *dryRun, Unproven: len(unproven),
		Branch: addOrgsBranch, title: "add organisations to " + name, message: "Add organisations",
	}
	res.setChanges(changes)
	res.UserScoped, err = userScopedModules(root, base, theirs)
	if err != nil {
		return err
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
