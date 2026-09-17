package pgmeta

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The SQL editor (ADR-0068): scripts run in one transaction that is rolled
// back unless the developer chose to commit, every statement's result
// comes back as text cells, and the server's error carries its position.

// Run modes.
const (
	// ModeRollback runs the script and rolls back: see what it would do.
	ModeRollback = "rollback"
	// ModeCommit runs the script and commits.
	ModeCommit = "commit"
	// ModeReadOnly runs in a read-only transaction: writes fail.
	ModeReadOnly = "readonly"
)

// Limits of a run.
const (
	// DefaultRunTimeout bounds a script; MaxRunTimeout is the most a
	// request may ask for.
	DefaultRunTimeout = 30 * time.Second
	MaxRunTimeout     = 5 * time.Minute
	// DefaultRowLimit is how many rows of each result set come back
	// without a limit; MaxRowLimit the most.
	DefaultRowLimit = 500
	MaxRowLimit     = 10_000
	// maxScriptBytes bounds a script.
	maxScriptBytes = 1 << 20
)

// RunRequest is a script to run.
type RunRequest struct {
	SQL  string `json:"sql"`
	Mode string `json:"mode,omitempty"`
	// RowLimit caps the rows of each result set (DefaultRowLimit, at most
	// MaxRowLimit); 0 means the default.
	RowLimit int `json:"row_limit,omitempty"`
	// TimeoutSeconds bounds the script (DefaultRunTimeout, at most
	// MaxRunTimeout).
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
}

// StatementResult is one statement's outcome.
type StatementResult struct {
	// Command is the command tag, such as SELECT 3 or UPDATE 1.
	Command string `json:"command"`
	// Columns and Rows are the result set, when the statement returns one;
	// Truncated reports rows beyond the limit were dropped.
	Columns      []string `json:"columns,omitempty"`
	Rows         [][]Cell `json:"rows,omitempty"`
	RowsAffected int64    `json:"rows_affected"`
	Truncated    bool     `json:"truncated,omitempty"`
}

// RunError is the server's error for a script, with where it happened.
type RunError struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
	Detail  string `json:"detail,omitempty"`
	Hint    string `json:"hint,omitempty"`
	// Position is the 1-based byte offset in the script; Line the line.
	Position int `json:"position,omitempty"`
	Line     int `json:"line,omitempty"`
}

// RunResult is a script's outcome.
type RunResult struct {
	Mode       string            `json:"mode"`
	Statements []StatementResult `json:"statements"`
	Error      *RunError         `json:"error,omitempty"`
	// Committed reports the transaction was committed; RolledBack that it
	// was rolled back (by the mode, or because of the error).
	Committed  bool    `json:"committed"`
	RolledBack bool    `json:"rolled_back"`
	DurationMS float64 `json:"duration_ms"`
	// Warnings are what Check found before the run.
	Warnings []Warning `json:"warnings,omitempty"`
}

// ErrScriptControlsTransaction reports a script with its own transaction
// control in rollback mode.
var ErrScriptControlsTransaction = errors.New("pgmeta: the script controls its own transaction (BEGIN, COMMIT, ROLLBACK, END); run it in commit mode")

var txControl = regexp.MustCompile(`(?im)^\s*(BEGIN|START\s+TRANSACTION|COMMIT|ROLLBACK|END)\b`)

