package pgmeta

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// DatabaseStats is the database's health at a glance (ADR-0073): who is
// connected, how well the cache serves reads, what takes space, and what
// waits.
type DatabaseStats struct {
	Database string `json:"database"`
	Version  string `json:"version"`
	// Size of the database in bytes.
	Size int64 `json:"size"`
	// Connections against max_connections, by application and state.
	Connections    int         `json:"connections"`
	MaxConnections int         `json:"max_connections"`
	Clients        []ClientUse `json:"clients"`
	// CacheHitRatio is shared buffer hits over reads, 0 to 1, since the
	// statistics were last reset; IndexHitRatio the same for indexes.
	CacheHitRatio float64 `json:"cache_hit_ratio"`
	IndexHitRatio float64 `json:"index_hit_ratio"`
	// Transactions committed and rolled back since the reset.
	Commits   int64 `json:"commits"`
	Rollbacks int64 `json:"rollbacks"`
	Deadlocks int64 `json:"deadlocks"`
	TempBytes int64 `json:"temp_bytes"`
	// StatsSince is when the database's statistics were reset.
	StatsSince *time.Time `json:"stats_since"`
	// Tables by total size, largest first (at most 50).
	Tables []RelationSize `json:"tables"`
	// Locks are the sessions waiting on a lock, with what they wait for.
	Locks []LockWait `json:"locks"`
	// LongRunning are sessions in a statement for more than a second.
	LongRunning []Activity `json:"long_running"`
}

// ClientUse is one application's connections in one state.
type ClientUse struct {
	Application string `json:"application"`
	State       string `json:"state"`
	Count       int    `json:"count"`
}

// RelationSize is a table's size.
type RelationSize struct {
	Schema  string `json:"schema"`
	Name    string `json:"name"`
	Total   int64  `json:"total"`
	Table   int64  `json:"table"`
	Indexes int64  `json:"indexes"`
	Toast   int64  `json:"toast"`
	// Rows is the planner's estimate.
	Rows int64 `json:"rows"`
	// SeqScans and IndexScans since the statistics reset; DeadRows waits
	// for vacuum.
	SeqScans   int64      `json:"seq_scans"`
	IndexScans int64      `json:"index_scans"`
	DeadRows   int64      `json:"dead_rows"`
	LastVacuum *time.Time `json:"last_vacuum"`
}

// LockWait is a session waiting for a lock another session holds.
type LockWait struct {
	PID         int    `json:"pid"`
	Application string `json:"application"`
	Waiting     string `json:"waiting"` // the waiting statement
	LockType    string `json:"lock_type"`
	Mode        string `json:"mode"`
	Relation    string `json:"relation,omitempty"`
	BlockedBy   []int  `json:"blocked_by"`
	WaitingFor  string `json:"waiting_for_ms"`
}

// Activity is one session's current statement.
type Activity struct {
	PID         int     `json:"pid"`
	Application string  `json:"application"`
	State       string  `json:"state"`
	Query       string  `json:"query"`
	DurationMS  float64 `json:"duration_ms"`
	WaitEvent   string  `json:"wait_event,omitempty"`
}

