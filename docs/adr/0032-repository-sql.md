# ADR-0032: Hand-written SQL in repositories

**Status:** Accepted (2026-09-14) · **Amends:** ADR-0005, ADR-0022

## Context

ADR-0005 chose sqlc: SQL in `db/queries/*.sql`, Go generated into `internal/db`, and repositories mapping generated types to domain types. The maintainer's previous production API used hand-written SQL instead, one repository file per operation, and the team found it easy to read, review and change. A generated package adds a tool to install, a `DO NOT EDIT` directory, a second place to look for each query, and mapping code between generated and domain types.

## Options

1. sqlc (ADR-0005 as written).
2. Hand-written SQL with pgx, one file per repository operation.
3. A query builder or ORM.

## Decision

Option 2. The generated app has no `sqlc.yaml`, no `db/queries` and no `internal/db`.

### Repository layout (per module)

```text
internal/modules/users/repository/
├── store.go          UserStore{db postgres.DBTX}; NewUserStore(db)
├── errors.go         constraint names → domain errors
├── scan.go           unexported row structs and mapping to domain types (when shared)
├── insert_user.go    const insertUserSQL + (*UserStore).InsertUser
├── update_user.go    const updateUserSQL + (*UserStore).UpdateUser
├── delete_user.go    const deleteUserSQL + (*UserStore).DeleteUser
├── select_user.go    const selectUserByIDSQL … + SelectUserByID, SelectUsers
└── *_test.go         every query runs against Docker PostgreSQL through pgtest
```

```go
// Shape only: repository/insert_user.go
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

### Rules

| Rule | Decision |
|---|---|
| Files | One file per operation, named after the method (`insert_user.go` → `InsertUser`); related reads may share a file |
| SQL | An unexported `const <name>SQL` next to the method that runs it; placeholders only, never string-built values; dynamic filters use a fixed query with nullable parameters or an allowlisted `ORDER BY` |
| Store | Holds `postgres.DBTX`, so the same store runs on the pool or inside `postgres.InTx` (ADR-0022 rule 5, ADR-0030) |
| Scanning | `Scan` into domain fields for short queries; for wide or shared rows, `pgx.CollectRows` with `pgx.RowToStructByName` into unexported row structs in `scan.go`, so columns are matched by name, not position. Domain structs stay tag-free (ADR-0022 rule 1) |
| Errors | `postgres.IsNoRows` → `domain.ErrXNotFound`; `postgres.UniqueViolation`/`ForeignKeyViolation`/`CheckViolation` with the constraint name → domain errors; anything else wrapped with `%v` (ADR-0018) |
| Deletes | `RowsAffected() == 0` → not found |
| Tests | Every repository method has a test against a real database from `pgtest` (Docker PostgreSQL, ADR-0028); the generator writes them with the repository |
| Library modules | Official modules (`auth`, `settings`, `auditpg`, …) follow the same style internally |

## Why

- A query and the Go code that runs it are in one file: "go to definition" lands on the SQL.
- No code-generation tool, no generated package, no mapping layer between generated and domain types.
- Familiar to the maintainer's team and to most Go developers who know `database/sql` or pgx.

## Trade-offs

- SQL is not checked against the schema at build time; typos and column mismatches surface in repository tests instead of at generate time. Repository tests are therefore required, not optional.
- More hand-written scanning code for wide tables (reduced by `RowToStructByName`).

## Consequences

- ADR-0005's "pgx + sqlc" becomes "pgx with hand-written SQL"; goose migrations, table prefixes and immutable released migrations are unchanged.
- `orb gen resource` generates the repository files and their tests in this layout.
- `modules/postgres` provides `DBTX`, `InTx`, error classification helpers and `pgtest`.
