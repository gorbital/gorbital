package orgshttp

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"gorbital.dev/config"
	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/authhttp"
	"gorbital.dev/modules/jobs"
	"gorbital.dev/modules/postgres"
	"gorbital.dev/modules/postgres/pgtest"
)

// TestModuleMigrationsMatchV01App checks that the module's migrations are
// full-multi's copies, byte for byte, under the same versions.
func TestModuleMigrationsMatchV01App(t *testing.T) {
	for _, m := range moduleMigrations() {
		got, err := fs.ReadFile(m.FS, m.File)
		if err != nil {
			t.Fatal(err)
		}
		name := strconv.FormatInt(m.Version, 10) + "_" + m.Name + ".sql"
		want, err := os.ReadFile(filepath.Join(repo, "examples", "full-multi", "db", "migrations", name))
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s differs from full-multi's %s (%v)", m.File, name, err)
		}
	}
}

// TestMigrateOnV01DatabaseIsNoOp: a database migrated by full-multi's
// v0.1 cmd/migrate needs nothing from gorbital.Migrate with sign-in and
// organisations from the library, whether the app still holds its copies of
// their migrations or deleted them.
func TestMigrateOnV01DatabaseIsNoOp(t *testing.T) {
	ctx := context.Background()
	url := pgtest.NewDatabase(t)
	pool, err := postgres.Open(ctx, config.NewSecret(url))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	appFS := os.DirFS(filepath.Join(repo, "examples", "full-multi", "db", "migrations"))
	if applied, err := postgres.Migrate(ctx, pool, appFS); err != nil || len(applied) == 0 {
		t.Fatalf("v0.1 migrate = %v, %v", applied, err)
	}
	if _, err := jobs.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	auth := authhttp.New()
	cfg := gorbital.Config{Env: "development", DatabaseURL: config.NewSecret(url)}
	for name, fsys := range map[string]fs.FS{"with its copies": appFS, "without its copies of the built-in modules'": withoutBuiltInCopies(t, appFS, auth)} {
		var out bytes.Buffer
		err := gorbital.Migrate(ctx, cfg, &out, gorbital.WithAuth(auth), gorbital.WithModules(Module(auth)), gorbital.WithMigrations(fsys))
		if err != nil || out.Len() != 0 {
			t.Errorf("Migrate() %s = %q, %v; want nothing applied", name, out.String(), err)
		}
	}
}

// withoutBuiltInCopies returns fsys's SQL files minus those sign-in and
// organisations declare, and the example projects module's.
func withoutBuiltInCopies(t *testing.T, fsys fs.FS, auth *authhttp.Authenticator) fs.FS {
	t.Helper()
	served := map[string]bool{}
	for _, m := range append(auth.Module().Migrations, moduleMigrations()...) {
		served[strconv.FormatInt(m.Version, 10)+"_"+m.Name+".sql"] = true
	}
	out := fstest.MapFS{}
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".sql") || served[e.Name()] {
			continue
		}
		data, err := fs.ReadFile(fsys, e.Name())
		if err != nil {
			t.Fatal(err)
		}
		out[e.Name()] = &fstest.MapFile{Data: data}
	}
	if len(out) != len(entries)-len(served)-1 { // migrations.go
		t.Fatalf("removed %d files, want the built-in modules' %d", len(entries)-1-len(out), len(served))
	}
	return out
}