// Stats reads the database's statistics.
func (c *Client) Stats(ctx context.Context) (DatabaseStats, error) {
	var st DatabaseStats
	err := c.readOnly(ctx, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `SELECT current_database(), version(), pg_database_size(current_database()),
			current_setting('max_connections')::int,
			(SELECT count(*) FROM pg_stat_activity WHERE backend_type = 'client backend'),
			coalesce(d.blks_hit::float8 / nullif(d.blks_hit + d.blks_read, 0), 0),
			d.xact_commit, d.xact_rollback, d.deadlocks, d.temp_bytes, d.stats_reset
			FROM pg_stat_database d WHERE d.datname = current_database()`)
		if err := row.Scan(&st.Database, &st.Version, &st.Size, &st.MaxConnections, &st.Connections, &st.CacheHitRatio,
			&st.Commits, &st.Rollbacks, &st.Deadlocks, &st.TempBytes, &st.StatsSince); err != nil {
			return err
		}
		if i := strings.IndexByte(st.Version, ','); i > 0 {
			st.Version = st.Version[:i] // "PostgreSQL 18.0 (Debian…)" → "PostgreSQL 18.0"
		}
		if err := tx.QueryRow(ctx, `SELECT coalesce(sum(idx_blks_hit)::float8 / nullif(sum(idx_blks_hit + idx_blks_read), 0), 0) FROM pg_statio_user_indexes`).Scan(&st.IndexHitRatio); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT coalesce(application_name, ''), coalesce(state, 'idle'), count(*) FROM pg_stat_activity
			WHERE backend_type = 'client backend' GROUP BY 1, 2 ORDER BY 3 DESC, 1, 2`)
		if err != nil {
			return err
		}
		st.Clients, err = pgx.CollectRows(rows, pgx.RowToStructByPos[ClientUse])
		if err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `SELECT n.nspname, c.relname, pg_total_relation_size(c.oid),
			pg_relation_size(c.oid), pg_indexes_size(c.oid), coalesce(pg_total_relation_size(c.reltoastrelid), 0),
			c.reltuples::bigint, coalesce(s.seq_scan, 0), coalesce(s.idx_scan, 0), coalesce(s.n_dead_tup, 0),
			greatest(s.last_vacuum, s.last_autovacuum)
			FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
			LEFT JOIN pg_stat_user_tables s ON s.relid = c.oid
			WHERE c.relkind IN ('r', 'p', 'm') AND n.nspname NOT IN ('pg_catalog', 'information_schema') AND n.nspname NOT LIKE 'pg_toast%'
			ORDER BY 3 DESC LIMIT 50`)
		if err != nil {
			return err
		}
		st.Tables, err = pgx.CollectRows(rows, pgx.RowToStructByPos[RelationSize])
		if err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `SELECT a.pid, coalesce(a.application_name, ''), left(coalesce(a.query, ''), 2000), l.locktype, l.mode,
			coalesce(l.relation::regclass::text, ''), coalesce(pg_blocking_pids(a.pid), '{}'),
			(extract(epoch FROM clock_timestamp() - a.query_start) * 1000)::bigint::text
			FROM pg_stat_activity a JOIN pg_locks l ON l.pid = a.pid AND NOT l.granted
			WHERE a.backend_type = 'client backend' ORDER BY a.query_start`)
		if err != nil {
			return err
		}
		st.Locks, err = pgx.CollectRows(rows, pgx.RowToStructByPos[LockWait])
		if err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `SELECT pid, coalesce(application_name, ''), coalesce(state, ''), left(coalesce(query, ''), 2000),
			extract(epoch FROM clock_timestamp() - query_start) * 1000, coalesce(wait_event, '')
			FROM pg_stat_activity WHERE backend_type = 'client backend' AND state <> 'idle' AND pid <> pg_backend_pid()
			AND query_start < clock_timestamp() - interval '1 second' ORDER BY query_start`)
		if err != nil {
			return err
		}
		st.LongRunning, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Activity])
		return err
	})
	if st.Clients == nil {
		st.Clients = []ClientUse{}
	}
	if st.Tables == nil {
		st.Tables = []RelationSize{}
	}
	if st.Locks == nil {
		st.Locks = []LockWait{}
	}
	if st.LongRunning == nil {
		st.LongRunning = []Activity{}
	}
	return st, err
}

// Statement is one entry of pg_stat_statements.
type Statement struct {
	QueryID int64  `json:"query_id"`
	Query   string `json:"query"`
	Calls   int64  `json:"calls"`
	// Times in milliseconds.
	TotalMS  float64 `json:"total_ms"`
	MeanMS   float64 `json:"mean_ms"`
	MinMS    float64 `json:"min_ms"`
	MaxMS    float64 `json:"max_ms"`
	StddevMS float64 `json:"stddev_ms"`
	Rows     int64   `json:"rows"`
	// HitRatio is shared buffer hits over reads for the statement.
	HitRatio float64 `json:"hit_ratio"`
	// TotalShare is the statement's share of every statement's total time.
	TotalShare float64 `json:"total_share"`
}

// Statements is the answer to a statements request.
type Statements struct {
	// Available reports pg_stat_statements loaded; Reason says why not.
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
	// Since is when the statistics were last reset.
	Since      *time.Time  `json:"since"`
	Statements []Statement `json:"statements"`
	Sort       string      `json:"sort"`
}

// Statement sorts.
var statementSorts = map[string]string{
	"total_time": "total_exec_time DESC",
	"mean_time":  "mean_exec_time DESC",
	"calls":      "calls DESC",
	"rows":       "rows DESC",
	"max_time":   "max_exec_time DESC",
}

// StatementsAvailable reports whether pg_stat_statements is loaded and
// installed, installing the extension when the server preloads it.
func (c *Client) statementsAvailable(ctx context.Context) (bool, string) {
	var preload string
	if err := c.pool.QueryRow(ctx, `SELECT current_setting('shared_preload_libraries', true)`).Scan(&preload); err != nil {
		return false, err.Error()
	}
	if !strings.Contains(preload, "pg_stat_statements") {
		return false, "pg_stat_statements isn't in shared_preload_libraries: add `command: [\"postgres\", \"-c\", \"shared_preload_libraries=pg_stat_statements\"]` to the postgres service in compose.yaml and recreate the container"
	}
	var installed bool
	if err := c.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_stat_statements')`).Scan(&installed); err != nil {
		return false, err.Error()
	}
	if !installed {
		if _, err := c.pool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pg_stat_statements`); err != nil {
			return false, "pg_stat_statements is loaded but not installed, and creating it failed: " + err.Error()
		}
	}
	return true, ""
}

// Statements reads pg_stat_statements for the current database, sorted
// by sort (total_time, mean_time, calls, rows, max_time), at most limit.
func (c *Client) Statements(ctx context.Context, sort string, limit int) (Statements, error) {
	order, ok := statementSorts[sort]
	if sort == "" {
		sort, order, ok = "total_time", statementSorts["total_time"], true
	}
	if !ok {
		return Statements{}, fmt.Errorf("unknown sort %q", sort)
	}
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	out := Statements{Sort: sort, Statements: []Statement{}}
	available, reason := c.statementsAvailable(ctx)
	if !available {
		out.Reason = reason
		return out, nil
	}
	out.Available = true
	err := c.readOnly(ctx, func(tx pgx.Tx) error {
		var total float64
		if err := tx.QueryRow(ctx, `SELECT coalesce(sum(total_exec_time), 0) FROM pg_stat_statements WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database())`).Scan(&total); err != nil {
			return err
		}
		_ = tx.QueryRow(ctx, `SELECT stats_reset FROM pg_stat_statements_info`).Scan(&out.Since)
		rows, err := tx.Query(ctx, `SELECT queryid, left(query, 4000), calls, total_exec_time, mean_exec_time, min_exec_time, max_exec_time, stddev_exec_time, rows,
			coalesce(shared_blks_hit::float8 / nullif(shared_blks_hit + shared_blks_read, 0), 0)
			FROM pg_stat_statements WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database())
			AND query NOT LIKE '%pg_stat_statements%' AND query NOT LIKE '%pg_catalog.%' ORDER BY `+order+` LIMIT $1`, limit)
		if err != nil {
			return err
		}
		for rows.Next() {
			var s Statement
			if err := rows.Scan(&s.QueryID, &s.Query, &s.Calls, &s.TotalMS, &s.MeanMS, &s.MinMS, &s.MaxMS, &s.StddevMS, &s.Rows, &s.HitRatio); err != nil {
				return err
			}
			if total > 0 {
				s.TotalShare = s.TotalMS / total
			}
			out.Statements = append(out.Statements, s)
		}
		return rows.Err()
	})
	return out, err
}

// ResetStatements forgets pg_stat_statements' counters.
func (c *Client) ResetStatements(ctx context.Context) error {
	if available, reason := c.statementsAvailable(ctx); !available {
		return errors.New(reason)
	}
	_, err := c.pool.Exec(ctx, `SELECT pg_stat_statements_reset()`)
	return err
}

// Advice is what the catalog and statistics suggest.
type Advice struct {
	// MissingFKIndexes are foreign keys whose referencing columns have no
	// index: deletes and updates on the referenced table scan them.
	MissingFKIndexes []IndexAdvice `json:"missing_fk_indexes"`
	// UnusedIndexes were never scanned since the statistics reset and
	// aren't unique or primary keys.
	UnusedIndexes []IndexAdvice `json:"unused_indexes"`
	// SeqScanned are tables with many rows read mostly by sequential scans.
	SeqScanned []IndexAdvice `json:"seq_scanned"`
	// DeadRows are tables waiting for a vacuum.
	DeadRows []IndexAdvice `json:"dead_rows"`
}

// IndexAdvice is one suggestion.
type IndexAdvice struct {
	Schema  string   `json:"schema"`
	Table   string   `json:"table"`
	Index   string   `json:"index,omitempty"`
	Columns []string `json:"columns,omitempty"`
	// Reason says why, with the numbers; SQL is what to run, when there
	// is something to run.
	Reason string `json:"reason"`
	SQL    string `json:"sql,omitempty"`
	Size   int64  `json:"size,omitempty"`
}

// Advise reads the statistics for index suggestions.
func (c *Client) Advise(ctx context.Context) (Advice, error) {
	adv := Advice{MissingFKIndexes: []IndexAdvice{}, UnusedIndexes: []IndexAdvice{}, SeqScanned: []IndexAdvice{}, DeadRows: []IndexAdvice{}}
	err := c.readOnly(ctx, func(tx pgx.Tx) error {
		// Foreign keys without an index whose leading columns are the key's.
		rows, err := tx.Query(ctx, `SELECT n.nspname, c.relname, con.conname,
			ARRAY(SELECT a.attname FROM unnest(con.conkey) WITH ORDINALITY k(attnum, ord) JOIN pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = k.attnum ORDER BY k.ord)
			FROM pg_constraint con JOIN pg_class c ON c.oid = con.conrelid JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE con.contype = 'f' AND n.nspname NOT IN ('pg_catalog', 'information_schema')
			AND NOT EXISTS (SELECT 1 FROM pg_index i WHERE i.indrelid = con.conrelid AND (i.indkey::int2[])[0:array_length(con.conkey, 1) - 1] = con.conkey::int2[])
			ORDER BY 1, 2, 3`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var a IndexAdvice
			var con string
			if err := rows.Scan(&a.Schema, &a.Table, &con, &a.Columns); err != nil {
				return err
			}
			a.Index = fmt.Sprintf("%s_%s_idx", a.Table, strings.Join(a.Columns, "_"))
			a.Reason = fmt.Sprintf("foreign key %s has no index on (%s): deletes and updates on the referenced table scan %s", con, strings.Join(a.Columns, ", "), a.Table)
			a.SQL = fmt.Sprintf("CREATE INDEX %s ON %s (%s);", ident(a.Index), ident(a.Schema, a.Table), joinIdents(a.Columns))
			adv.MissingFKIndexes = append(adv.MissingFKIndexes, a)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `SELECT s.schemaname, s.relname, s.indexrelname, pg_relation_size(s.indexrelid)
			FROM pg_stat_user_indexes s JOIN pg_index i ON i.indexrelid = s.indexrelid
			WHERE s.idx_scan = 0 AND NOT i.indisunique AND NOT i.indisprimary AND pg_relation_size(s.indexrelid) > 8192
			ORDER BY 4 DESC LIMIT 50`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var a IndexAdvice
			if err := rows.Scan(&a.Schema, &a.Table, &a.Index, &a.Size); err != nil {
				return err
			}
			a.Reason = "never scanned since the statistics were reset; if that stays true under real traffic, drop it"
			a.SQL = fmt.Sprintf("DROP INDEX %s;", ident(a.Schema, a.Index))
			adv.UnusedIndexes = append(adv.UnusedIndexes, a)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `SELECT schemaname, relname, seq_scan, seq_tup_read, coalesce(idx_scan, 0), n_live_tup
			FROM pg_stat_user_tables WHERE n_live_tup > 1000 AND seq_scan > 10 AND seq_tup_read > 10 * n_live_tup
			AND seq_scan > 5 * coalesce(idx_scan, 0) ORDER BY seq_tup_read DESC LIMIT 20`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var a IndexAdvice
			var seq, read, idx, live int64
			if err := rows.Scan(&a.Schema, &a.Table, &seq, &read, &idx, &live); err != nil {
				return err
			}
			a.Reason = fmt.Sprintf("%d sequential scans read %d rows of a %d-row table (%d index scans): the queries on it may need an index on the columns they filter by", seq, read, live, idx)
			adv.SeqScanned = append(adv.SeqScanned, a)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `SELECT schemaname, relname, n_dead_tup, n_live_tup FROM pg_stat_user_tables
			WHERE n_dead_tup > 1000 AND n_dead_tup > n_live_tup / 5 ORDER BY n_dead_tup DESC LIMIT 20`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var a IndexAdvice
			var dead, live int64
			if err := rows.Scan(&a.Schema, &a.Table, &dead, &live); err != nil {
				return err
			}
			a.Reason = fmt.Sprintf("%d dead rows against %d live: autovacuum hasn't caught up", dead, live)
			a.SQL = fmt.Sprintf("VACUUM ANALYZE %s;", ident(a.Schema, a.Table))
			adv.DeadRows = append(adv.DeadRows, a)
		}
		return rows.Err()
	})
	return adv, err
}

func joinIdents(cols []string) string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = ident(c)
	}
	return strings.Join(out, ", ")
}
