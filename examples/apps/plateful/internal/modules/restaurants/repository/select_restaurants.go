package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/page"

	"example.com/plateful/internal/modules/restaurants/domain"
	"example.com/plateful/internal/modules/restaurants/usecase"
)

// selectRestaurantsSQL holds one fixed query per sort, keyed "name",
// "-name" and so on. Only the allowlisted sort expressions below ever become
// SQL text; everything a request sends is a placeholder. Pages use keyset
// pagination: the next page starts after the last row's (sort value, id).
var selectRestaurantsSQL = func() map[string]string {
	sorts := []struct{ field, key, after string }{
		// Text sorts ignore case and compare byte by byte, so the order
		// doesn't depend on the database's locale.
		{"name", `lower(name) COLLATE "C"`, `lower($2::text) COLLATE "C"`},
		{"created_at", `profile_created_at`, `$2::timestamptz`},
	}
	queries := make(map[string]string, 2*len(sorts))
	for _, s := range sorts {
		for _, desc := range []bool{false, true} {
			name, dir, cmp := s.field, "ASC", ">"
			if desc {
				name, dir, cmp = "-"+s.field, "DESC", "<"
			}
			queries[name] = `
	SELECT ` + restaurantColumns + ` FROM orgs
	WHERE ` + isRestaurant + `
	  AND (cardinality($5::text[]) = 0 OR status = ANY($5))
	  AND ($6::text = '' OR lower(cuisine) = lower($6))
	  AND (NOT $1::boolean OR (` + s.key + `, id) ` + cmp + ` (` + s.after + `, $3))
	ORDER BY ` + s.key + ` ` + dir + `, id ` + dir + `
	LIMIT $4`
		}
	}
	return queries
}()

// SelectRestaurants returns up to q.Limit restaurants in q.Sort order,
// starting after q.After.
func (s *Store) SelectRestaurants(ctx context.Context, q usecase.BrowseQuery) ([]domain.Restaurant, error) {
	name := q.Sort.Field
	if q.Sort.Desc {
		name = "-" + name
	}
	sql, ok := selectRestaurantsSQL[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q is not sortable", page.ErrInvalidSort, q.Sort.Field)
	}
	// The unused side of the comparison still has to parse, so a first page
	// passes a value of the right shape rather than an empty string.
	after, afterID := "", ""
	if q.Sort.Field == "created_at" {
		after = "0001-01-01T00:00:00Z"
	}
	if q.After != nil {
		afterID = q.After.ID
		if after = q.After.Text; q.Sort.Field == "created_at" {
			after = q.After.Time
		}
	}
	statuses := make([]string, len(q.Statuses))
	for i, st := range q.Statuses {
		statuses[i] = string(st)
	}
	rows, err := s.db.Query(ctx, sql, q.After != nil, after, afterID, q.Limit, statuses, q.Cuisine)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanRestaurant)
}
