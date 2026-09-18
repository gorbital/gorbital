package repository

import (
	"context"

	"gorbital.dev/modules/postgres"

	orgsdomain "example.com/plateful/internal/modules/orgs/domain"
)

const selectUserEmailSQL = `
	SELECT email, email_normalized, email_verified_at IS NOT NULL
	FROM auth_users WHERE id = $1 AND deleted_at IS NULL`

// SelectUserEmail returns an account's email address and whether it is
// verified, or ErrMemberNotFound when there is no live account.
func (s *Store) SelectUserEmail(ctx context.Context, userID string) (email, normalized string, verified bool, err error) {
	err = s.db.QueryRow(ctx, selectUserEmailSQL, userID).Scan(&email, &normalized, &verified)
	if postgres.IsNoRows(err) {
		return "", "", false, orgsdomain.ErrMemberNotFound
	}
	return email, normalized, verified, err
}
