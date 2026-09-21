# modules/postgres

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/modules/postgres"
```

Package postgres connects gorbital apps to PostgreSQL: a pgx connection pool with OpenTelemetry tracing, transactions, error classification for repositories, a readiness check, goose migrations (ADR-0005, ADR-0032) and the organisation row-level security policies read (ADR-0061).

Repositories hold a [DBTX](#DBTX), so the same store runs on the pool or inside a transaction started with [InTx](#InTx). Transactions are passed explicitly, never stored in a context (ADR-0030).

This package is the pgx adapter, so pgx types appear in its API and its errors wrap pgx errors. Repositories translate them into domain errors with [IsNoRows](#IsNoRows), [UniqueViolation](#UniqueViolation) and the other helpers before returning.

Stability: stable (ADR-0015, ADR-0054).

## Contents

- Constants: [`OrgSetting`](#OrgSetting), [`ScopeSetting`](#ScopeSetting), [`BypassSetting`](#BypassSetting), [`DefaultConnectTimeout`](#DefaultConnectTimeout)
- Functions: [`CheckViolation`](#CheckViolation), [`ForeignKeyViolation`](#ForeignKeyViolation), [`HealthCheck`](#HealthCheck), [`InTx`](#InTx), [`InTxWithOptions`](#InTxWithOptions), [`IsNoRows`](#IsNoRows), [`IsRetryable`](#IsRetryable), [`Migrate`](#Migrate), [`MigrateDown`](#MigrateDown), [`NotNullViolation`](#NotNullViolation), [`Open`](#Open), [`UniqueViolation`](#UniqueViolation), [`WithOrg`](#WithOrg), [`WithScope`](#WithScope), [`WithoutRowLevelSecurity`](#WithoutRowLevelSecurity)
- Types:
  - [`Beginner`](#Beginner)
  - [`DBTX`](#DBTX)
  - [`MigrationInfo`](#MigrationInfo): [`MigrationList`](#MigrationList)
  - [`MigrationState`](#MigrationState): [`Migrations`](#Migrations)
  - [`Option`](#Option): [`WithApplicationName`](#WithApplicationName), [`WithConnectTimeout`](#WithConnectTimeout), [`WithLogger`](#WithLogger), [`WithMaxConnIdleTime`](#WithMaxConnIdleTime), [`WithMaxConnLifetime`](#WithMaxConnLifetime), [`WithMaxConns`](#WithMaxConns), [`WithMeterProvider`](#WithMeterProvider), [`WithMinConns`](#WithMinConns), [`WithTracerProvider`](#WithTracerProvider)
  - [`RowLevelSecurityReport`](#RowLevelSecurityReport): [`CheckRowLevelSecurity`](#CheckRowLevelSecurity), [`RowLevelSecurityReport.On`](#RowLevelSecurityReport.On)

## Constants

<a id="OrgSetting"></a>
<a id="ScopeSetting"></a>
<a id="BypassSetting"></a>

```go
const (
	// OrgSetting holds the organisation ID of the acquiring context, or ""
	// when it has none.
	OrgSetting = "gorbital.org_id"
	// ScopeSetting is [OrgSetting] under the name the framework uses for
	// tenancy since v0.2.2 (ADR-0088). There is one session setting whatever
	// an app calls its scope: it is invisible to people, row-level-security
	// policies in live databases name it, and renaming it would change what
	// those policies mean.
	ScopeSetting = OrgSetting
	// BypassSetting is "on" for a context from [WithoutRowLevelSecurity],
	// otherwise "".
	BypassSetting = "gorbital.rls_bypass"
)
```

Row-level security (ADR-0061). Every connection a pool from [Open](#Open) hands out carries the organisation of the context that acquired it, in two session settings that policies read:

```
org_id = current_setting('gorbital.org_id', true)
    OR current_setting('gorbital.rls_bypass', true) = 'on'