// Run runs a script in one transaction.
func (c *Client) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	mode := req.Mode
	if mode == "" {
		mode = ModeRollback
	}
	if mode != ModeRollback && mode != ModeCommit && mode != ModeReadOnly {
		return RunResult{}, fmt.Errorf("%w: mode is rollback, commit or readonly", ErrInvalidInput)
	}
	if len(req.SQL) > maxScriptBytes {
		return RunResult{}, fmt.Errorf("%w: the script is over %d bytes", ErrInvalidInput, maxScriptBytes)
	}
	if strings.TrimSpace(req.SQL) == "" {
		return RunResult{}, fmt.Errorf("%w: the script is empty", ErrInvalidInput)
	}
	if mode != ModeCommit && txControl.MatchString(req.SQL) {
		return RunResult{}, ErrScriptControlsTransaction
	}
	limit := req.RowLimit
	if limit <= 0 {
		limit = DefaultRowLimit
	}
	if limit > MaxRowLimit {
		limit = MaxRowLimit
	}
	timeout := time.Duration(req.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = DefaultRunTimeout
	}
	if timeout > MaxRunTimeout {
		timeout = MaxRunTimeout
	}

	out := RunResult{Mode: mode, Statements: []StatementResult{}, Warnings: Check(req.SQL)}
	ctx, cancel := context.WithTimeout(ctx, timeout+5*time.Second)
	defer cancel()
	opts := pgx.TxOptions{}
	if mode == ModeReadOnly {
		opts.AccessMode = pgx.ReadOnly
	}
	tx, err := c.pool.BeginTx(ctx, opts)
	if err != nil {
		return RunResult{}, fmt.Errorf("pgmeta: begin: %w", err)
	}
	rollback := context.WithoutCancel(ctx)
	if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL statement_timeout = '%dms'", timeout.Milliseconds())); err != nil {
		_ = tx.Rollback(rollback)
		return RunResult{}, err
	}
	start := time.Now()
	results, runErr := tx.Conn().PgConn().Exec(ctx, req.SQL).ReadAll()
	out.DurationMS = float64(time.Since(start).Microseconds()) / 1000
	for _, r := range results {
		out.Statements = append(out.Statements, statementResult(r, limit))
	}
	if runErr != nil {
		out.Error = runError(runErr, req.SQL)
		_ = tx.Rollback(rollback)
		out.RolledBack = true
		return out, nil
	}
	if mode == ModeCommit {
		if err := tx.Commit(ctx); err != nil {
			out.Error = runError(err, req.SQL)
			out.RolledBack = true
			return out, nil
		}
		out.Committed = true
		return out, nil
	}
	if err := tx.Rollback(rollback); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		return out, fmt.Errorf("pgmeta: rollback: %w", err)
	}
	out.RolledBack = true
	return out, nil
}

// statementResult converts one result, keeping at most limit rows.
func statementResult(r *pgconn.Result, limit int) StatementResult {
	s := StatementResult{Command: r.CommandTag.String(), RowsAffected: r.CommandTag.RowsAffected()}
	if len(r.FieldDescriptions) == 0 {
		return s
	}
	s.Columns = make([]string, 0, len(r.FieldDescriptions))
	for _, f := range r.FieldDescriptions {
		s.Columns = append(s.Columns, f.Name)
	}
	s.Rows = make([][]Cell, 0, min(len(r.Rows), limit))
	for i, raw := range r.Rows {
		if i >= limit {
			s.Truncated = true
			break
		}
		row := make([]Cell, len(raw))
		for j, v := range raw {
			if v != nil {
				str := string(v)
				row[j] = &str
			}
		}
		s.Rows = append(s.Rows, row)
	}
	return s
}

// runError converts the server's error, locating it in the script.
func runError(err error, script string) *RunError {
	out := &RunError{Message: err.Error()}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		out.Message, out.Code, out.Detail, out.Hint = pgErr.Message, pgErr.Code, pgErr.Detail, pgErr.Hint
		if pgErr.Position > 0 {
			out.Position = int(pgErr.Position)
			out.Line = 1 + strings.Count(script[:min(len(script), out.Position-1)], "\n")
		}
		if pgErr.Code == "57014" {
			out.Message = "the script took too long and was cancelled: " + pgErr.Message
		}
	}
	return out
}

// Explain returns the plan of one statement as EXPLAIN (FORMAT JSON)
// prints it; with analyze it runs the statement in a rolled-back
// transaction.
func (c *Client) Explain(ctx context.Context, sql string, analyze bool) (json.RawMessage, error) {
	if strings.TrimSpace(sql) == "" {
		return nil, fmt.Errorf("%w: the statement is empty", ErrInvalidInput)
	}
	if txControl.MatchString(sql) {
		return nil, ErrScriptControlsTransaction
	}
	var plan json.RawMessage
	err := c.readOnlyTx(ctx, analyze, func(tx pgx.Tx) error {
		explain := "EXPLAIN (FORMAT JSON, VERBOSE"
		if analyze {
			explain += ", ANALYZE, BUFFERS"
		}
		rows, err := tx.Query(ctx, explain+") "+strings.TrimRight(strings.TrimSpace(sql), ";"))
		if err != nil {
			return inputError(err)
		}
		defer rows.Close()
		var parts []string
		for rows.Next() {
			raw := rows.RawValues()
			if len(raw) > 0 && raw[0] != nil {
				parts = append(parts, string(raw[0]))
			}
		}
		if err := rows.Err(); err != nil {
			return inputError(err)
		}
		plan = json.RawMessage(strings.Join(parts, ""))
		return nil
	})
	return plan, err
}

