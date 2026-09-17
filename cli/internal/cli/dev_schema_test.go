package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gorbital.dev/cli/internal/pgmeta"
	"gorbital.dev/cli/internal/portal"
)

// fakeMigrations lists db/migrations from disk, applied as the test says.
func fakeMigrations(dir string, applied map[string]bool) func(context.Context) ([]pgmeta.Migration, error) {
	return func(context.Context) ([]pgmeta.Migration, error) {
		entries, err := os.ReadDir(filepath.Join(dir, "db", "migrations"))
		if err != nil {
			return nil, err
		}
		var out []pgmeta.Migration
		for _, e := range entries {
			data, err := os.ReadFile(filepath.Join(dir, "db", "migrations", e.Name()))
			if err != nil {
				return nil, err
			}
			version, _ := strconv.ParseInt(strings.SplitN(e.Name(), "_", 2)[0], 10, 64)
			out = append(out, pgmeta.Migration{Version: version, Path: "db/migrations/" + e.Name(), SQL: string(data), Applied: applied[e.Name()]})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
		return out, nil
	}
}

// newSchemaRunner returns a runner over a stand-in app with two migration
// files, the first applied, whose migrate command applies the rest.
func newSchemaRunner(t *testing.T) (*devRunner, *bytes.Buffer, *fakeCommands, map[string]bool) {
	t.Helper()
	dir := newDevApp(t, fullManifest, newDevPorts(t).env())
	writeFile(t, filepath.Join("db", "migrations", "10_init.sql"), "-- +goose Up\nCREATE TABLE a ();\n")
	writeFile(t, filepath.Join("db", "migrations", "20_invoices.sql"), "-- +goose Up\nCREATE TABLE invoices ();\n")
	var out bytes.Buffer
	d := newDevRunner(&out)
	f := &fakeCommands{}
	f.install(d)
	applied := map[string]bool{"10_init.sql": true}
	d.database, d.reload = true, false
	d.migrations = fakeMigrations(dir, applied)
	run := d.run
	d.run = func(ctx context.Context, env []string, name string, args ...string) error {
		err := run(ctx, env, name, args...)
		if err == nil && name == "go" && slices.Contains(args, "./cmd/migrate") && !slices.Contains(args, "--down") {
			for _, e := range []string{"10_init.sql", "20_invoices.sql", "30_new.sql"} {
				if _, statErr := os.Stat(filepath.Join(dir, "db", "migrations", e)); statErr == nil {
					applied[e] = true
				}
			}
		}
		return err
	}
	return d, &out, f, applied
}

func TestRunMigrateRecordsAndPublishes(t *testing.T) {
	d, out, f, _ := newSchemaRunner(t)
	if err := d.runMigrate(context.Background(), nil, portal.SchemaSourceStartup, commandMigrate); err != nil {
		t.Fatalf("runMigrate() error = %v", err)
	}
	if want := []string{"go run ./cmd/migrate"}; !slices.Equal(f.calls, want) {
		t.Errorf("commands = %q, want %q", f.calls, want)
	}
	st, ok := d.hub.Schema()
	if !ok || st.Source != portal.SchemaSourceStartup || !slices.Equal(st.Applied, []string{"20_invoices.sql"}) || len(st.Pending) != 0 || st.NeedsRestart || st.Problem != "" {
		t.Errorf("startup status = %+v, %v", st, ok)
	}
	record, err := portal.LoadMigrationRecord(d.dir)
	if err != nil || len(record.Applied) != 2 || record.Applied["10_init.sql"] != portal.HashSQL(readFile(t, filepath.Join("db", "migrations", "10_init.sql"))) {
		t.Errorf("record = %+v, %v; want both applied files baselined", record, err)
	}
	if strings.Contains(out.String(), "was edited") || strings.Contains(out.String(), "db/migrations changed") {
		t.Errorf("unexpected warning:\n%s", out.String())
	}

	// On demand: the same source and applied files, computed again.
	now, err := d.currentSchema(context.Background())
	if err != nil || now.Source != portal.SchemaSourceStartup || !slices.Equal(now.Applied, []string{"20_invoices.sql"}) || now.CheckedAt.Before(st.CheckedAt) {
		t.Errorf("currentSchema() = %+v, %v", now, err)
	}
}

func TestSchemaChangedInCodeWarnsWithoutReload(t *testing.T) {
	d, out, _, _ := newSchemaRunner(t)
	if err := d.runMigrate(context.Background(), nil, portal.SchemaSourceStartup, commandMigrate); err != nil {
		t.Fatal(err)
	}
	d.setState(portal.StateRunning, "")
	out.Reset()

	// A new file with --no-reload: reported, not applied.
	writeFile(t, filepath.Join("db", "migrations", "30_new.sql"), "-- +goose Up\nCREATE TABLE new ();\n")
	d.schemaChangedInCode(context.Background())
	st, _ := d.hub.Schema()
	if st.Source != portal.SchemaSourceCode || !st.NeedsRestart || len(st.Pending) != 1 || st.Pending[0] != (portal.PendingMigration{File: "30_new.sql", Version: "30", Reason: portal.PendingNew}) {
		t.Errorf("status = %+v", st)
	}
	if want := "orb: db/migrations changed (30_new.sql is not applied); restart the app to apply it (Dev Portal → Restart)\n"; !strings.Contains(out.String(), want) {
		t.Errorf("output lacks %q:\n%s", want, out.String())
	}

	// An applied file edited afterwards: the trap, named every time.
	out.Reset()
	writeFile(t, filepath.Join("db", "migrations", "10_init.sql"), "-- +goose Up\nCREATE TABLE a (id int);\n")
	d.schemaChangedInCode(context.Background())
	st, _ = d.hub.Schema()
	if len(st.Edited) != 1 || st.Edited[0] != (portal.EditedMigration{File: "10_init.sql", Version: "10"}) {
		t.Errorf("edited = %+v", st.Edited)
	}
	if want := "orb: 10_init.sql was edited after it was applied; PostgreSQL still has the old version: use Migrations → Redo (development only) or add a new migration\n"; !strings.Contains(out.String(), want) {
		t.Errorf("output lacks %q:\n%s", want, out.String())
	}

	// With reload on and the app running, a pending file doesn't need a
	// restart; a stopped app does.
	d.reload = true
	d.schemaChangedInCode(context.Background())
	if st, _ := d.hub.Schema(); st.NeedsRestart {
		t.Errorf("needs_restart with reload and a running app: %+v", st)
	}
	d.setState(portal.StateStopped, "")
	d.schemaChangedInCode(context.Background())
	if st, _ := d.hub.Schema(); !st.NeedsRestart {
		t.Errorf("no needs_restart with the app stopped: %+v", st)
	}
}

func TestRunMigrateFailureKeepsPending(t *testing.T) {
	d, out, f, _ := newSchemaRunner(t)
	f.fail = map[string]bool{"go run ./cmd/migrate": true}
	d.reload = true
	d.setState(portal.StateRunning, "")
	err := d.runMigrate(context.Background(), nil, portal.SchemaSourceMigrate, commandMigrate)
	if err == nil || !strings.Contains(err.Error(), "migrations failed") {
		t.Fatalf("runMigrate() error = %v", err)
	}
	st, _ := d.hub.Schema()
	if st.Source != portal.SchemaSourceMigrate || !strings.Contains(st.Problem, "migrations failed") || !st.NeedsRestart || len(st.Pending) != 1 || len(st.Applied) != 0 {
		t.Errorf("after a failed migrate = %+v", st)
	}
	if _, err := os.Stat(filepath.Join(d.dir, ".orb", "portal", "migrations.json")); !os.IsNotExist(err) {
		t.Error("a failed migrate wrote the record")
	}
	// The code path names the failure instead of promising a restart.
	out.Reset()
	d.schemaChangedInCode(context.Background())
	if !strings.Contains(out.String(), "20_invoices.sql is not applied): the last migrate failed") {
		t.Errorf("output = %s", out.String())
	}

	// The next success clears the problem.
	f.fail = nil
	if err := d.runMigrate(context.Background(), nil, portal.SchemaSourcePortal, commandMigrate); err != nil {
		t.Fatal(err)
	}
	if st, _ := d.hub.Schema(); st.Source != portal.SchemaSourcePortal || st.Problem != "" || st.NeedsRestart || !slices.Equal(st.Applied, []string{"20_invoices.sql"}) {
		t.Errorf("after the retry = %+v", st)
	}
}

func TestRunMigrateRedoRefreshesTheRecord(t *testing.T) {
	d, out, f, _ := newSchemaRunner(t)
	if err := d.runMigrate(context.Background(), nil, portal.SchemaSourceStartup, commandMigrate); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join("db", "migrations", "20_invoices.sql"), "-- +goose Up\nCREATE TABLE invoices (id int);\n")
	d.schemaChangedInCode(context.Background())
	if st, _ := d.hub.Schema(); len(st.Edited) != 1 {
		t.Fatalf("edited = %+v", st.Edited)
	}
	out.Reset()
	if err := d.runMigrate(context.Background(), nil, portal.SchemaSourcePortal, commandRedo); err != nil {
		t.Fatal(err)
	}
	if want := "go run ./cmd/migrate --redo"; f.calls[len(f.calls)-1] != want {
		t.Errorf("last command = %q, want %q", f.calls[len(f.calls)-1], want)
	}
	st, _ := d.hub.Schema()
	if len(st.Edited) != 0 || !slices.Equal(st.Applied, []string{"20_invoices.sql"}) || st.Source != portal.SchemaSourcePortal {
		t.Errorf("after redo = %+v", st)
	}
	if strings.Contains(out.String(), "was edited") {
		t.Errorf("redo still warns:\n%s", out.String())
	}
}
