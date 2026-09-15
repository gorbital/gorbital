// Package pgtest gives each test its own PostgreSQL database on the Docker
// PostgreSQL server started with `docker compose up -d --wait` (ADR-0028).
//
//	func TestInsertUser(t *testing.T) {
//		pool := pgtest.New(t, pgtest.WithMigrations(migrations.FS))
//		store := repository.NewUserStore(pool)
//		// ...
//	}
//
// The server URL comes from GORBITAL_TEST_DATABASE_URL. When it is unset,
// tests are skipped with instructions; set GORBITAL_REQUIRE_DB=1 (as CI does)
// to fail them instead.
//
// Migrations are applied once per distinct set of files into a template
// database, and each test's database is cloned from it, so tests stay fast
// and fully isolated. Old templates remain until `docker compose down -v`.
//
// Stability: pre-1.0 (ADR-0015).
package pgtest

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/modules/postgres"
)

// Environment variables read by this package.
const (
	EnvURL     = "GORBITAL_TEST_DATABASE_URL"
	EnvRequire = "GORBITAL_REQUIRE_DB"
)

const setupHint = "PostgreSQL for tests is not configured: run `docker compose up -d --wait` and set " +
	EnvURL + " (see compose.yaml)"

// URL returns the test server's connection URL. It skips the test when
// [EnvURL] is unset, or fails it when [EnvRequire] is "1".
func URL(t testing.TB) string {
	t.Helper()
	url := strings.TrimSpace(os.Getenv(EnvURL))
	if url == "" {
		if os.Getenv(EnvRequire) == "1" {
			t.Fatal(setupHint)
		}
		t.Skip(setupHint)
	}
	return url
}

type options struct {
	migrations fs.FS
	maxConns   int32
}

// An Option configures [New] and [NewDatabase].
type Option interface{ apply(*options) }

type optionFunc func(*options)

func (f optionFunc) apply(o *options) { f(o) }

// WithMigrations applies the goose migrations in fsys to the new database.
func WithMigrations(fsys fs.FS) Option {
	return optionFunc(func(o *options) { o.migrations = fsys })
}

// WithMaxConns sets the returned pool's size. Default: 4. [NewDatabase]
// ignores it.
func WithMaxConns(n int32) Option {
	return optionFunc(func(o *options) { o.maxConns = n })
}

// New creates a database for this test and returns a pool connected to it.
// When the test ends, the pool is closed and the database dropped.
func New(t testing.TB, opts ...Option) *pgxpool.Pool {
	t.Helper()
	o := newOptions(opts)
	cfg, name := createDatabase(t, o)

	dbCfg := cfg.Copy()
	dbCfg.ConnConfig.Database = name
	dbCfg.MaxConns = o.maxConns
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, dbCfg)
	if err != nil {
		t.Fatalf("pgtest: open pool: %v", err)
	}
	// Cleanups run in reverse order: the pool closes before the drop.
	t.Cleanup(pool.Close)
	return pool
}

// NewDatabase creates a database for this test and returns its connection
// URL, for tests that start a whole application from configuration. The
// database is dropped when the test ends; close every connection first.
// [EnvURL] must be in URL form (postgres://…).
func NewDatabase(t testing.TB, opts ...Option) string {
	t.Helper()
	u, err := url.Parse(URL(t))
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		t.Fatalf("pgtest: NewDatabase needs %s as a postgres:// URL", EnvURL)
	}
	_, name := createDatabase(t, newOptions(opts))
	u.Path = "/" + name
	u.RawPath = ""
	return u.String()
}

func newOptions(opts []Option) options {
	o := options{maxConns: 4}
	for _, opt := range opts {
		opt.apply(&o)
	}
	return o
}