// readOnlyTx runs fn and rolls back; writable allows writes inside the
// transaction (EXPLAIN ANALYZE of an UPDATE), still rolled back.
func (c *Client) readOnlyTx(ctx context.Context, writable bool, fn func(tx pgx.Tx) error) error {
	opts := pgx.TxOptions{AccessMode: pgx.ReadOnly}
	if writable {
		opts = pgx.TxOptions{}
	}
	tx, err := c.pool.BeginTx(ctx, opts)
	if err != nil {
		return fmt.Errorf("pgmeta: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout = '30s'"); err != nil {
		return err
	}
	return fn(tx)
}

// Warning is something Check found in a script.
type Warning struct {
	// Kind: drop, truncate, delete_without_where, update_without_where,
	// drop_column, alter_type.
	Kind    string `json:"kind"`
	Message string `json:"message"`
	// Line is where the statement starts.
	Line int `json:"line"`
}

var (
	stripComments = regexp.MustCompile(`(?s)--[^\n]*|/\*.*?\*/`)
	stripStrings  = regexp.MustCompile(`'(?:[^']|'')*'`)
	warningRules  = []struct {
		kind, message string
		re            *regexp.Regexp
		// noWhere warns only when the statement has no WHERE.
		noWhere bool
	}{
		{"drop", "drops a database object", regexp.MustCompile(`(?i)^\s*DROP\s+(TABLE|SCHEMA|DATABASE|INDEX|TYPE|VIEW|MATERIALIZED\s+VIEW|FUNCTION|SEQUENCE|TRIGGER|EXTENSION)\b`), false},
		{"truncate", "removes every row of the table", regexp.MustCompile(`(?i)^\s*TRUNCATE\b`), false},
		{"drop_column", "drops a column and its values", regexp.MustCompile(`(?i)^\s*ALTER\s+TABLE\b.*\bDROP\s+(COLUMN\s+)?(IF\s+EXISTS\s+)?"?[A-Za-z_]`), false},
		{"delete_without_where", "deletes every row of the table (no WHERE)", regexp.MustCompile(`(?i)^\s*DELETE\s+FROM\b`), true},
		{"update_without_where", "updates every row of the table (no WHERE)", regexp.MustCompile(`(?i)^\s*UPDATE\b`), true},
		{"alter_type", "changes a column's type, converting every value", regexp.MustCompile(`(?i)^\s*ALTER\s+TABLE\b.*\b(SET\s+DATA\s+)?TYPE\b`), false},
	}
	wherePattern = regexp.MustCompile(`(?i)\bWHERE\b`)
)

// Check finds statements a developer should confirm before running: drops,
// truncates, deletes and updates without WHERE, dropped columns and type
// changes. It splits on semicolons outside strings and comments, so it
// misses dollar-quoted bodies; it is a warning, not a guard.
func Check(script string) []Warning {
	clean := stripStrings.ReplaceAllStringFunc(stripComments.ReplaceAllStringFunc(script, blank), blank)
	var warnings []Warning
	offset := 0
	for _, stmt := range strings.Split(clean, ";") {
		line := 1 + strings.Count(clean[:offset], "\n")
		offset += len(stmt) + 1
		flat := strings.Join(strings.Fields(stmt), " ")
		if flat == "" {
			continue
		}
		for _, rule := range warningRules {
			if !rule.re.MatchString(flat) || (rule.noWhere && wherePattern.MatchString(flat)) {
				continue
			}
			warnings = append(warnings, Warning{Kind: rule.kind, Message: rule.message, Line: line + leadingNewlines(stmt)})
		}
	}
	return warnings
}

// blank replaces s with spaces and newlines, keeping positions.
func blank(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] != '\n' {
			b[i] = ' '
		}
	}
	return string(b)
}

func leadingNewlines(s string) int {
	n := 0
	for _, r := range s {
		switch r {
		case '\n':
			n++
		case ' ', '\t', '\r':
		default:
			return n
		}
	}
	return n
}

// Template is a ready-made query.
type Template struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	SQL         string `json:"sql"`
	// Needs names an extension the template needs, if any.
	Needs string `json:"needs,omitempty"`
}

