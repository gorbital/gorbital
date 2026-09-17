package gorbital

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/modules/auditpg"
	"gorbital.dev/modules/flags"
	"gorbital.dev/modules/idempotency"
	"gorbital.dev/modules/jobs"
	"gorbital.dev/modules/mail/suppressionpg"
	"gorbital.dev/modules/observability"
	"gorbital.dev/modules/postgres"
	"gorbital.dev/modules/ratelimitpg"
	"gorbital.dev/modules/releases"
	"gorbital.dev/modules/settings"
)

// A Migration is one goose migration file a module contributes to the app's
// single migration history (ADR-0083). Library modules number their files
// locally, such as 00001_settings.sql; Version places the file in the app's
// history, and is the version a v0.1 app holds the same file under, so an
// upgraded database sees it as applied.
type Migration struct {
	// Version orders the migration in the app's history, such as
	// 20260914000001. Released versions never change.
	Version int64
	// Name describes the migration in lowercase snake_case, such as
	// "settings"; the merged file is named <Version>_<Name>.sql.
	Name string
	// FS holds the file, usually the module's embedded migrations.
	FS fs.FS
	// File is the file's path in FS, such as "00001_settings.sql".
	File string
}

// libraryMigrations maps the migrations of the library modules gorbital
// builds on to the versions v0.1 apps carry them under, in their
// db/migrations with identical content. It is frozen: add entries for new
// migrations with versions later than every released one, and never change
// an entry (ADR-0083).
var libraryMigrations = []struct {
	Migration
	module string
}{
	{Migration{20260914000001, "settings", settings.Migrations, "00001_settings.sql"}, "gorbital.dev/modules/settings"},
	{Migration{20260914000002, "jobs_definitions", jobs.Migrations, "00001_jobs_definitions.sql"}, "gorbital.dev/modules/jobs"},
	{Migration{20260914000003, "audit_events", auditpg.Migrations, "00001_audit_events.sql"}, "gorbital.dev/modules/auditpg"},
	{Migration{20260915000003, "release_instances", releases.Migrations, "00001_release_instances.sql"}, "gorbital.dev/modules/releases"},
	{Migration{20260917000002, "ratelimit_buckets", ratelimitpg.Migrations, "00001_ratelimit_buckets.sql"}, "gorbital.dev/modules/ratelimitpg"},
	{Migration{20260918000001, "settings_org_values", settings.Migrations, "00002_settings_org_values.sql"}, "gorbital.dev/modules/settings"},
	{Migration{20260918000010, "flags", flags.Migrations, "00001_flags.sql"}, "gorbital.dev/modules/flags"},
	{Migration{20260918000040, "idempotency_keys", idempotency.Migrations, "00001_idempotency_keys.sql"}, "gorbital.dev/modules/idempotency"},
	{Migration{20260918000050, "mail_suppressions", suppressionpg.Migrations, "00001_mail_suppressions.sql"}, "gorbital.dev/modules/mail/suppressionpg"},
	{Migration{20260918000060, "observability_minutes", observability.Migrations, "00001_observability_minutes.sql"}, "gorbital.dev/modules/observability"},
	{Migration{20260918000061, "incidents", observability.Migrations, "00002_incidents.sql"}, "gorbital.dev/modules/observability"},
}

// migrationSource is one migration file with where it came from, for error
// messages.
type migrationSource struct {
	version int64
	file    string // the merged file's name
	origin  string // such as "gorbital.dev/modules/settings 00001_settings.sql"
	content []byte
}

