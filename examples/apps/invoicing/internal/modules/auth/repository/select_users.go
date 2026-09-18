package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	authdomain "example.com/invoicing/internal/modules/auth/domain"
)

// selectUsersSQL lists accounts newest first, after a keyset cursor, with
// an optional search on the address or ID. Deleted accounts are never
// listed; banned ones are (operators need to see them).
const selectUsersSQL = `SELECT ` + userColumns + ` FROM auth_users
WHERE deleted_at IS NULL
  AND ($1 = '' OR email_normalized LIKE '%' || $1 || '%' OR id = $1)
  AND ($2::timestamptz IS NULL OR (created_at, id) < ($2, $3))
ORDER BY created_at DESC, id DESC
LIMIT $4`

// SelectUsers returns up to limit accounts newest first, matching query
// (a substring of the normalized address, or an ID) when it isn't empty,
// after the account created at afterTime with afterID when afterTime is
// set.
func (s *Store) SelectUsers(ctx context.Context, query string, afterTime *time.Time, afterID string, limit int) ([]authdomain.User, error) {
	rows, err := s.db.Query(ctx, selectUsersSQL, query, afterTime, afterID, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanUser)
}
