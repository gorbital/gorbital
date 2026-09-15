package pgtest_test

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres/pgtest"
)

func TestURLSkipsWhenNotConfigured(t *testing.T) {
	t.Setenv(pgtest.EnvURL, "")
	t.Setenv(pgtest.EnvRequire, "")

	var inner *testing.T
	t.Run("inner", func(t *testing.T) {
		inner = t
		pgtest.URL(t)
		t.Error("URL() returned without a configured database")
	})
	if !inner.Skipped() {
		t.Error("test was not skipped")
	}
}

func TestNewGivesIsolatedDatabases(t *testing.T) {
	ctx := context.Background()
	a, b := pgtest.New(t), pgtest.New(t)

	var nameA, nameB string
	if err := a.QueryRow(ctx, "SELECT current_database()").Scan(&nameA); err != nil {
		t.Fatal(err)
	}
	if err := b.QueryRow(ctx, "SELECT current_database()").Scan(&nameB); err != nil {
		t.Fatal(err)
	}
	if nameA == nameB {
		t.Fatalf("both pools use database %s", nameA)
	}

	if _, err := a.Exec(ctx, "CREATE TABLE only_in_a (id int)"); err != nil {
		t.Fatal(err)
	}
	var visible bool
	if err := b.QueryRow(ctx, "SELECT to_regclass('only_in_a') IS NOT NULL").Scan(&visible); err != nil {
		t.Fatal(err)
	}
	if visible {
		t.Error("table created in one test database is visible in another")
	}
}

func TestNewWithMigrationsClonesFreshDatabases(t *testing.T) {
	ctx := context.Background()
	fsys := fstest.MapFS{
		"00001_create_notes.sql": {Data: []byte("-- +goose Up\nCREATE TABLE notes (id int PRIMARY KEY);\n")},
	}
	for i := range 3 {
		pool := pgtest.New(t, pgtest.WithMigrations(fsys))
		// Each clone starts empty, so the same primary key inserts cleanly.
		if _, err := pool.Exec(ctx, "INSERT INTO notes VALUES (1)"); err != nil {
			t.Fatalf("database %d: insert into migrated table: %v", i, err)
		}
	}
}

func TestNewDatabaseReturnsAConnectableURL(t *testing.T) {
	ctx := context.Background()
	fsys := fstest.MapFS{
		"00001_create_items.sql": {Data: []byte("-- +goose Up\nCREATE TABLE items (id int PRIMARY KEY);\n")},
	}
	dbURL := pgtest.NewDatabase(t, pgtest.WithMigrations(fsys))
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect to NewDatabase URL: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	var name string
	if err := conn.QueryRow(ctx, "SELECT current_database()").Scan(&name); err != nil || !strings.HasPrefix(name, "pgtest_") {
		t.Errorf("current_database() = %q, %v; want a pgtest database", name, err)
	}
	if _, err := conn.Exec(ctx, "INSERT INTO items VALUES (1)"); err != nil {
		t.Errorf("migrated table missing: %v", err)
	}
}

func TestDatabaseIsDroppedWhenTestEnds(t *testing.T) {
	ctx := context.Background()
	var name string
	t.Run("inner", func(t *testing.T) {
		pool := pgtest.New(t)
		if err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&name); err != nil {
			t.Fatal(err)
		}
	})

	conn, err := pgx.Connect(ctx, pgtest.URL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	var exists bool
	if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", name).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Errorf("database %s still exists after its test ended", name)
	}
}