var migrationName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// mergeMigrations returns one file system with the library's migrations,
// the modules' and the app's (appFS, which may be nil), each once:
//
//   - a built-in migration (the library's or a module's) is named
//     <version>_<name>.sql;
//   - an app file with the same version and identical content is the same
//     migration, such as a v0.1 app's copy of a library file;
//   - the same version with different content, or two built-in migrations
//     with one version, is an error naming both.
func mergeMigrations(modules []Module, appFS fs.FS) (fs.FS, error) {
	byVersion := map[int64]migrationSource{}
	var errs []error
	addBuiltin := func(m Migration, owner string) {
		origin := owner + " " + m.File
		switch {
		case m.Version <= 0:
			errs = append(errs, fmt.Errorf("gorbital: migration %s: version %d must be positive", origin, m.Version))
			return
		case !migrationName.MatchString(m.Name):
			errs = append(errs, fmt.Errorf("gorbital: migration %s: name %q must be lowercase snake_case", origin, m.Name))
			return
		case m.FS == nil:
			errs = append(errs, fmt.Errorf("gorbital: migration %s: FS is nil", origin))
			return
		}
		content, err := fs.ReadFile(m.FS, m.File)
		if err != nil {
			errs = append(errs, fmt.Errorf("gorbital: migration %s: %w", origin, err))
			return
		}
		if prev, ok := byVersion[m.Version]; ok {
			errs = append(errs, fmt.Errorf("gorbital: migration version %d is used by %s and %s", m.Version, prev.origin, origin))
			return
		}
		byVersion[m.Version] = migrationSource{
			version: m.Version, file: strconv.FormatInt(m.Version, 10) + "_" + m.Name + ".sql", origin: origin, content: content,
		}
	}
	for _, l := range libraryMigrations {
		addBuiltin(l.Migration, l.module)
	}
	for _, mod := range modules {
		for _, m := range mod.Migrations {
			addBuiltin(m, "module "+strconv.Quote(mod.Name))
		}
	}

	if appFS != nil {
		entries, err := fs.ReadDir(appFS, ".")
		if err != nil {
			return nil, fmt.Errorf("gorbital: read the app's migrations: %w", err)
		}
		appVersions := map[int64]string{}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
				continue
			}
			version, err := parseMigrationFile(e.Name())
			if err != nil {
				errs = append(errs, fmt.Errorf("gorbital: db/migrations/%s: %w", e.Name(), err))
				continue
			}
			origin := "db/migrations/" + e.Name()
			if prev, ok := appVersions[version]; ok {
				errs = append(errs, fmt.Errorf("gorbital: migration version %d is used by %s and %s", version, prev, origin))
				continue
			}
			appVersions[version] = origin
			content, err := fs.ReadFile(appFS, e.Name())
			if err != nil {
				errs = append(errs, fmt.Errorf("gorbital: %s: %w", origin, err))
				continue
			}
			if prev, ok := byVersion[version]; ok {
				if !bytes.Equal(prev.content, content) {
					errs = append(errs, fmt.Errorf("gorbital: migration version %d differs between %s and %s: a released migration never changes; give yours a new version", version, prev.origin, origin))
				}
				continue // identical: the app's copy of a built-in migration
			}
			byVersion[version] = migrationSource{version: version, file: e.Name(), origin: origin, content: content}
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	files := make(map[string][]byte, len(byVersion))
	for _, s := range byVersion {
		files[s.file] = s.content
	}
	return newMemFS(files), nil
}

// parseMigrationFile returns the version of a goose SQL migration file name,
// such as 20260915000002_books.sql: a positive decimal number, an
// underscore, a name and .sql, as goose itself reads it.
func parseMigrationFile(name string) (int64, error) {
	base, ok := strings.CutSuffix(name, ".sql")
	if !ok || strings.ContainsAny(base, "/\\") {
		return 0, errors.New("a migration file is named <version>_<name>.sql")
	}
	digits, rest, ok := strings.Cut(base, "_")
	if !ok || rest == "" || digits == "" {
		return 0, errors.New("a migration file is named <version>_<name>.sql, such as 20260915000002_books.sql")
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("version %q must be digits", digits)
		}
	}
	version, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || version <= 0 {
		return 0, fmt.Errorf("version %q must be a positive 64-bit number", digits)
	}
	return version, nil
}

// migrationSet returns the merged migrations of o.
func (o options) migrationSet() (fs.FS, error) {
	return mergeMigrations(o.allModules(), o.migrations)
}

// Migrate applies every pending migration: the merged goose history of the
// library, the modules and the app's own ([WithMigrations]) with the goose
// version table v0.1 apps use, then River's job tables. It reports each
// applied version to w. A database migrated by a v0.1 app has every library
// migration already, under the same versions, so nothing is applied twice.
//
// Apps run it from the migrate command ([Main]) before starting a new
// version; New never migrates (ADR-0017). It returns an error for a missing
// DATABASE_URL, conflicting migrations (naming both files), or a migration
// that fails, after reporting the ones applied before it.
func Migrate(ctx context.Context, cfg Config, w io.Writer, opts ...Option) error {
	o := newOptions(opts)
	fsys, err := o.migrationSet()
	if err != nil {
		return err
	}
	pool, err := openMigrationPool(ctx, cfg, o.name)
	if err != nil {
		return err
	}
	defer pool.Close()

	applied, err := postgres.Migrate(ctx, pool, fsys)
	for _, v := range applied {
		fmt.Fprintf(w, "applied migration %d\n", v)
	}
	if err != nil {
		return err
	}
	riverApplied, err := jobs.Migrate(ctx, pool)
	for _, v := range riverApplied {
		fmt.Fprintf(w, "applied job queue migration %d\n", v)
	}
	return err
}

// migrateDown rolls back the most recent migration, for development only
// (ADR-0069). River's tables are never rolled back.
func migrateDown(ctx context.Context, cfg Config, w io.Writer, o options) error {
	if cfg.Production() {
		return fmt.Errorf("%w: migrate-down is for development only, and APP_ENV is production", errInvalidConfig)
	}
	fsys, err := o.migrationSet()
	if err != nil {
		return err
	}
	pool, err := openMigrationPool(ctx, cfg, o.name)
	if err != nil {
		return err
	}
	defer pool.Close()
	version, err := postgres.MigrateDown(ctx, pool, fsys)
	if err != nil {
		return err
	}
	if version == 0 {
		fmt.Fprintln(w, "no migration to roll back")
		return nil
	}
	fmt.Fprintf(w, "rolled back migration %d\n", version)
	return nil
}