```

*Since `v0.1.0`: OrgSetting, BypassSetting; `v0.2.0 (unreleased)`: ScopeSetting*

<a id="DefaultConnectTimeout"></a>

```go
const DefaultConnectTimeout = 5 * time.Second
```

DefaultConnectTimeout bounds each connection attempt and the initial ping.

*Since `v0.1.0`*

## Functions

<a id="CheckViolation"></a>

### func CheckViolation

```go
func CheckViolation(err error) (constraint string, ok bool)
```

CheckViolation reports whether err is a check constraint violation and returns the constraint's name.

*Since `v0.1.0`*

<a id="ForeignKeyViolation"></a>

### func ForeignKeyViolation

```go
func ForeignKeyViolation(err error) (constraint string, ok bool)
```

ForeignKeyViolation reports whether err is a foreign key violation and returns the constraint's name.

*Since `v0.1.0`*

<a id="HealthCheck"></a>

### func HealthCheck

```go
func HealthCheck(pool *pgxpool.Pool) health.Check
```

HealthCheck returns a readiness check that pings the database.

*Since `v0.1.0`*

<a id="InTx"></a>

### func InTx

```go
func InTx(ctx context.Context, db Beginner, fn func(tx pgx.Tx) error) error
```

InTx runs fn in a read-write transaction with the server's default isolation level. See [InTxWithOptions](#InTxWithOptions).

*Since `v0.1.0`*

**Example**

```go
ctx := context.Background()
pool, err := postgres.Open(ctx, config.NewSecret("postgres://localhost:5432/acme"))
if err != nil {
	log.Fatal(err)
}
defer pool.Close()

