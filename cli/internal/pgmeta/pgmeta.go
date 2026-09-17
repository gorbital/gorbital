// Package pgmeta reads a PostgreSQL database's catalog, reads and edits
// rows, and renders schema changes as goose migrations, for the Dev
// Portal's Table Editor and Schema pages (ADR-0067).
//
// Three rules hold everywhere:
//
//   - Identifiers come from the catalog, never from the request as they
//     are: a column named in a query must exist in the table, and every
//     identifier is quoted with [pgx.Identifier].
//   - Values travel as parameters, as text the server casts to the column's
//     type, and come back as text (every column is cast to text), so any
//     type round-trips unchanged and nothing is lost to JSON numbers.
//   - Schema changes are written, never run directly: [Render] turns a
//     [Plan] into a migration file; the app's migrate command applies it.
//
// Catalog queries are adapted from supabase/postgres-meta (Apache-2.0),
// src/lib/sql/*.sql.ts, without information_schema and with the PostgreSQL
// 18 catalog changes (NOT NULL constraints in pg_constraint, virtual
// generated columns) taken into account.
package pgmeta

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Client reads and edits one database. It is safe for concurrent use.
type Client struct {
	pool *pgxpool.Pool
	// managedPrefixes name tables the framework's modules own, shown as
	// managed (rows editable with a warning, no schema changes).
	managedPrefixes []string
}

// DefaultManagedPrefixes are the table name prefixes of gorbital's modules
// in a Full app: their rows can be edited with a warning, their schema is
// the framework's.
var DefaultManagedPrefixes = []string{
	"auth_", "settings_", "audit_events", "jobs_definition", "release_instances", "flags_",
	"idempotency_keys", "mail_", "observability_", "incidents", "incident_updates", "ratelimit_",
}

// systemTables are owned by tools, never edited: goose's version table and
// River's queue tables.
var systemTables = []string{"goose_db_version", "river_"}

// Errors of the package.
var (
	// ErrUnknownColumn reports a column a query names that the table lacks.
	ErrUnknownColumn = errors.New("pgmeta: unknown column")
	// ErrUnknownOperator reports a filter operator outside the allowed set.
	ErrUnknownOperator = errors.New("pgmeta: unknown operator")
	// ErrNoPrimaryKey reports an edit on a table without a primary key.
	ErrNoPrimaryKey = errors.New("pgmeta: the table has no primary key, so rows can't be edited")
	// ErrRowCount reports an edit that would touch a different number of
	// rows than expected; nothing is written.
	ErrRowCount = errors.New("pgmeta: the edit would change an unexpected number of rows")
	// ErrNotFound reports a schema or table that doesn't exist.
	ErrNotFound = errors.New("pgmeta: not found")
	// ErrSystemTable reports a schema change on a system or managed table.
	ErrSystemTable = errors.New("pgmeta: the table belongs to a tool or to gorbital; change it with its own migrations")
	// ErrInvalidInput reports a value the server refused for its column, or
	// a malformed request; the message names the problem.
	ErrInvalidInput = errors.New("pgmeta: invalid input")
)

// Open connects to url with a small pool for the portal: two connections,
// UTC, and statement timeouts set per query.
func Open(ctx context.Context, url string) (*Client, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("pgmeta: parse database url: %w", err)
	}
	cfg.MaxConns = 2
	cfg.MinConns = 0
	cfg.MaxConnIdleTime = time.Minute
	cfg.ConnConfig.RuntimeParams["application_name"] = "orb dev portal"
	cfg.ConnConfig.RuntimeParams["TimeZone"] = "UTC"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("pgmeta: connect: %w", err)
	}
	return &Client{pool: pool, managedPrefixes: DefaultManagedPrefixes}, nil
}

// Close releases the connections.
func (c *Client) Close() {
	if c != nil && c.pool != nil {
		c.pool.Close()
	}
}

// Ping checks the connection.
func (c *Client) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return c.pool.Ping(ctx)
}

// ident quotes identifier parts: "schema"."table".
func ident(parts ...string) string { return pgx.Identifier(parts).Sanitize() }

// literal quotes s as a SQL string literal.
func literal(s string) string {
	if strings.ContainsAny(s, `\`) {
		return "E'" + strings.NewReplacer(`\`, `\\`, `'`, `''`).Replace(s) + "'"
	}
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// inputError turns a server's complaint about a value (SQLSTATE class 22,
// 23 and 42) into ErrInvalidInput with the server's message, and leaves
// other errors alone.
func inputError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code[:2] {
		case "22", "23", "42":
			return fmt.Errorf("%w: %s", ErrInvalidInput, pgErr.Message)
		}
	}
	return err
}
