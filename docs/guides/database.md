# Database guide

`gorbital.dev/modules/postgres` connects apps to PostgreSQL. Decisions: [ADR-0005](../adr/0005-database-strategy.md) (PostgreSQL, goose), [ADR-0028](../adr/0028-local-development-environment.md) (Docker), [ADR-0032](../adr/0032-repository-sql.md) (hand-written SQL).

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
- One ordered history for the app's tables and gorbital module tables (modules ship theirs as `settings.Migrations`, `jobs.Migrations`, `auditpg.Migrations`, and they are copied in).
- Released migrations are never edited; changes are new files, forward-only.
- `orb gen migration <name>` creates an empty one that runs after the existing ones ([CLI guide](cli.md#orb-gen-migration)). Write its SQL before running `cmd/migrate`: an empty migration is recorded as applied.
- `cmd/migrate` applies them with `postgres.Migrate(ctx, pool, migrations.FS)`, then River's migrations with `jobs.Migrate`. Apps never migrate at startup (ADR-0017).
- `postgres.Migrate` takes a PostgreSQL advisory lock, so concurrent migrators apply each migration once.
- `postgres.Migrations(ctx, pool, fsys)` reports `Current`, `Latest` and `Pending` without changing anything.

## Running PostgreSQL locally

| | Generated app | gorbital repository |
|---|---|---|
| Defined in | The app's `compose.yaml`, service `postgres` | Root `compose.yaml` |
| Image | `postgres:18` | `postgres:18` |
| Address | `127.0.0.1:${POSTGRES_PORT:-5432}` | `127.0.0.1:${GORBITAL_POSTGRES_PORT:-55432}` |
| User, password, database | The app's name, all three | `gorbital` |
| Data | Named volume `postgres-data` | Named volume |
| Start | `orb dev`, or `docker compose up -d --wait` | `docker compose up -d --wait` |

Ports bind to `127.0.0.1` only, so the development password is never reachable from the network.

| Task | Command (in the app's folder) |
|---|---|
| Open a SQL shell | `docker compose exec postgres psql -U acme-api acme-api` |
| See the tables | `\dt` in `psql` |
| See applied migrations | `SELECT version_id, is_applied, tstamp FROM goose_db_version ORDER BY id;` |
| Stop, keep data | `docker compose down` |
| **Reset: delete all data** | `docker compose down -v`, then `orb dev` (migrates and seeds again) |
| Dump | `docker compose exec postgres pg_dump -U acme-api acme-api > dump.sql` |

## Commands

| Command | What it does | When |
|---|---|---|
| `go run ./cmd/migrate` | Opens `DATABASE_URL`, applies pending goose migrations from `db/migrations` under an advisory lock, then River's queue migrations; prints `applied migration <version>` for each and nothing when up to date | After pulling or generating migrations; before each release in production (`/migrate` in the image) |
| `go run ./cmd/seed` | Creates the development administrator and example data through the modules' use cases. Refuses when `APP_ENV=production`; needs `AUTH_ENCRYPTION_KEYS`; does nothing when `admin@example.com` exists | Development, after migrating; `orb dev` runs it |
| `orb gen migration <name>` | Creates an empty migration that sorts last | Schema changes outside `orb gen resource` |
| `orb gen resource …` | Creates a module with its own migration | New resources |

Each needs the environment: `orb dev` provides it, or `set -a; . ./.env; set +a`.

**Rollbacks.** There are no down migrations ([ADR-0005](../adr/0005-database-strategy.md)). To undo a released migration, write a new one that reverses it. Locally, before a migration is shared, edit it and reset with `docker compose down -v`. A database's schema and data are restored from backups, not by running migrations backwards.

## Schema of a Full app

The tables a new single-tenant Full app creates, by migration. Library modules own their tables' shape; the app owns its modules' tables. Every ID is `text` with a type prefix (`usr_…`), and every timestamp is `timestamptz` in UTC.

| Migration | Table | Owner | Holds |
|---|---|---|---|
| `…0001_settings` | `settings_values` | `modules/settings` | Runtime settings changed from their defaults: key, JSON value, version |
| | `settings_history` | `modules/settings` | Every change: old and new value, actor, reason |
| `…0002_jobs_definitions` | `jobs_definitions` | `modules/jobs` | Runtime configuration of each job: enabled, schedule, timeout, attempts, queue, priority, version |
| | `jobs_definition_history` | `modules/jobs` | Every configuration change |
| `…0003_audit_events` | `audit_events` | `modules/auditpg` | Append-only audit log: action, actor, resource, outcome, request ID, redacted metadata |
| `…0001_auth` | `auth_users` | App `auth` module | Accounts: email and its normalized form, argon2id password hash, verification and deletion times |
| | `auth_sessions` | App `auth` module | Sessions: SHA-256 of the token, idle and absolute expiry, last seen, user agent, revocation time and reason |
| | `auth_codes` | App `auth` module | Email verification and password reset codes: SHA-256, purpose, expiry, attempts |
| | `auth_user_roles` | App `auth` module | Platform roles (`org_id` NULL) and organisation-scoped roles |
| `…0002_projects` | `projects` | App `projects` module | The example resource, owned by a user |
| `…0003_release_instances` | `release_instances` | `modules/releases` | Each running or stopped instance: version, commit, start, heartbeat, clean stop |
| `…0004_auth_mfa` | `auth_totp` | App `auth` module | One TOTP secret per user, encrypted, with its key ID and last used step |
| | `auth_recovery_codes` | App `auth` module | SHA-256 of each recovery code, and when it was used |
| | `auth_mfa_challenges` | App `auth` module | Pending second-factor sign-ins: token hash, attempts and limit, expiry, consumption |
| `…0005_auth_passkeys` | `auth_passkeys` | App `auth` module | Passkeys: credential ID, verified credential record (public key, flags, counter), backup flags, name |
| | `auth_webauthn_ceremonies` | App `auth` module | Single-use WebAuthn challenges for registration, sign-in and verification |
| `…0006_auth_social` | `auth_identities` | App `auth` module | Google and Apple identities linked to users; Apple refresh tokens encrypted |
| | `auth_oauth_states` | App `auth` module | Web sign-in in progress: state, PKCE verifier, nonce, return address |
| | `auth_social_nonces` | App `auth` module | Single-use nonces for native ID-token sign-in |
| River (`jobs.Migrate`) | `river_job`, `river_leader`, `river_queue`, … | River | The job queue |
| goose | `goose_db_version` | goose | Applied migrations |

A multi-tenant app also has `orgs`, `org_members` and `org_invitations` (`…orgs.sql`), and its `projects` belong to an organisation (`org_id`, `created_by`) rather than a user.

### Relationships

```text
auth_users ─┬─< auth_sessions            ON DELETE CASCADE
            ├─< auth_codes               ON DELETE CASCADE
            ├─< auth_user_roles          ON DELETE CASCADE
            ├── auth_totp (1:1)          ON DELETE CASCADE
            ├─< auth_recovery_codes      ON DELETE CASCADE
            ├─< auth_mfa_challenges ─< auth_webauthn_ceremonies
            ├─< auth_passkeys            ON DELETE CASCADE
            ├─< auth_identities          ON DELETE CASCADE, UNIQUE (provider, subject)
            └─< projects (owner_id)      ON DELETE CASCADE

multi-tenant:
orgs ─┬─< org_members >── auth_users     PRIMARY KEY (org_id, user_id)
      ├─< org_invitations
      └─< projects (org_id)              ON DELETE CASCADE, UNIQUE (org_id, id)
```

Settings, job definitions, audit events and release instances have no foreign keys to users: audit history outlives the accounts it mentions. Deleting a user is soft first (`deleted_at`); the `auth_cleanup` job purges accounts after `auth.deleted_account_retention`, and the cascades remove their rows.

### Indexes worth knowing

| Index | Why |
|---|---|
| `auth_users_email` UNIQUE on `email_normalized` `WHERE deleted_at IS NULL` | One live account per address; a deleted account frees it |
| `auth_sessions_user`, `auth_sessions_expiry` | List a user's sessions; purge expired ones |
| `auth_identities (provider, subject)` UNIQUE | One account per Google or Apple identity |
| `settings_values_platform_key` / `_org_key`, partial on `org_id` | One value per key, platform-wide or per organisation |
| `audit_events_action`, `_actor`, `_resource`, `_org`, `_request` | The filters of `GET /ops/audit`, newest first by `id DESC` |
| `projects_owner_name` UNIQUE on `(owner_id, lower(name))` | Names unique per owner, ignoring case |
| `projects_owner_created`, `_updated`, `_name_sort` | One per sort of `GET /v1/projects`, ending in `id` for stable keyset pagination |
| Partial `*_expiry` indexes | Cleanup jobs delete expired rows without scanning |

### Transactions and concurrency

- Use cases that write more than one row do it in one transaction through their `TxManager` port (`postgres.InTx` underneath): a row and its audit event (`auditpg.RecordTx`), a user and their verification code, a row and a job (`jobs.Client.InsertTx`).
- Updates to resources, settings and job definitions carry a `version`; the `UPDATE … WHERE version = $n` fails with a conflict when someone changed the row first, so concurrent edits never overwrite each other silently.
- Reads that must not change before a write lock the row (`SELECT … FOR UPDATE`) inside the transaction.
- Single-use values (codes, challenges, nonces, OAuth states) are consumed with a conditional `UPDATE … WHERE consumed_at IS NULL`, so two concurrent uses can't both succeed, on any number of instances.
- Migrations take an advisory lock; River elects a single leader for periodic jobs.

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
