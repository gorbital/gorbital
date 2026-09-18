package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/page"

	"example.com/plateful/internal/modules/menus/domain"
	"example.com/plateful/internal/modules/menus/usecase"
)

// docs:start select-items-sql

// selectItemsSQL holds one fixed query per sort, keyed "name", "-name" and
// so on. Only the allowlisted sort expressions below ever become SQL text;
// everything a request sends is a placeholder, so no sort, filter or cursor
// can be written into the statement. Pages use keyset pagination: the next
// page starts after the last row's (sort value, id).
var selectItemsSQL = func() map[string]string {
	sorts := []struct{ field, key, after string }{
		// Text sorts ignore case and compare byte by byte, so the order
		// doesn't depend on the database's locale.
		{"name", `lower(name) COLLATE "C"`, `lower($3::text) COLLATE "C"`},
		// The price sorts as the integer it is stored as; comparing minor
		// units never has to worry about a currency's decimal places.
		{"price_minor", `price_minor`, `$3::bigint`},
		{"created_at", `created_at`, `$3::timestamptz`},
	}
	queries := make(map[string]string, 2*len(sorts))
	for _, s := range sorts {
		for _, desc := range []bool{false, true} {
			name, dir, cmp := s.field, "ASC", ">"
			if desc {
				name, dir, cmp = "-"+s.field, "DESC", "<"
			}
			queries[name] = `
	SELECT ` + itemColumns + ` FROM menu_items
	WHERE org_id = $1
	  AND ($5::text = '' OR lower(section) = lower($5))
	  AND ($6::boolean IS NULL OR available = $6)
	  AND (NOT $2::boolean OR (` + s.key + `, id) ` + cmp + ` (` + s.after + `, $4))
	ORDER BY ` + s.key + ` ` + dir + `, id ` + dir + `
	LIMIT $7`
		}
	}
	return queries
}()

// docs:end select-items-sql

// SelectItems returns up to q.Limit of an organisation's menu items in
// q.Sort order, starting after q.After.
func (s *Store) SelectItems(ctx context.Context, q usecase.ListQuery) ([]domain.Item, error) {
	name := q.Sort.Field
	if q.Sort.Desc {
		name = "-" + name
	}
	sql, ok := selectItemsSQL[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q is not sortable", page.ErrInvalidSort, q.Sort.Field)
	}
	// One parameter carries the cursor's sort value for every sort, cast in
	// the query to the type that sort compares. A first page still has to
	// pass a value of that type, because the comparison has to parse even
	// though $2 switches it off.
	after, afterID := sortValue(q), ""
	if q.After != nil {
		afterID = q.After.ID
	}
	rows, err := s.db.Query(ctx, sql, q.OrgID, q.After != nil,
		after, afterID, q.Section, q.Available, q.Limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanItem)
}

// sortValue is the cursor's position in the type the sort compares, and the
// zero of that type when there is no cursor.
func sortValue(q usecase.ListQuery) any {
	var position usecase.Position
	if q.After != nil {
		position = *q.After
	}
	switch q.Sort.Field {
	case "price_minor":
		return position.Number
	case "created_at":
		return position.Time
	default:
		return position.Text
	}
}