// Stores built from tx take part in the transaction.
err = postgres.InTx(ctx, pool, func(tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, "UPDATE accounts SET balance = balance - 10 WHERE id = $1", 1); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, "UPDATE accounts SET balance = balance + 10 WHERE id = $1", 2)
	return err
})
if err != nil {
	log.Fatal(err)
}
```

<a id="InTxWithOptions"></a>

### func InTxWithOptions

```go
func InTxWithOptions(ctx context.Context, db Beginner, opts pgx.TxOptions, fn func(tx pgx.Tx) error) error
```

InTxWithOptions runs fn in a transaction started with opts. It commits when fn returns nil. Otherwise it rolls back and returns fn's error unchanged, so callers can match domain errors with [errors.Is](https://pkg.go.dev/errors#Is). If fn panics, the transaction is rolled back and the panic continues.

Build tx-bound repositories inside fn (for example NewUserStore(tx)); never keep tx after fn returns. With [pgx.Serializable](https://pkg.go.dev/github.com/jackc/pgx/v5#Serializable), retry when [IsRetryable](#IsRetryable) reports true.

*Since `v0.1.0`*

<a id="IsNoRows"></a>

### func IsNoRows

```go
func IsNoRows(err error) bool
```

IsNoRows reports whether err means a query returned no rows, as from QueryRow(...).Scan. Repositories return their not-found domain error.

*Since `v0.1.0`*

<a id="IsRetryable"></a>

### func IsRetryable

```go
func IsRetryable(err error) bool
```

IsRetryable reports whether err is a serialization failure or deadlock, after which the whole transaction can be retried.

*Since `v0.1.0`*

<a id="Migrate"></a>

### func Migrate

```go
func Migrate(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS) ([]int64, error)
```

Migrate applies every pending goose migration in the root of fsys (files such as 00001\_create\_users.sql) and returns the versions it applied, in order. It returns nil when fsys has no migrations or none are pending.

A PostgreSQL advisory lock serialises concurrent callers, so several instances can run Migrate at once. Migrations run [WithoutRowLevelSecurity](#WithoutRowLevelSecurity), so a data migration reaches every organisation's rows (ADR-0061). Apps run migrations from a separate command, never implicitly at startup (ADR-0017).

*Since `v0.1.0`*

<a id="MigrateDown"></a>

### func MigrateDown

```go
func MigrateDown(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS) (int64, error)
```

MigrateDown rolls back the most recently applied migration in fsys, for development (ADR-0069): a migration written from the Dev Portal carries a Down section, and undoing it is how a mistake is corrected before it is released. It returns the version rolled back, or 0 when none was applied. Production apps refuse to call it.

*Since `v0.1.0`*

<a id="NotNullViolation"></a>

### func NotNullViolation

```go
func NotNullViolation(err error) (column string, ok bool)
```

NotNullViolation reports whether err is a not-null violation and returns the column's name.

*Since `v0.1.0`*

<a id="Open"></a>

### func Open

```go
func Open(ctx context.Context, url config.Secret, opts ...Option) (*pgxpool.Pool, error)
```

Open creates a connection pool for url and pings the database, so a wrong URL or unreachable server fails at startup. Errors never include the URL, which may contain a password.

Every query becomes an OpenTelemetry client span carrying the SQL text but never its arguments.

Every connection carries the organisation of the context that acquired it in [OrgSetting](#OrgSetting), and [BypassSetting](#BypassSetting) for a context from [WithoutRowLevelSecurity](#WithoutRowLevelSecurity), for row-level security policies (ADR-0061). Setting them costs one round trip when an acquire changes them, and nothing otherwise. Don't set these settings, or run RESET ALL or DISCARD ALL, yourself; and don't put the pool behind a pooler in transaction mode, which doesn't keep session settings with a client. Close the pool on shutdown:

```go
cleanup.Add("postgres", func(context.Context) error { pool.Close(); return nil })
```

*Since `v0.1.0`*

**Example**

```go
ctx := context.Background()
url, err := config.OS.Secret("DATABASE_URL")
if err != nil {
	log.Fatal(err)
}
pool, err := postgres.Open(ctx, url,
	postgres.WithMaxConns(20),
	postgres.WithApplicationName("acme-api"),
)
if err != nil {
	log.Fatal(err)
}
defer pool.Close()
```

<a id="UniqueViolation"></a>

### func UniqueViolation

```go
func UniqueViolation(err error) (constraint string, ok bool)
```

UniqueViolation reports whether err is a unique constraint violation and returns the constraint's name, so repositories can map it to a domain error such as ErrDuplicateEmail.

*Since `v0.1.0`*

<a id="WithOrg"></a>

### func WithOrg

```go
func WithOrg(ctx context.Context, orgID string) context.Context
```

WithOrg returns a copy of ctx whose connections carry orgID in [OrgSetting](#OrgSetting), so row-level security policies limit them to that organisation's rows. Without it, a connection carries the organisation of the context's actor ([actor.Actor.OrgID](actor.md#Actor.OrgID), set by orgs.RequireMember after checking membership), or none. Use it where code acts in one organisation without an organisation actor, such as a job reading one organisation's rows.

The organisation is set when a connection is acquired: a transaction keeps the one its context had at BeginTx.

*Since `v0.1.0`*

<a id="WithScope"></a>

### func WithScope

```go
func WithScope(ctx context.Context, scopeID string) context.Context
```

WithScope returns a copy of ctx whose connections carry scopeID in [ScopeSetting](#ScopeSetting), so row-level security policies limit them to that scope's rows. It is [WithOrg](#WithOrg) under the name the framework uses for tenancy since v0.2.2, and is what gorbital.Scope.Session is usually set to.

*Since `v0.2.0 (unreleased)`*

**Example**

WithScope carries one scope into a context's connections, so row-level security policies limit them to that scope's rows. Use it where code acts in one scope without a scoped actor, such as a job working through one merchant's orders.

```go
ctx := postgres.WithScope(context.Background(), "mch_1")

