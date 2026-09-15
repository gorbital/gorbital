# Database guide

`apistock.dev/modules/postgres` connects apps to PostgreSQL. Decisions: [ADR-0005](../adr/0005-database-strategy.md) (PostgreSQL, goose), [ADR-0028](../adr/0028-local-development-environment.md) (Docker), [ADR-0032](../adr/0032-repository-sql.md) (hand-written SQL).

## Connecting

```go
pool, err := postgres.Open(ctx, cfg.DatabaseURL, // config.Secret from DATABASE_URL
	postgres.WithMaxConns(10),
	postgres.WithApplicationName("acme-api"),
	postgres.WithTracerProvider(tel.TracerProvider()),
)
if err != nil {
	return err
}
cleanup.Add("postgres", func(context.Context) error { pool.Close(); return nil })
checker.Add(postgres.HealthCheck(pool)) // /readyz
```

| Option | Default | Notes |
|---|---|---|
| `WithMaxConns(n)` | max(4, CPUs) | Pool size per instance |
| `WithMinConns(n)` | 0 | Idle connections kept open |
| `WithMaxConnLifetime(d)` | 1 hour | Moves load to new hosts after failover |
| `WithMaxConnIdleTime(d)` | 30 minutes | |
| `WithConnectTimeout(d)` | 5 seconds | Bounds each connection attempt and the startup ping |
| `WithApplicationName(s)` | none | Shown in `pg_stat_activity` |
| `WithTracerProvider(tp)` | global | Every query becomes a client span with its SQL text, never its arguments |

`Open` pings the database, so a wrong URL fails at startup. Errors never include the URL, which may contain a password.

## Repositories

Repositories use hand-written SQL, one file per operation, like this:

```text
internal/modules/users/repository/
├── store.go          UserStore{db postgres.DBTX}; NewUserStore(db)
├── errors.go         constraint names → domain errors
├── scan.go           shared row mapping (when several reads return the same columns)
├── insert_user.go    const insertUserSQL + (*UserStore).InsertUser
├── update_user.go
├── delete_user.go
├── select_user.go    SelectUserByID, SelectUsers
└── *_test.go         every method tested against a real database
```

### store.go

```go
// UserStore persists users. It works on the pool or inside a transaction.
type UserStore struct {
	db postgres.DBTX
}

func NewUserStore(db postgres.DBTX) *UserStore { return &UserStore{db: db} }
```

`postgres.DBTX` is implemented by `*pgxpool.Pool`, `*pgxpool.Conn`, `*pgx.Conn` and `pgx.Tx`.

### insert_user.go

```go
const insertUserSQL = `
	INSERT INTO users (id, name, email, password_hash, role, status)
	VALUES ($1, $2, $3, $4, $5, $6)
	RETURNING created_at, updated_at`

// InsertUser inserts a new user row.
func (s *UserStore) InsertUser(ctx context.Context, u *domain.User) error {
	err := s.db.QueryRow(ctx, insertUserSQL,
		u.ID, u.Name, u.Email, u.PasswordHash, u.Role, u.Status,
	).Scan(&u.CreatedAt, &u.UpdatedAt)
	if constraint, ok := postgres.UniqueViolation(err); ok && constraint == usersEmailKey {
		return domain.ErrDuplicateEmail
	}
	if err != nil {
		return fmt.Errorf("insert user: %v", err)
	}
	return nil
}
```

### select_user.go

```go
const selectUserByIDSQL = `
	SELECT id, name, email, role, status, created_at, updated_at
	FROM users
	WHERE id = $1 AND deleted_at IS NULL`

func (s *UserStore) SelectUserByID(ctx context.Context, id string) (*domain.User, error) {
	var u domain.User
	err := s.db.QueryRow(ctx, selectUserByIDSQL, id).
		Scan(&u.ID, &u.Name, &u.Email, &u.Role, &u.Status, &u.CreatedAt, &u.UpdatedAt)
	if postgres.IsNoRows(err) {
		return nil, domain.ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("select user: %v", err)
	}
	return &u, nil
}
```

