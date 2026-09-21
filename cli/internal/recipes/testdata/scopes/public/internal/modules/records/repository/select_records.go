package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/page"

	"example.com/app/internal/modules/records/domain"
	"example.com/app/internal/modules/records/usecase"
)

// selectRecordsSQL holds one fixed query per sort, keyed "created_at",
// "-created_at" and so on. Only the allowlisted sort expressions below become
// SQL text; every value is a placeholder. Pages use keyset
// pagination: the next page starts after the last row's (sort value, id).
var selectRecordsSQL = func() map[string]string {
	sorts := []struct{ field, key, after string }{
		{"created_at", `created_at`, `$2::timestamptz`},
		{"updated_at", `updated_at`, `$2::timestamptz`},
		// Text sorts ignore case and compare byte by byte, so the order
		// doesn't depend on the database's locale.
		{"title", `lower(title) COLLATE "C"`, `lower($2::text) COLLATE "C"`},
	}
	queries := make(map[string]string, 2*len(sorts))
	for _, s := range sorts {
		for _, desc := range []bool{false, true} {
			name, dir, cmp := s.field, "ASC", ">"
			if desc {
				name, dir, cmp = "-"+s.field, "DESC", "<"
			}
			queries[name] = `
	SELECT ` + recordColumns + ` FROM records
	WHERE TRUE
	  AND ($5::text = '' OR state = $5)
	  AND (NOT $1::boolean OR (` + s.key + `, id) ` + cmp + ` (` + s.after + `, $3))
	ORDER BY ` + s.key + ` ` + dir + `, id ` + dir + `
	LIMIT $4`
		}
	}
	return queries
}()

// SelectRecords returns up to q.Limit of the records in q.Sort
// order, starting after q.After.
func (s *Store) SelectRecords(ctx context.Context, q usecase.ListQuery) ([]domain.Record, error) {
	name := q.Sort.Field
	if q.Sort.Desc {
		name = "-" + name
	}
	sql, ok := selectRecordsSQL[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q is not sortable", page.ErrInvalidSort, q.Sort.Field)
	}
	byTime := q.Sort.Field == "created_at" || q.Sort.Field == "updated_at"
	var after any
	switch {
	case q.After != nil && byTime:
		after = q.After.Time
	case q.After != nil:
		after = q.After.Text
	case byTime:
		after = time.Time{}
	default:
		after = ""
	}
	var afterID string
	if q.After != nil {
		afterID = q.After.ID
	}
	rows, err := s.db.Query(ctx, sql, q.After != nil, after, afterID, q.Limit, q.State)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanRecord)
}
