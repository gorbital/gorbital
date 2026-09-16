package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// RowLevelSecurityReport describes row-level security in the current schema
// for the role the connection runs as, so apps can warn at startup and in
// orb doctor when policies exist but don't apply (ADR-0061).
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

// On reports whether any table has row-level security enabled.
func (r RowLevelSecurityReport) On() bool {
	return len(r.Forced) > 0 || len(r.NotForced) > 0
}

const selectRoleBypassSQL = `
	SELECT current_user::text, rolsuper OR rolbypassrls FROM pg_roles WHERE rolname = current_user`

// selectRowLevelSecuritySQL lists the current schema's tables that have
// row-level security on or a NOT NULL org_id column.
const selectRowLevelSecuritySQL = `
	SELECT c.relname::text, c.relrowsecurity, c.relforcerowsecurity
	FROM pg_class c
	WHERE c.relnamespace = current_schema()::regnamespace AND c.relkind IN ('r', 'p')
	  AND (c.relrowsecurity OR EXISTS (
		SELECT 1 FROM pg_attribute a
		WHERE a.attrelid = c.oid AND a.attname = 'org_id' AND a.attnotnull AND NOT a.attisdropped))
	ORDER BY c.relname`

// CheckRowLevelSecurity reports row-level security in the current schema as
// the role db connects with. It changes nothing.
func CheckRowLevelSecurity(ctx context.Context, db DBTX) (RowLevelSecurityReport, error) {
	var r RowLevelSecurityReport
	if err := db.QueryRow(ctx, selectRoleBypassSQL).Scan(&r.Role, &r.Bypasses); err != nil {
		return RowLevelSecurityReport{}, fmt.Errorf("postgres: read the role: %w", err)
	}
	rows, err := db.Query(ctx, selectRowLevelSecuritySQL)
	if err != nil {
		return RowLevelSecurityReport{}, fmt.Errorf("postgres: read row-level security: %w", err)
	}
	var (
		table           string
		enabled, forced bool
	)
	_, err = pgx.ForEachRow(rows, []any{&table, &enabled, &forced}, func() error {
		switch {
		case forced && enabled:
			r.Forced = append(r.Forced, table)
		case enabled:
			r.NotForced = append(r.NotForced, table)
		default:
			r.Unprotected = append(r.Unprotected, table)
		}
		return nil
	})
	if err != nil {
		return RowLevelSecurityReport{}, fmt.Errorf("postgres: read row-level security: %w", err)
	}
	return r, nil
}