For wide or shared rows, scan into an unexported row struct with `pgx.CollectRows(rows, pgx.RowToStructByName[userRow])` in `scan.go`, so columns are matched by name. Domain structs never get `db` tags.

### delete_user.go

```go
const deleteUserSQL = `
	UPDATE users SET deleted_at = now(), updated_at = now()
	WHERE id = $1 AND deleted_at IS NULL`

func (s *UserStore) DeleteUser(ctx context.Context, id string) error {
	tag, err := s.db.Exec(ctx, deleteUserSQL, id)
	if err != nil {
		return fmt.Errorf("delete user: %v", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrUserNotFound
	}
	return nil
}
```

### Rules

- SQL lives in an unexported `const` next to the method that runs it; values are always placeholders, never concatenated.
- Translate expected driver conditions into domain errors; wrap everything else with `%v` so pgx errors never become part of the module's API (ADR-0018).
- A dynamic `ORDER BY` uses an allowlist of column names.

## Error helpers

| Helper | Returns true for | Also returns |
|---|---|---|
| `IsNoRows(err)` | `QueryRow(...).Scan` found nothing | |
| `UniqueViolation(err)` | SQLSTATE 23505 | constraint name |
| `ForeignKeyViolation(err)` | 23503 | constraint name |
| `CheckViolation(err)` | 23514 | constraint name |
| `NotNullViolation(err)` | 23502 | column name |
| `IsRetryable(err)` | serialization failure or deadlock | |

## Transactions

```go
err := postgres.InTx(ctx, pool, func(tx pgx.Tx) error {
	users := repository.NewUserStore(tx)
	if err := users.InsertUser(ctx, u); err != nil {
		return err // rolled back; the domain error is returned unchanged
	}
	_, err := jobsClient.InsertTx(ctx, tx, welcome.Args{UserID: u.ID}, nil) // job commits with the user
	return err
})
```

- `InTx` commits when the function returns nil and rolls back on an error or a panic (the panic continues).
- Build tx-bound stores inside the function; never keep `tx` after it returns or put it in a context (ADR-0030).
- `InTxWithOptions(ctx, pool, pgx.TxOptions{IsoLevel: pgx.Serializable}, fn)`; retry the whole function when `IsRetryable(err)`.
- In generated apps, use cases reach transactions through a `TxManager` port and never import pgx (ADR-0022).

## Migrations

- Goose SQL files in `db/migrations`, named `<timestamp>_<description>.sql`, with `-- +goose Up` sections.
- One ordered history for the app's tables and apistock module tables (modules ship theirs as `settings.Migrations`, `jobs.Migrations`, `auditpg.Migrations`, and they are copied in).
- Released migrations are never edited; changes are new files, forward-only.
- `aps gen migration <name>` creates an empty one that runs after the existing ones ([CLI guide](cli.md#aps-gen-migration)). Write its SQL before running `cmd/migrate`: an empty migration is recorded as applied.
- `cmd/migrate` applies them with `postgres.Migrate(ctx, pool, migrations.FS)`, then River's migrations with `jobs.Migrate`. Apps never migrate at startup (ADR-0017).
- `postgres.Migrate` takes a PostgreSQL advisory lock, so concurrent migrators apply each migration once.
- `postgres.Migrations(ctx, pool, fsys)` reports `Current`, `Latest` and `Pending` without changing anything.

## Testing with pgtest

```go
func TestInsertUser(t *testing.T) {
	pool := pgtest.New(t, pgtest.WithMigrations(migrations.FS))
	store := repository.NewUserStore(pool)
	// ...
}
```

| Function | Returns | Use for |
|---|---|---|
| `pgtest.New(t, opts...)` | `*pgxpool.Pool` on a fresh database | Repository and module tests |
| `pgtest.NewDatabase(t, opts...)` | Connection URL of a fresh database | Tests that start a whole app from config |
| `pgtest.URL(t)` | The server URL | Tests that need the server itself |

Options: `WithMigrations(fsys)` (applied once into a template per distinct set of files), `WithMaxConns(n)` (default 4). Databases are dropped when the test ends. See [local development](local-development.md) for the environment variables.