// Templates returns the built-in queries.
func Templates() []Template {
	return []Template{
		{Name: "Table sizes", Description: "Every table with its rows and size, largest first", SQL: `SELECT n.nspname AS schema, c.relname AS table,
       GREATEST(c.reltuples, 0)::bigint AS row_estimate,
       pg_size_pretty(pg_total_relation_size(c.oid)) AS total_size,
       pg_size_pretty(pg_relation_size(c.oid)) AS table_size,
       pg_size_pretty(pg_indexes_size(c.oid)) AS index_size
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind IN ('r', 'p') AND n.nspname NOT IN ('pg_catalog', 'information_schema')
ORDER BY pg_total_relation_size(c.oid) DESC
LIMIT 50;`},
		{Name: "Index usage", Description: "Indexes and how often they were used; unused ones are candidates to drop", SQL: `SELECT schemaname AS schema, relname AS table, indexrelname AS index,
       idx_scan AS scans, idx_tup_read AS tuples_read,
       pg_size_pretty(pg_relation_size(indexrelid)) AS size
FROM pg_stat_user_indexes
ORDER BY idx_scan ASC, pg_relation_size(indexrelid) DESC
LIMIT 50;`},
		{Name: "Missing indexes", Description: "Tables read by sequential scans far more than by index", SQL: `SELECT schemaname AS schema, relname AS table, seq_scan, seq_tup_read, idx_scan,
       n_live_tup AS live_rows
FROM pg_stat_user_tables
WHERE seq_scan > idx_scan AND n_live_tup > 1000
ORDER BY seq_tup_read DESC
LIMIT 50;`},
		{Name: "Table bloat", Description: "Dead rows waiting for vacuum, per table", SQL: `SELECT schemaname AS schema, relname AS table, n_live_tup AS live_rows, n_dead_tup AS dead_rows,
       round(100.0 * n_dead_tup / GREATEST(n_live_tup + n_dead_tup, 1), 1) AS dead_pct,
       last_autovacuum, last_autoanalyze
FROM pg_stat_user_tables
ORDER BY n_dead_tup DESC
LIMIT 50;`},
		{Name: "Active connections", Description: "Every connection, what it runs and for how long", SQL: `SELECT pid, usename AS user, application_name, client_addr, state,
       now() - query_start AS running_for, wait_event_type, left(query, 120) AS query
FROM pg_stat_activity
WHERE datname = current_database() AND pid <> pg_backend_pid()
ORDER BY query_start;`},
		{Name: "Locks", Description: "Who waits for whom", SQL: `SELECT blocked.pid AS blocked_pid, left(blocked.query, 80) AS blocked_query,
       blocking.pid AS blocking_pid, left(blocking.query, 80) AS blocking_query
FROM pg_stat_activity blocked
JOIN pg_stat_activity blocking ON blocking.pid = ANY(pg_blocking_pids(blocked.pid))
WHERE cardinality(pg_blocking_pids(blocked.pid)) > 0;`},
		{Name: "Slow queries", Description: "The statements that took the most time in all (needs pg_stat_statements)", Needs: "pg_stat_statements", SQL: `SELECT calls, round(total_exec_time::numeric, 1) AS total_ms, round(mean_exec_time::numeric, 2) AS mean_ms,
       rows, left(query, 120) AS query
FROM pg_stat_statements
ORDER BY total_exec_time DESC
LIMIT 25;`},
		{Name: "River queues", Description: "Background jobs by queue and state", SQL: `SELECT queue, state, count(*) AS jobs, min(scheduled_at) AS oldest
FROM river_job
GROUP BY queue, state
ORDER BY queue, state;`},
		{Name: "Failed jobs", Description: "The most recent job errors", SQL: `SELECT id, kind, queue, state, attempt, max_attempts, finalized_at,
       errors[array_length(errors, 1)] ->> 'error' AS last_error
FROM river_job
WHERE state IN ('retryable', 'discarded')
ORDER BY finalized_at DESC NULLS LAST
LIMIT 25;`},
		{Name: "Recent audit events", Description: "The last 50 audit events", SQL: `SELECT occurred_at, action, actor_kind, actor_id, target_kind, target_id
FROM audit_events
ORDER BY occurred_at DESC
LIMIT 50;`},
		{Name: "Migrations", Description: "Applied migrations, newest first", SQL: `SELECT version_id, is_applied, tstamp
FROM goose_db_version
ORDER BY id DESC
LIMIT 50;`},
		{Name: "Extensions", Description: "Installed extensions", SQL: `SELECT extname, extversion, nspname AS schema
FROM pg_extension e
JOIN pg_namespace n ON n.oid = e.extnamespace
ORDER BY extname;`},
	}
}
