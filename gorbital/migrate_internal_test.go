package gorbital

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"gorbital.dev/config"
	"gorbital.dev/modules/jobs"
	"gorbital.dev/modules/postgres"
	"gorbital.dev/modules/postgres/pgtest"
)

// goldenMigrations are the migrations of the v0.1 golden apps, which hold
// copies of the library's migrations under the versions the frozen table
// maps them to.
var goldenMigrations = []string{
	filepath.Join("..", "examples", "v0.1", "full-single", "db", "migrations"),
	filepath.Join("..", "examples", "v0.1", "full-multi", "db", "migrations"),
}

// TestLibraryMigrationsMatchV01Apps: every entry of the frozen version table
// is byte for byte the file a v0.1 app holds under that version.
func TestLibraryMigrationsMatchV01Apps(t *testing.T) {
	for _, dir := range goldenMigrations {
		for _, l := range libraryMigrations {
			name := strconv.FormatInt(l.Version, 10) + "_" + l.Name + ".sql"
			app, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Errorf("%s: %v", dir, err)
				continue
			}
			lib, err := fs.ReadFile(l.FS, l.File)
			if err != nil {
				t.Fatalf("%s %s: %v", l.module, l.File, err)
			}
			if !bytes.Equal(app, lib) {
				t.Errorf("%s/%s differs from %s %s: the table maps released files, which never change", dir, name, l.module, l.File)
			}
		}
	}
}

