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
	"strings"
	"time"

	"gorbital.dev/cli/internal/recipes"
)

const addRLSUsage = `Usage: orb add rls [flags]

Turns on row-level security in a multi-tenant app (ADR-0061): a migration
forces it on every table with a NOT NULL org_id column (organisation
memberships and invitations aside), with a policy that limits each database
connection to the organisation of its request. The app already sets that
organisation on every connection, so no code changes. Tables you add later
get the policy from orb gen resource --scope org.

The app's database role must not be a superuser or have BYPASSRLS, or
PostgreSQL applies no policy to it; the app warns at startup and orb doctor
reports it.
`

type addRLSResult struct {
	Name string `json:"name"`
	// AlreadyOn reports an app whose gorbital.lock already records row-level
	// security; nothing changed.
	AlreadyOn bool `json:"already_on"`
	// Migration is the migration written, or that would be.
	Migration string   `json:"migration,omitempty"`
	Files     []string `json:"files"`
	DryRun    bool     `json:"dry_run"`
}

// runAddRLS adds the row-level security migration to a multi-tenant app and
// records rls: true in gorbital.yaml and gorbital.lock, so orb upgrade and
// orb gen resource know. It writes in the working tree, like orb add mail;
// the developer reviews and commits.
func runAddRLS(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("orb add rls", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dryRun := flags.Bool("dry-run", false, "show what would change without writing")
	asJSON := flags.Bool("json", false, "print the result as JSON")
	flags.Usage = func() {
		fmt.Fprint(stderr, addRLSUsage+"\nFlags:\n")
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
	name := filepath.Base(app.dir)
	lock, err := readLock(app.dir)
	switch {
	case errors.Is(err, errNoLock):
		return fmt.Errorf("%s has no %s: orb add rls works in apps created with orb new --preset full --tenancy multi", app.dir, lockPath)
	case err != nil:
		return err
	case lock.APIVersion != LockAPIVersion:
		return errors.New("gorbital.lock was written before v0.5; run orb upgrade --from <release that created the app> first")
	case lock.Inputs.Preset != "full" || lock.Inputs.Tenancy != recipes.TenancyMulti:
		return errors.New("row-level security separates organisations' data, and this app is single-tenant; add organisations first with orb add orgs")
	case lock.Inputs.RLS:
		res := addRLSResult{Name: name, AlreadyOn: true, Files: []string{}, DryRun: *dryRun}
		if *asJSON {
			return writeJSON(stdout, res)
		}
		fmt.Fprintf(stdout, "✓ %s already has row-level security. Nothing to change.\n", name)
		return nil
	case lock.Orb.Version != Version || (lock.Orb.Revision != "" && lock.Orb.Revision != buildRevision()):
		// Earlier releases don't set the organisation on connections, so the
		// policies would hide every organisation's rows from the app.
		return fmt.Errorf("%s was last written by orb %s; run orb upgrade first, so the app sets the organisation on its database connections", name, cmpOr(lock.Orb.Version, "(unknown)"))
	}

	root, err := os.OpenRoot(app.dir)
	if err != nil {
		return err
	}
	defer root.Close()
	policy, err := root.ReadFile(recipes.RowLevelSecurityPath)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%s has no %s; restore it from git history or run orb upgrade", name, recipes.RowLevelSecurityPath)
	} else if err != nil {
		return err
	}
	manifest, err := root.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", manifestPath, err)
	}
	version, err := nextMigrationVersion(app.dir, time.Now())
	if err != nil {
		return err
	}
	migration := "db/migrations/" + version + "_row_level_security.sql"
	manifest = recipes.SetManifestKey(manifest, recipes.RowLevelSecurityKey, "true")
	lock.Inputs.RLS = true
	lock.record(manifestPath, manifest)
	lockData, err := lock.encode()
	if err != nil {
		return err
	}
	res := addRLSResult{Name: name, Migration: migration, Files: []string{migration, manifestPath, lockPath}, DryRun: *dryRun}

	if !*dryRun {
		if !insideGitRepo(ctx, app.dir) {
			return errors.New("orb add rls needs the app in git, so the change can be reviewed: git init && git add -A && git commit -m 'Create app'")
		}
		if err := requireCleanGit(ctx, app.dir); err != nil {
			return err
		}
		for _, w := range []struct {
			path    string
			content []byte
		}{{migration, policy}, {manifestPath, manifest}, {lockPath, lockData}} {
			if err := root.WriteFile(w.path, w.content, 0o644); err != nil {
				return fmt.Errorf("write %s: %w", w.path, err)
			}
		}
	}

	if *asJSON {
		return writeJSON(stdout, res)
	}
	s := newStyles(stdout)
	verb := "Row-level security added to " + name
	if *dryRun {
		verb = "Would add row-level security to " + name + " (dry run)"
	}
	fmt.Fprintf(stdout, "%s\n\n", s.strong.Render(verb))
	for _, f := range res.Files {
		fmt.Fprintf(stdout, "  %s\n", f)
	}
	if *dryRun {
		return nil
	}
	fmt.Fprintf(stdout, `
Next:
  1. Make sure DATABASE_URL connects as a role that isn't a superuser and
     has no BYPASSRLS; PostgreSQL applies no policy to those. The local
     Docker database's user is a superuser, so policies don't apply there
     (docs/guides/row-level-security.md shows how to create an app role).
  2. go run ./cmd/migrate
  3. orb doctor   (reports a role that bypasses the policies)
  4. go test ./...   (the tests connect as a role without bypass)
  5. git add -A && git commit -m 'Add row-level security'

Organisation resources you generate from now on get the policy in their
migration. Code that must work across organisations, such as a maintenance
job, uses postgres.WithoutRowLevelSecurity with a reason; migrations already do.
`)
	return nil
}