func openMigrationPool(ctx context.Context, cfg Config, name string) (*pgxpool.Pool, error) {
	if cfg.DatabaseURL.IsZero() {
		return nil, fmt.Errorf("%w: DATABASE_URL is required", errInvalidConfig)
	}
	return postgres.Open(ctx, cfg.DatabaseURL, postgres.WithApplicationName(name+"-migrate"), postgres.WithConnectTimeout(10*time.Second))
}

// migrationStatus is what migrate --status reports, for people and for
// orb doctor, in the shape of a v0.1 app's cmd/migrate --status.
type migrationStatus struct {
	ConfigError   string `json:"config_error,omitempty"`
	DatabaseError string `json:"database_error,omitempty"`
	Current       int64  `json:"current"`
	Latest        int64  `json:"latest"`
	Pending       int    `json:"pending"`
}

// readMigrationStatus reads the database's migration state and changes
// nothing.
func readMigrationStatus(ctx context.Context, cfg Config, cfgErr error, o options) migrationStatus {
	switch {
	case cfgErr != nil:
		return migrationStatus{ConfigError: cfgErr.Error()}
	case cfg.DatabaseURL.IsZero():
		return migrationStatus{ConfigError: "DATABASE_URL is required"}
	}
	fsys, err := o.migrationSet()
	if err != nil {
		return migrationStatus{ConfigError: err.Error()}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	pool, err := postgres.Open(ctx, cfg.DatabaseURL, postgres.WithApplicationName(o.name+"-status"), postgres.WithConnectTimeout(3*time.Second))
	if err != nil {
		return migrationStatus{DatabaseError: "connect to DATABASE_URL: " + err.Error()}
	}
	defer pool.Close()
	state, err := postgres.Migrations(ctx, pool, fsys)
	if err != nil {
		return migrationStatus{DatabaseError: "read the migration state: " + err.Error()}
	}
	return migrationStatus{Current: state.Current, Latest: state.Latest, Pending: state.Pending}
}

// memFS is a read-only in-memory file system of files in its root, the
// merged migrations goose reads.
type memFS struct {
	files map[string][]byte
	names []string // sorted
}

func newMemFS(files map[string][]byte) *memFS {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	return &memFS{files: files, names: names}
}

func (m *memFS) Open(name string) (fs.File, error) {
	if name == "." {
		return &memDir{fsys: m}, nil
	}
	content, ok := m.files[name]
	if !ok || !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &memFile{info: memInfo{name: name, size: int64(len(content))}, r: bytes.NewReader(content)}, nil
}

func (m *memFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name != "." {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	entries := make([]fs.DirEntry, 0, len(m.names))
	for _, n := range m.names {
		entries = append(entries, fs.FileInfoToDirEntry(memInfo{name: n, size: int64(len(m.files[n]))}))
	}
	return entries, nil
}

func (m *memFS) ReadFile(name string) ([]byte, error) {
	content, ok := m.files[name]
	if !ok {
		return nil, &fs.PathError{Op: "read", Path: name, Err: fs.ErrNotExist}
	}
	return bytes.Clone(content), nil
}

type memFile struct {
	info memInfo
	r    *bytes.Reader
}

func (f *memFile) Stat() (fs.FileInfo, error) { return f.info, nil }
func (f *memFile) Read(p []byte) (int, error) { return f.r.Read(p) }
func (f *memFile) Close() error               { return nil }

type memDir struct {
	fsys *memFS
	read int
}

func (d *memDir) Stat() (fs.FileInfo, error) { return memInfo{name: ".", dir: true}, nil }
func (d *memDir) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: ".", Err: fs.ErrInvalid}
}
func (d *memDir) Close() error { return nil }

func (d *memDir) ReadDir(n int) ([]fs.DirEntry, error) {
	all, _ := d.fsys.ReadDir(".")
	rest := all[d.read:]
	if n <= 0 {
		d.read = len(all)
		return rest, nil
	}
	if len(rest) == 0 {
		return nil, io.EOF
	}
	rest = rest[:min(n, len(rest))]
	d.read += len(rest)
	return rest, nil
}

type memInfo struct {
	name string
	size int64
	dir  bool
}

func (i memInfo) Name() string { return i.name }
func (i memInfo) Size() int64  { return i.size }
func (i memInfo) Mode() fs.FileMode {
	if i.dir {
		return fs.ModeDir | 0o555
	}
	return 0o444
}
func (i memInfo) ModTime() time.Time { return time.Time{} }
func (i memInfo) IsDir() bool        { return i.dir }
func (i memInfo) Sys() any           { return nil }