func TestMergeMigrations(t *testing.T) {
	settingsV1, err := fs.ReadFile(libraryMigrations[0].FS, libraryMigrations[0].File)
	if err != nil {
		t.Fatal(err)
	}
	books := fstest.MapFS{"00001_books.sql": {Data: []byte("-- +goose Up\nCREATE TABLE books (id text);\n")}}
	tests := []struct {
		name      string
		modules   []Module
		app       fs.FS
		wantFiles []string
		wantErr   []string
	}{
		{name: "library only", wantFiles: []string{"20260914000001_settings.sql", "20260918000061_incidents.sql"}},
		{name: "a v0.1 app's copies collapse", app: fstest.MapFS{
			"20260914000001_settings.sql":      {Data: settingsV1},
			"20260915000002_projects.sql":      {Data: []byte("-- +goose Up\n")},
			"migrations.go":                    {Data: []byte("package migrations")},
			"notes/20260915000009_ignored.sql": {Data: []byte("in a subdirectory")},
			"20260915000001_auth.sql.orig":     {Data: []byte("not a migration")},
		}, wantFiles: []string{"20260914000001_settings.sql", "20260915000002_projects.sql"}},
		{name: "an app file changed from the library's", app: fstest.MapFS{
			"20260914000001_settings.sql": {Data: []byte("-- +goose Up\nSELECT 1;\n")},
		}, wantErr: []string{"20260914000001", "gorbital.dev/modules/settings 00001_settings.sql", "db/migrations/20260914000001_settings.sql"}},
		{name: "a module migration", modules: []Module{{Name: "books", Migrations: []Migration{{Version: 20270101000001, Name: "books", FS: books, File: "00001_books.sql"}}}},
			wantFiles: []string{"20270101000001_books.sql"}},
		{name: "a module migration with a library version", modules: []Module{{Name: "books", Migrations: []Migration{{Version: 20260918000010, Name: "books", FS: books, File: "00001_books.sql"}}}},
			wantErr: []string{"20260918000010", "gorbital.dev/modules/flags 00001_flags.sql", `module "books" 00001_books.sql`}},
		{name: "a module migration an app holds identically", modules: []Module{{Name: "books", Migrations: []Migration{{Version: 20270101000001, Name: "books", FS: books, File: "00001_books.sql"}}}},
			app:       fstest.MapFS{"20270101000001_books.sql": {Data: books["00001_books.sql"].Data}},
			wantFiles: []string{"20270101000001_books.sql"}},
		{name: "invalid module migrations", modules: []Module{{Name: "books", Migrations: []Migration{
			{Version: 0, Name: "books", FS: books, File: "00001_books.sql"},
			{Version: 20270101000002, Name: "Books", FS: books, File: "00001_books.sql"},
			{Version: 20270101000003, Name: "books", File: "00001_books.sql"},
			{Version: 20270101000004, Name: "books", FS: books, File: "missing.sql"},
		}}}, wantErr: []string{"must be positive", "lowercase snake_case", "FS is nil", "missing.sql"}},
		{name: "app files with one version", app: fstest.MapFS{
			"20270101000001_a.sql": {Data: []byte("a")},
			"20270101000001_b.sql": {Data: []byte("b")},
			"books.sql":            {Data: []byte("c")},
		}, wantErr: []string{"db/migrations/20270101000001_a.sql and db/migrations/20270101000001_b.sql", "db/migrations/books.sql"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mergeMigrations(tt.modules, tt.app)
			if len(tt.wantErr) > 0 {
				for _, want := range tt.wantErr {
					if err == nil || !strings.Contains(err.Error(), want) {
						t.Errorf("mergeMigrations() error = %v, want one containing %q", err, want)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("mergeMigrations() error = %v", err)
			}
			for _, name := range tt.wantFiles {
				if _, err := fs.Stat(got, name); err != nil {
					t.Errorf("merged migrations lack %s: %v", name, err)
				}
			}
			entries, err := fs.ReadDir(got, ".")
			if err != nil {
				t.Fatal(err)
			}
			versions := map[string]bool{}
			for _, e := range entries {
				v, _, _ := strings.Cut(e.Name(), "_")
				if versions[v] {
					t.Errorf("version %s appears twice", v)
				}
				versions[v] = true
			}
			if len(entries) < len(libraryMigrations) {
				t.Errorf("merged %d migrations, want at least the library's %d", len(entries), len(libraryMigrations))
			}
		})
	}
}

func TestMemFS(t *testing.T) {
	m := newMemFS(map[string][]byte{"20260914000001_settings.sql": []byte("-- +goose Up\n"), "2_b.sql": []byte("b")})
	if err := fstest.TestFS(m, "20260914000001_settings.sql", "2_b.sql"); err != nil {
		t.Fatal(err)
	}
}

func FuzzParseMigrationFile(f *testing.F) {
	for _, seed := range []string{"20260915000002_books.sql", "00001_settings.sql", "1_a.sql", "0_zero.sql", "books.sql", "_x.sql", "12_.sql",
		"99999999999999999999_big.sql", "1a_x.sql", "+1_x.sql", "1_x.SQL", "a/1_x.sql", "１_x.sql"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, name string) {
		version, err := parseMigrationFile(name)
		if err != nil {
			if version != 0 {
				t.Fatalf("parseMigrationFile(%q) = %d with error %v", name, version, err)
			}
			return
		}
		digits, rest, _ := strings.Cut(strings.TrimSuffix(name, ".sql"), "_")
		if version <= 0 || rest == "" || !strings.HasSuffix(name, ".sql") || strings.ContainsAny(name, `/\`) {
			t.Fatalf("parseMigrationFile(%q) = %d, accepted an invalid name", name, version)
		}
		if n, err := strconv.ParseInt(digits, 10, 64); err != nil || n != version {
			t.Fatalf("parseMigrationFile(%q) = %d, but its prefix is %q", name, version, digits)
		}
	})
}

// TestMigrateOnV01DatabaseIsNoOp: a database migrated by a v0.1 app's
// cmd/migrate (goose over its db/migrations, then River's tables) needs
// nothing from Migrate, whether the app still holds its copies of the
// library's migrations or not.
func TestMigrateOnV01DatabaseIsNoOp(t *testing.T) {
	for _, dir := range goldenMigrations {
		t.Run(filepath.Base(filepath.Dir(filepath.Dir(dir))), func(t *testing.T) {
			ctx := context.Background()
			url := pgtest.NewDatabase(t)
			pool, err := postgres.Open(ctx, config.NewSecret(url))
			if err != nil {
				t.Fatal(err)
			}
			appFS := os.DirFS(dir)
			applied, err := postgres.Migrate(ctx, pool, appFS)
			if err != nil || len(applied) == 0 {
				t.Fatalf("v0.1 migrate = %v, %v", applied, err)
			}
			if _, err := jobs.Migrate(ctx, pool); err != nil {
				t.Fatal(err)
			}
			pool.Close()

			cfg := Config{Env: "development", DatabaseURL: config.NewSecret(url)}
			// The app keeps db/migrations as it is, with its copies.
			var out bytes.Buffer
			if err := Migrate(ctx, cfg, &out, WithMigrations(appFS)); err != nil || out.Len() != 0 {
				t.Errorf("Migrate() with the v0.1 app's migrations = %q, %v; want nothing applied", out.String(), err)
			}
			// The app deleted its copies of the library's migrations.
			out.Reset()
			if err := Migrate(ctx, cfg, &out, WithMigrations(withoutLibraryCopies(t, appFS))); err != nil || out.Len() != 0 {
				t.Errorf("Migrate() without the library copies = %q, %v; want nothing applied", out.String(), err)
			}
			s := readMigrationStatus(ctx, cfg, nil, newOptions([]Option{WithMigrations(appFS)}))
			if s.Pending != 0 || s.ConfigError != "" || s.DatabaseError != "" || s.Current == 0 {
				t.Errorf("migration status = %+v, want none pending", s)
			}
		})
	}
}

// withoutLibraryCopies returns fsys's SQL files minus those the frozen table
// serves.
func withoutLibraryCopies(t *testing.T, fsys fs.FS) fs.FS {
	t.Helper()
	served := map[string]bool{}
	for _, l := range libraryMigrations {
		served[strconv.FormatInt(l.Version, 10)+"_"+l.Name+".sql"] = true
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
	return out
}

func TestMigrateAppliesAndRollsBack(t *testing.T) {
	ctx := context.Background()
	cfg := Config{Env: "development", DatabaseURL: config.NewSecret(pgtest.NewDatabase(t))}
	books := fstest.MapFS{"20270101000001_books.sql": {Data: []byte("-- +goose Up\nCREATE TABLE books (id text);\n-- +goose Down\nDROP TABLE books;\n")}}
	var out bytes.Buffer
	if err := Migrate(ctx, cfg, &out, WithMigrations(books)); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"applied migration 20260914000001", "applied migration 20270101000001", "applied job queue migration"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("Migrate() output lacks %q:\n%s", want, out.String())
		}
	}
	out.Reset()
	if err := migrateDown(ctx, cfg, &out, newOptions([]Option{WithMigrations(books)})); err != nil || out.String() != "rolled back migration 20270101000001\n" {
		t.Errorf("migrateDown() = %q, %v", out.String(), err)
	}
	if err := migrateDown(ctx, Config{Env: "production", DatabaseURL: cfg.DatabaseURL}, &out, newOptions(nil)); !errors.Is(err, errInvalidConfig) || !strings.Contains(err.Error(), "development only") {
		t.Errorf("migrateDown() in production = %v, want a refusal", err)
	}
	if err := Migrate(ctx, Config{Env: "development"}, &out); !errors.Is(err, errInvalidConfig) || !strings.Contains(err.Error(), "DATABASE_URL is required") {
		t.Errorf("Migrate() without DATABASE_URL = %v", err)
	}
}
