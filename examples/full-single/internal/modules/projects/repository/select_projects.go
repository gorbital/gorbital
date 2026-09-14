package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"apistock.dev/page"

	projectsdomain "example.com/acme-api/internal/modules/projects/domain"
	projectsusecase "example.com/acme-api/internal/modules/projects/usecase"
)

// selectProjectsSQL holds one fixed query per sort, keyed "name", "-name" and
// so on. Only the allowlisted sort expressions below become SQL text; every
// value is a placeholder (ADR-0032). Pages use keyset pagination: the next
// page starts after the last row's (sort value, id).
var selectProjectsSQL = func() map[string]string {
	sorts := []struct{ field, key, after string }{
		{"created_at", `created_at`, `$4::timestamptz`},
		{"updated_at", `updated_at`, `$4::timestamptz`},
		// Case-insensitive, compared byte by byte so the order doesn't depend
		// on the database's locale.
		{"name", `lower(name) COLLATE "C"`, `lower($4::text) COLLATE "C"`},
	}
	queries := make(map[string]string, 2*len(sorts))
	for _, s := range sorts {
		for _, desc := range []bool{false, true} {
			name, dir, cmp := s.field, "ASC", ">"
			if desc {
				name, dir, cmp = "-"+s.field, "DESC", "<"
			}
			queries[name] = `
	SELECT ` + projectColumns + ` FROM projects
	WHERE owner_id = $1
	  AND ($2::text = '' OR status = $2)
	  AND (NOT $3::boolean OR (` + s.key + `, id) ` + cmp + ` (` + s.after + `, $5))
	ORDER BY ` + s.key + ` ` + dir + `, id ` + dir + `
	LIMIT $6`
		}
	}
	return queries
}()

// SelectProjects returns up to q.Limit of an owner's projects in q.Sort
// order, starting after q.After.
func (s *Store) SelectProjects(ctx context.Context, q projectsusecase.ListQuery) ([]projectsdomain.Project, error) {
	name := q.Sort.Field
	if q.Sort.Desc {
		name = "-" + name
	}
	sql, ok := selectProjectsSQL[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q is not sortable", page.ErrInvalidSort, q.Sort.Field)
	}
	var after any = time.Time{}
	if q.Sort.Field == "name" {
		after = ""
	}
	var afterID string
	if q.After != nil {
		after, afterID = q.After.Time, q.After.ID
		if q.Sort.Field == "name" {
			after = q.After.Name
		}
	}
	rows, err := s.db.Query(ctx, sql, q.OwnerID, string(q.Status), q.After != nil, after, afterID, q.Limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanProject)
}
