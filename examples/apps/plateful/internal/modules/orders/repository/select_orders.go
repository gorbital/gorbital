package repository

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/page"

	"example.com/plateful/internal/modules/orders/domain"
	"example.com/plateful/internal/modules/orders/usecase"
)

// docs:start list-orders-sql

// selectOrdersSQL holds one fixed query per sort, keyed "placed_at",
// "-placed_at" and so on. Only the allowlisted sort expressions below ever
// become SQL text; everything a request sends is a placeholder.
//
// The three owner filters are $1 to $3, and every query carries all three.
// Exactly one is ever set, by the use case that built the query, which is
// what makes a row belong to this caller: one restaurant's orders, one
// customer's, or one courier's. They are in the WHERE clause of every page,
// never inside the cursor, because a cursor is opaque but not signed.
//
// The keyset value is one text parameter cast per sort. Giving each sort its
// own typed placeholder looks tidier and doesn't work: a placeholder that
// never appears in the statement has no type, and PostgreSQL refuses the
// whole query at run time with "could not determine data type".
var selectOrdersSQL = func() map[string]string {
	sorts := []struct{ field, key, after string }{
		{"placed_at", `placed_at`, `$5::timestamptz`},
		{"total_minor", `total_minor`, `$5::bigint`},
	}
	queries := make(map[string]string, 2*len(sorts))
	for _, s := range sorts {
		for _, desc := range []bool{false, true} {
			name, dir, cmp := s.field, "ASC", ">"
			if desc {
				name, dir, cmp = "-"+s.field, "DESC", "<"
			}
			queries[name] = `
	SELECT ` + orderColumns + ` FROM orders
	WHERE ($1::text = '' OR org_id = $1)
	  AND ($2::text = '' OR customer_id = $2)
	  AND ($3::text = '' OR courier_id = $3)
	  AND ($8::text = '' OR status = $8)
	  AND ($9::timestamptz IS NULL OR placed_at >= $9)
	  AND ($10::timestamptz IS NULL OR placed_at < $10)
	  AND (NOT $4::boolean OR (` + s.key + `, id) ` + cmp + ` (` + s.after + `, $6))
	ORDER BY ` + s.key + ` ` + dir + `, id ` + dir + `
	LIMIT $7`
		}
	}
	return queries
}()

// docs:end list-orders-sql

// SelectOrders returns up to q.Limit orders in q.Sort order, starting after
// q.After.
func (s *Store) SelectOrders(ctx context.Context, q usecase.ListQuery) ([]domain.Order, error) {
	name := q.Sort.Field
	if q.Sort.Desc {
		name = "-" + name
	}
	sql, ok := selectOrdersSQL[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q is not sortable", page.ErrInvalidSort, q.Sort.Field)
	}
	after, afterID := zeroAfter(q.Sort.Field), ""
	if q.After != nil {
		afterID = q.After.ID
		if after = strconv.FormatInt(q.After.Total, 10); q.Sort.Field == "placed_at" {
			after = q.After.Time.Format(time.RFC3339Nano)
		}
	}
	// When a restaurant's list filters by courier, the courier filter is the
	// narrowing one and the organisation still bounds it: both are set.
	courier := q.CourierID
	rows, err := s.db.Query(ctx, sql,
		q.OrgID, q.CustomerID, courier, q.After != nil, after, afterID, q.Limit,
		string(q.Status), nullTime(q.PlacedFrom), nullTime(q.PlacedBefore))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanOrder)
}

// zeroAfter is a value of the right shape for the first page, where the
// comparison is switched off but still has to parse.
func zeroAfter(field string) string {
	if field == "placed_at" {
		return "0001-01-01T00:00:00Z"
	}
	return "0"
}