// Connections acquired from ctx now set postgres.ScopeSetting to
// mch_1, and policies written against it see only that merchant.
log.Println(postgres.ScopeSetting, ctx != nil)
```

<a id="WithoutRowLevelSecurity"></a>

### func WithoutRowLevelSecurity

```go
func WithoutRowLevelSecurity(ctx context.Context, reason string) context.Context
```

WithoutRowLevelSecurity returns a copy of ctx whose connections set [BypassSetting](#BypassSetting) to "on", so policies let them read and write every organisation's rows. It is for system paths that work across organisations, such as migrations and maintenance jobs; reason names the path ("migrate", "job:orgs\_purge"). It panics when reason is empty.

Decide it in code, never from request input. The first connection acquired with the returned context is logged at info level through the pool's logger ([WithLogger](#WithLogger)), and every query span it runs carries gorbital.rls\_bypass with the reason.

*Since `v0.1.0`*

## Types

<a id="Beginner"></a>
<a id="Beginner.BeginTx"></a>

### type Beginner

```go
type Beginner interface {
	BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error)
}
```

Beginner starts transactions. \*pgxpool.Pool and \*pgx.Conn implement it.

*Since `v0.1.0`*

<a id="DBTX"></a>
<a id="DBTX.Exec"></a>
<a id="DBTX.Query"></a>
<a id="DBTX.QueryRow"></a>

### type DBTX

```go
type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}
```

DBTX runs queries. \*pgxpool.Pool, \*pgxpool.Conn, \*pgx.Conn and pgx.Tx implement it, so repositories work with or without a transaction.

*Since `v0.1.0`*

<a id="MigrationInfo"></a>
<a id="MigrationInfo.Version"></a>
<a id="MigrationInfo.Path"></a>
<a id="MigrationInfo.Applied"></a>
<a id="MigrationInfo.AppliedAt"></a>

### type MigrationInfo

```go
type MigrationInfo struct {
	Version int64
	// Path is the file's path in fsys.
	Path      string
	Applied   bool
	AppliedAt time.Time
}
```

MigrationInfo is one migration file and whether the database has it.

*Since `v0.1.0`*

<a id="MigrationList"></a>

#### func MigrationList

```go
func MigrationList(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS) ([]MigrationInfo, error)
```

MigrationList reports every migration in fsys with its state, oldest first. It changes nothing.

*Since `v0.1.0`*

<a id="MigrationState"></a>
<a id="MigrationState.Current"></a>
<a id="MigrationState.Latest"></a>
<a id="MigrationState.Pending"></a>

### type MigrationState

```go
type MigrationState struct {
	// Current is the highest applied version, 0 when none are applied.
	Current int64
	// Latest is the highest version in the migration files.
	Latest int64
	// Pending counts migration files not yet applied.
	Pending int
}
```

MigrationState describes the schema version against a set of migrations.

*Since `v0.1.0`*

<a id="Migrations"></a>

#### func Migrations

```go
func Migrations(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS) (MigrationState, error)
```

Migrations reports the database's migration state for fsys, for readiness reports and doctor commands. It changes nothing.

*Since `v0.1.0`*

<a id="Option"></a>

### type Option

```go
type Option interface {
	// contains filtered or unexported methods
}
```

An Option configures [Open](#Open).

*Since `v0.1.0`*

<a id="WithApplicationName"></a>

#### func WithApplicationName

```go
func WithApplicationName(name string) Option
```

WithApplicationName sets application\_name, shown in pg\_stat\_activity.

*Since `v0.1.0`*

<a id="WithConnectTimeout"></a>

#### func WithConnectTimeout

```go
func WithConnectTimeout(d time.Duration) Option
```

WithConnectTimeout bounds each connection attempt and the ping in [Open](#Open). Default: [DefaultConnectTimeout](#DefaultConnectTimeout).

*Since `v0.1.0`*

<a id="WithLogger"></a>

#### func WithLogger

```go
func WithLogger(logger *slog.Logger) Option
```

WithLogger sets the logger that records uses of [WithoutRowLevelSecurity](#WithoutRowLevelSecurity). Default: discard.

*Since `v0.1.0`*

<a id="WithMaxConnIdleTime"></a>

#### func WithMaxConnIdleTime

```go
func WithMaxConnIdleTime(d time.Duration) Option
```

WithMaxConnIdleTime closes connections idle longer than d. Default: 30 minutes.

*Since `v0.1.0`*

<a id="WithMaxConnLifetime"></a>

#### func WithMaxConnLifetime

```go
func WithMaxConnLifetime(d time.Duration) Option
```

WithMaxConnLifetime closes connections older than d, so load moves to new database hosts after failover. Default: one hour.

*Since `v0.1.0`*

<a id="WithMaxConns"></a>

#### func WithMaxConns

```go
func WithMaxConns(n int32) Option
```

WithMaxConns sets the maximum pool size. Default: pgx's default, the greater of 4 and the number of CPUs.

*Since `v0.1.0`*

<a id="WithMeterProvider"></a>

#### func WithMeterProvider

```go
func WithMeterProvider(mp metric.MeterProvider) Option
```

WithMeterProvider reports the pool's connection and acquire statistics as metrics of mp, read when metrics are collected: db.client.connection.count (by state, idle or used), db.client.connection.max, pgxpool.acquires, pgxpool.acquire.waits, pgxpool.acquire.wait\_time, pgxpool.acquire.canceled and pgxpool.connections.created. Series are labelled with db.client.connection.pool.name, the application name set by [WithApplicationName](#WithApplicationName) or "postgres". The instruments stay registered for the life of mp. Default: no pool metrics.

*Since `v0.1.0`*

<a id="WithMinConns"></a>

#### func WithMinConns

```go
func WithMinConns(n int32) Option
```

WithMinConns sets how many connections the pool keeps open when idle. Default: 0.

*Since `v0.1.0`*

<a id="WithTracerProvider"></a>

#### func WithTracerProvider

```go
func WithTracerProvider(tp trace.TracerProvider) Option
```

WithTracerProvider sets the provider for query spans. Default: the global OpenTelemetry provider.

*Since `v0.1.0`*

<a id="RowLevelSecurityReport"></a>
<a id="RowLevelSecurityReport.Role"></a>
<a id="RowLevelSecurityReport.Bypasses"></a>
<a id="RowLevelSecurityReport.Forced"></a>
<a id="RowLevelSecurityReport.NotForced"></a>
<a id="RowLevelSecurityReport.Unprotected"></a>

### type RowLevelSecurityReport

```go
type RowLevelSecurityReport struct {
	// Role is the role queries run as (current_user).
	Role string
	// Bypasses reports a superuser or a role with BYPASSRLS: PostgreSQL
	// applies no policy to it, forced or not.
	Bypasses bool
	// Forced lists tables with row-level security enabled and forced, which
	// limits their owner too.
	Forced []string
	// NotForced lists tables with row-level security enabled but not
	// forced: a role that owns one isn't limited by its policies.
	NotForced []string
	// Unprotected lists tables with a NOT NULL org_id column and row-level
	// security off.
	Unprotected []string
}
```

RowLevelSecurityReport describes row-level security in the current schema for the role the connection runs as, so apps can warn at startup and in orb doctor when policies exist but don't apply (ADR-0061).

*Since `v0.1.0`*

<a id="CheckRowLevelSecurity"></a>

#### func CheckRowLevelSecurity

```go
func CheckRowLevelSecurity(ctx context.Context, db DBTX) (RowLevelSecurityReport, error)
```

CheckRowLevelSecurity reports row-level security in the current schema as the role db connects with. It changes nothing.

*Since `v0.1.0`*

<a id="RowLevelSecurityReport.On"></a>

#### func (RowLevelSecurityReport) On

```go
func (r RowLevelSecurityReport) On() bool
```

On reports whether any table has row-level security enabled.

*Since `v0.1.0`*