// createDatabase creates an empty or migrated database and registers its
// drop. It returns the server's configuration and the database name.
func createDatabase(t testing.TB, o options) (*pgxpool.Config, string) {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(URL(t))
	if err != nil {
		t.Fatalf("pgtest: %s is not a valid PostgreSQL connection string", EnvURL)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	admin, err := pgx.ConnectConfig(ctx, cfg.ConnConfig.Copy())
	if err != nil {
		t.Fatalf("pgtest: connect: %v\n%s", err, setupHint)
	}
	defer func() { _ = admin.Close(context.Background()) }()

	name := "pgtest_" + randomHex()
	create := "CREATE DATABASE " + pgx.Identifier{name}.Sanitize()
	if o.migrations != nil {
		template, err := ensureTemplate(ctx, admin, cfg, o.migrations)
		if err != nil {
			t.Fatalf("pgtest: %v", err)
		}
		create += " TEMPLATE " + pgx.Identifier{template}.Sanitize()
	}
	if _, err := admin.Exec(ctx, create); err != nil {
		t.Fatalf("pgtest: create database: %v", err)
	}
	t.Cleanup(func() { dropDatabase(t, cfg, name) })
	return cfg, name
}

// ensureTemplate returns a template database with fsys's migrations
// applied, building it if needed. Test binaries for different packages run
// as parallel processes; an advisory lock lets one build while others wait.
func ensureTemplate(ctx context.Context, admin *pgx.Conn, cfg *pgxpool.Config, fsys fs.FS) (string, error) {
	sum, err := hashFS(fsys)
	if err != nil {
		return "", fmt.Errorf("read migrations: %w", err)
	}
	name := "pgtest_tpl_" + hex.EncodeToString(sum[:8])
	lockKey := int64(binary.BigEndian.Uint64(sum[:8])) //nolint:gosec // any 64 bits make a lock key

	if _, err := admin.Exec(ctx, "SELECT pg_advisory_lock($1)", lockKey); err != nil {
		return "", fmt.Errorf("lock template: %w", err)
	}
	defer func() { _, _ = admin.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", lockKey) }()

	var exists bool
	if err := admin.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", name).Scan(&exists); err != nil {
		return "", fmt.Errorf("find template: %w", err)
	}
	if exists {
		return name, nil
	}

	// Build under another name and rename, so a crash never leaves a
	// half-migrated template that later tests would trust.
	building := name + "_building"
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{building}.Sanitize()+" WITH (FORCE)"); err != nil {
		return "", fmt.Errorf("drop stale template: %w", err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{building}.Sanitize()); err != nil {
		return "", fmt.Errorf("create template: %w", err)
	}
	bCfg := cfg.Copy()
	bCfg.ConnConfig.Database = building
	bCfg.MaxConns = 2
	pool, err := pgxpool.NewWithConfig(ctx, bCfg)
	if err != nil {
		return "", fmt.Errorf("open template: %w", err)
	}
	_, err = postgres.Migrate(ctx, pool, fsys)
	pool.Close()
	if err != nil {
		return "", fmt.Errorf("migrate template: %w", err)
	}
	if _, err := admin.Exec(ctx, "ALTER DATABASE "+pgx.Identifier{building}.Sanitize()+" RENAME TO "+pgx.Identifier{name}.Sanitize()); err != nil {
		return "", fmt.Errorf("rename template: %w", err)
	}
	return name, nil
}

func dropDatabase(t testing.TB, cfg *pgxpool.Config, name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgx.ConnectConfig(ctx, cfg.ConnConfig.Copy())
	if err != nil {
		t.Errorf("pgtest: drop database %s: %v", name, err)
		return
	}
	defer func() { _ = admin.Close(context.Background()) }()
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
		t.Errorf("pgtest: drop database %s: %v", name, err)
	}
}

// hashFS hashes every file's path and contents in lexical order.
func hashFS(fsys fs.FS) ([]byte, error) {
	h := sha256.New()
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		h.Write([]byte(path))
		h.Write([]byte{0})
		h.Write(data)
		h.Write([]byte{0})
		return nil
	})
	return h.Sum(nil), err
}

func randomHex() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b) // never fails (crypto/rand)
	return hex.EncodeToString(b)
}
