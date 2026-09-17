package authhttp

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
	"gorbital.dev/modules/jobs"
	"gorbital.dev/modules/postgres"
	"gorbital.dev/modules/postgres/pgtest"
)

// TestMigrateOnV01DatabaseIsNoOp: a database migrated by a v0.1 golden
// app's cmd/migrate needs nothing from gorbital.Migrate with sign-in from
// the library, whether the app still holds its copies of sign-in's
// migrations or deleted them; a new database gets the same tables.
func TestMigrateOnV01DatabaseIsNoOp(t *testing.T) {
	for _, app := range []string{"full-single", "full-multi"} {
		t.Run(app, func(t *testing.T) {
			ctx := context.Background()
			url := pgtest.NewDatabase(t)
			pool, err := postgres.Open(ctx, config.NewSecret(url))
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			appFS := os.DirFS(filepath.Join(repo, "examples", app, "db", "migrations"))
			if applied, err := postgres.Migrate(ctx, pool, appFS); err != nil || len(applied) == 0 {
				t.Fatalf("v0.1 migrate = %v, %v", applied, err)
			}
			if _, err := jobs.Migrate(ctx, pool); err != nil {
				t.Fatal(err)
			}

			cfg := gorbital.Config{Env: "development", DatabaseURL: config.NewSecret(url)}
			for name, fsys := range map[string]fs.FS{"with its copies": appFS, "without its copies of sign-in's": withoutSignInCopies(t, appFS)} {
				var out bytes.Buffer
				if err := gorbital.Migrate(ctx, cfg, &out, gorbital.WithAuth(New()), gorbital.WithMigrations(fsys)); err != nil || out.Len() != 0 {
					t.Errorf("Migrate() %s = %q, %v; want nothing applied", name, out.String(), err)
				}
			}
		})
	}
}

// withoutSignInCopies returns fsys's SQL files minus sign-in's.
func withoutSignInCopies(t *testing.T, fsys fs.FS) fs.FS {
	t.Helper()
	served := map[string]bool{}
	for _, m := range moduleMigrations() {
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
		t.Fatalf("removed %d files, want sign-in's %d", len(entries)-1-len(out), len(served))
	}
	return out
}
