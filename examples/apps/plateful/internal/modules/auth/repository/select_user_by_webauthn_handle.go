package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	authdomain "example.com/plateful/internal/modules/auth/domain"
)

const selectUserByWebAuthnHandleSQL = `SELECT ` + userColumns + ` FROM auth_users WHERE webauthn_user_handle = $1 AND deleted_at IS NULL`

// SelectUserByWebAuthnHandle returns the account a passkey names, or
// ErrUserNotFound.
func (s *Store) SelectUserByWebAuthnHandle(ctx context.Context, handle []byte) (authdomain.User, error) {
	rows, err := s.db.Query(ctx, selectUserByWebAuthnHandleSQL, handle)
	if err != nil {
		return authdomain.User{}, err
	}
	u, err := pgx.CollectExactlyOneRow(rows, scanUser)
	if errors.Is(err, pgx.ErrNoRows) {
		return authdomain.User{}, authdomain.ErrUserNotFound
	}
	return u, err
}
