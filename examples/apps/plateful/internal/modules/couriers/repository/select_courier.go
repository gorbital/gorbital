package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/couriers/domain"
)

// docs:start select-courier-by-user

// selectCourierByUserSQL reads one courier by the sign-in account behind it.
//
// Compare it with the same query in an org-scoped module, which would read
// "WHERE org_id = $1 AND id = $2": there, the organisation in the WHERE
// clause is a second lock on a row the guard has already checked, and
// forgetting it leaks across tenants. Here user_id is the only clause there
// is, so it is not a second lock but the first and last one. That is the
// price of a table with no tenant: the isolation is one parameter in one
// query, and the use case that passes it.
const selectCourierByUserSQL = `SELECT ` + courierColumns + ` FROM couriers WHERE user_id = $1`

// SelectCourierByUser returns the account's courier profile, or
// ErrCourierNotFound. lock locks the row until the transaction ends, which
// is how an availability change and the orders module's assignment of the
// same courier are kept apart.
func (s *Store) SelectCourierByUser(ctx context.Context, userID string, lock bool) (domain.Courier, error) {
	sql := selectCourierByUserSQL
	if lock {
		sql += ` FOR UPDATE`
	}
	rows, err := s.db.Query(ctx, sql, userID)
	if err != nil {
		return domain.Courier{}, err
	}
	c, err := pgx.CollectExactlyOneRow(rows, scanCourier)
	if postgres.IsNoRows(err) {
		return domain.Courier{}, domain.ErrCourierNotFound
	}
	return c, err
}

// docs:end select-courier-by-user
