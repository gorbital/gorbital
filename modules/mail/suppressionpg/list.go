package suppressionpg

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"
)

const suppressionColumns = `id, email, reason, source, detail, created_at, updated_at`

// fields are the scan targets for suppressionColumns.
func (s *Suppression) fields() []any {
	return []any{&s.ID, &s.Email, &s.Reason, &s.Source, &s.Detail, &s.CreatedAt, &s.UpdatedAt}
}

func (s Suppression) utc() Suppression {
	s.CreatedAt, s.UpdatedAt = s.CreatedAt.UTC(), s.UpdatedAt.UTC()
	return s
}

func scanSuppression(row pgx.CollectableRow) (Suppression, error) {
	var s Suppression
	err := row.Scan(s.fields()...)
	return s.utc(), err
}

// Filter selects suppressions. Empty fields match everything.
type Filter struct {
	Reason Reason
	// Limit is clamped to 1–100; 0 means 50.
	Limit int
	// Cursor is Page.NextCursor from the previous page.
	Cursor string
}

// Page is a page of suppressions, newest first.
type Page struct {
	Suppressions []Suppression
	// NextCursor fetches the next page; empty on the last page.
	NextCursor string
}

const selectSuppressionsSQL = `SELECT ` + suppressionColumns + ` FROM mail_suppressions`

// List returns suppressions matching f, most recently added first. It
// returns [ErrInvalidCursor] for a cursor it didn't return.
func (s *Store) List(ctx context.Context, f Filter) (Page, error) {
	limit := f.Limit
	switch {
	case limit <= 0:
		limit = 50
	case limit > 100:
		limit = 100
	}
	var (
		where []string
		args  []any
	)
	add := func(condition string, arg any) {
		args = append(args, arg)
		where = append(where, fmt.Sprintf(condition, len(args)))
	}
	if f.Reason != "" {
		add("reason = $%d", string(f.Reason))
	}
	if f.Cursor != "" {
		id, err := strconv.ParseInt(f.Cursor, 10, 64)
		if err != nil || id < 1 {
			return Page{}, ErrInvalidCursor
		}
		add("id < $%d", id)
	}
	sql := selectSuppressionsSQL
	if len(where) > 0 {
		sql += " WHERE " + strings.Join(where, " AND ")
	}
	args = append(args, limit+1)
	sql += fmt.Sprintf(" ORDER BY id DESC LIMIT $%d", len(args))

	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return Page{}, dbError("list suppressions", err)
	}
	list, err := pgx.CollectRows(rows, scanSuppression)
	if err != nil {
		return Page{}, dbError("list suppressions", err)
	}
	page := Page{Suppressions: list}
	if len(list) > limit {
		page.Suppressions = list[:limit]
		page.NextCursor = strconv.FormatInt(page.Suppressions[limit-1].ID, 10)
	}
	return page, nil
}

const deleteSuppressionSQL = `DELETE FROM mail_suppressions WHERE id = $1 RETURNING ` + suppressionColumns

// Remove takes the suppression id off the list and returns it, so the
// address receives email again until its next bounce or complaint. It
// returns [ErrNotFound] for an unknown ID.
func (s *Store) Remove(ctx context.Context, id int64) (Suppression, error) {
	var sup Suppression
	err := s.pool.QueryRow(ctx, deleteSuppressionSQL, id).Scan(sup.fields()...)
	switch {
	case postgres.IsNoRows(err):
		return Suppression{}, ErrNotFound
	case err != nil:
		return Suppression{}, dbError("remove suppression", err)
	}
	return sup.utc(), nil
}
