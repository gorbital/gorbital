package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	authdomain "example.com/admin-tool/internal/modules/auth/domain"
)

// A handle is set once: later calls keep and return the stored one.
const setWebAuthnUserHandleSQL = `
	UPDATE auth_users SET webauthn_user_handle = COALESCE(webauthn_user_handle, $2)
	WHERE id = $1 AND deleted_at IS NULL
	RETURNING webauthn_user_handle`

// SetWebAuthnUserHandle sets a user's passkey handle unless one is set, and
// returns the stored handle, or ErrUserNotFound.
func (s *Store) SetWebAuthnUserHandle(ctx context.Context, userID string, handle []byte) ([]byte, error) {
	var stored []byte
	err := s.db.QueryRow(ctx, setWebAuthnUserHandleSQL, userID, handle).Scan(&stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, authdomain.ErrUserNotFound
	}
	return stored, err
}
