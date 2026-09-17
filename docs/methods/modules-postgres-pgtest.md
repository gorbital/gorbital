# modules/postgres/pgtest

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/modules/postgres/pgtest"
```

Package pgtest gives each test its own PostgreSQL database on the Docker PostgreSQL server started with \`docker compose up -d --wait\` (ADR-0028).

```go
func TestInsertUser(t *testing.T) {
	pool := pgtest.New(t, pgtest.WithMigrations(migrations.FS))
	store := repository.NewUserStore(pool)
	// ...
}
```

The server URL comes from GORBITAL\_TEST\_DATABASE\_URL. When it is unset, tests are skipped with instructions; set GORBITAL\_REQUIRE\_DB=1 (as CI does) to fail them instead.

Migrations are applied once per distinct set of files into a template database, and each test's database is cloned from it, so tests stay fast and fully isolated. Old templates remain until \`docker compose down -v\`.

Stability: stable, for tests only: the API follows the compatibility promise; what the helpers do inside a test may change (ADR-0015, ADR-0054).

## Contents

- Constants: [`EnvURL`](#EnvURL), [`EnvRequire`](#EnvRequire)
- Functions: [`New`](#New), [`NewDatabase`](#NewDatabase), [`URL`](#URL)
- Types:
  - [`Option`](#Option): [`WithMaxConns`](#WithMaxConns), [`WithMigrations`](#WithMigrations)

## Constants

<a id="EnvURL"></a>
<a id="EnvRequire"></a>

```go
const (
	EnvURL     = "GORBITAL_TEST_DATABASE_URL"
	EnvRequire = "GORBITAL_REQUIRE_DB"
)
```

Environment variables read by this package.

*Since `v0.1.0`*

## Functions

<a id="New"></a>

### func New

```go
func New(t testing.TB, opts ...Option) *pgxpool.Pool
```

New creates a database for this test and returns a pool connected to it. When the test ends, the pool is closed and the database dropped.

*Since `v0.1.0`*

<a id="NewDatabase"></a>

### func NewDatabase

```go
func NewDatabase(t testing.TB, opts ...Option) string
```

NewDatabase creates a database for this test and returns its connection URL, for tests that start a whole application from configuration. The database is dropped when the test ends; close every connection first. [EnvURL](#EnvURL) must be in URL form (postgres://…).

*Since `v0.1.0`*

<a id="URL"></a>

### func URL

```go
func URL(t testing.TB) string
```

URL returns the test server's connection URL. It skips the test when [EnvURL](#EnvURL) is unset, or fails it when [EnvRequire](#EnvRequire) is "1".

*Since `v0.1.0`*

## Types

<a id="Option"></a>

### type Option

```go
type Option interface {
	// contains filtered or unexported methods
}
```

An Option configures [New](#New) and [NewDatabase](#NewDatabase).

*Since `v0.1.0`*

<a id="WithMaxConns"></a>

#### func WithMaxConns

```go
func WithMaxConns(n int32) Option
```

WithMaxConns sets the returned pool's size. Default: 4. [NewDatabase](#NewDatabase) ignores it.

*Since `v0.1.0`*

<a id="WithMigrations"></a>

#### func WithMigrations

```go
func WithMigrations(fsys fs.FS) Option
```

WithMigrations applies the goose migrations in fsys to the new database.

*Since `v0.1.0`*
