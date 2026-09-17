package cli

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"gorbital.dev/cli/internal/pgmeta"
	"gorbital.dev/cli/internal/portal"
)

// The live schema status (ADR-0080): orb dev compares db/migrations with
// the database after every migrate run, whenever a migration file changes
// (even with --no-reload) and once at startup, and publishes the result to
// the portal as a schema event. A record of each applied file's content
// (.orb/portal/migrations.json) finds files edited after they were
// applied, which PostgreSQL never sees and goose never mentions.

// migrateArgs are cmd/migrate's flags per portal command.
var migrateArgs = map[devCommand][]string{commandDown: {"--down"}, commandRedo: {"--redo"}}

// listMigrations lists the files with their state in the database, through
// the portal's connection. Tests replace d.migrations.
func (d *devRunner) listMigrations(ctx context.Context) ([]pgmeta.Migration, error) {
	if d.migrations != nil {
		return d.migrations(ctx)
	}
	open := d.databaseConfig().Open
	if open == nil {
		return nil, errors.New("this app has no database")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	db, err := open(ctx)
	if err != nil {
		return nil, err
	}
	return db.Migrations(ctx, d.dir)
}

// runMigrate runs the app's migrate command for c (plain for startup,
// rebuilds and apply pending; --down or --redo for the portal's roll back
// and redo), records the content of the files that became applied, and
// publishes the schema status with source. The migrate error, if any, is
// kept as the status's problem until the next success.
func (d *devRunner) runMigrate(ctx context.Context, env []string, source string, c devCommand) error {
	before := map[string]bool{}
	if migrations, err := d.listMigrations(ctx); err == nil {
		for _, f := range portal.AppliedFiles(migrations) {
			before[f] = true
		}
	}
	err := d.migrateWith(ctx, env, migrateArgs[c]...)
	d.mu.Lock()
	d.lastMigrateError = ""
	if err != nil {
		d.lastMigrateError = err.Error()
	}
	d.mu.Unlock()

	migrations, listErr := d.listMigrations(ctx)
	if listErr != nil {
		d.hub.SetSchema(d.schemaUnavailable(source, listErr))
		return err
	}
	var applied []string
	if err == nil {
		for _, f := range portal.AppliedFiles(migrations) {
			if !before[f] {
				applied = append(applied, f)
			}
		}
		if c == commandRedo {
			// Redo applies the last migration again with its current
			// content: record that content, not the one it replaced.
			if files := portal.AppliedFiles(migrations); len(files) > 0 && len(applied) == 0 {
				applied = files[len(files)-1:]
			}
		}
		record, recErr := portal.LoadMigrationRecord(d.dir)
		if recErr != nil {
			fmt.Fprintf(d.out, "orb: couldn't read the migration record: %v\n", recErr)
		}
		if saveErr := record.Update(migrations, applied).Save(d.dir); saveErr != nil {
			fmt.Fprintf(d.out, "orb: couldn't write the migration record: %v\n", saveErr)
		}
	}
	d.publishStatus(d.schemaStatus(migrations, source, applied))
	return err
}

// schemaStatus builds the status from migrations, the record and what the
// app is doing.
func (d *devRunner) schemaStatus(migrations []pgmeta.Migration, source string, applied []string) portal.SchemaStatus {
	record, err := portal.LoadMigrationRecord(d.dir)
	if err != nil {
		fmt.Fprintf(d.out, "orb: couldn't read the migration record: %v\n", err)
	}
	d.mu.Lock()
	state, problem, migrateErr := d.state, d.problem, d.lastMigrateError
	d.mu.Unlock()
	return portal.ComputeSchemaStatus(portal.SchemaInput{
		Migrations: migrations, Record: record.Applied, Source: source, Applied: applied,
		AutoApply: d.reload && state == portal.StateRunning && problem == "",
		Problem:   migrateErr,
	})
}

// schemaUnavailable is the status when the database doesn't answer.
func (d *devRunner) schemaUnavailable(source string, err error) portal.SchemaStatus {
	st := portal.ComputeSchemaStatus(portal.SchemaInput{Source: source})
	st.Problem = "the schema status can't be computed: " + err.Error()
	return st
}

// currentSchema computes the status now, for GET /_portal/api/db/
// schema-status, keeping the source and applied files of the last
// published status so a poll and the stream agree.
func (d *devRunner) currentSchema(ctx context.Context) (portal.SchemaStatus, error) {
	migrations, err := d.listMigrations(ctx)
	if err != nil {
		return portal.SchemaStatus{}, err
	}
	source, applied := portal.SchemaSourceStartup, []string(nil)
	if last, ok := d.hub.Schema(); ok {
		source, applied = last.Source, last.Applied
	}
	return d.schemaStatus(migrations, source, applied), nil
}

// schemaChangedInCode publishes the status after a change to db/migrations
// that orb dev didn't apply, with the warnings it deserves.
func (d *devRunner) schemaChangedInCode(ctx context.Context) {
	migrations, err := d.listMigrations(ctx)
	if err != nil {
		fmt.Fprintf(d.out, "orb: db/migrations changed, but the database doesn't answer: %v\n", err)
		d.hub.SetSchema(d.schemaUnavailable(portal.SchemaSourceCode, err))
		return
	}
	d.publishStatus(d.schemaStatus(migrations, portal.SchemaSourceCode, nil))
}

// publishStatus prints the warnings st deserves and publishes it.
func (d *devRunner) publishStatus(st portal.SchemaStatus) {
	prev, _ := d.hub.Schema()
	d.warnSchema(st, prev)
	d.hub.SetSchema(st)
}

// warnSchema prints what the developer must know: pending files after a
// code change, with what applies them, and files edited after they were
// applied (every time after a code change, otherwise when newly found).
func (d *devRunner) warnSchema(st, prev portal.SchemaStatus) {
	if st.Source == portal.SchemaSourceCode && len(st.Pending) > 0 {
		files := make([]string, 0, len(st.Pending))
		for _, p := range st.Pending {
			files = append(files, p.File)
		}
		verb, it := "is", "it"
		if len(files) > 1 {
			verb, it = "are", "them"
		}
		list := strings.Join(files, ", ")
		switch {
		case st.Problem != "":
			fmt.Fprintf(d.out, "orb: db/migrations changed (%s %s not applied): the last migrate failed; fix the SQL and save again, or Dev Portal → Migrations → Apply pending\n", list, verb)
		case !d.reload:
			fmt.Fprintf(d.out, "orb: db/migrations changed (%s %s not applied); restart the app to apply %s (Dev Portal → Restart)\n", list, verb, it)
		default:
			fmt.Fprintf(d.out, "orb: db/migrations changed (%s %s not applied); the next successful rebuild applies %s (fix the errors and save again, or Dev Portal → Restart)\n", list, verb, it)
		}
	}
	known := map[string]bool{}
	for _, e := range prev.Edited {
		known[e.File] = true
	}
	for _, e := range st.Edited {
		if st.Source != portal.SchemaSourceCode && known[e.File] {
			continue
		}
		fmt.Fprintf(d.out, "orb: %s was edited after it was applied; PostgreSQL still has the old version: use Migrations → Redo (development only) or add a new migration\n", e.File)
	}
}

// migrationsChanged reports whether the migrations snapshot differs from
// the one last examined, and remembers the new one.
func migrationsChanged(seen *uint64) bool {
	sql, err := snapshot(filepath.Clean(migrationsDir), isSQL)
	if err != nil || sql == *seen {
		return false
	}
	*seen = sql
	return true
}
