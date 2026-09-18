package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	authdomain "example.com/invoicing/internal/modules/auth/domain"
)

const deleteIdentitySQL = `DELETE FROM auth_identities WHERE id = $1 AND user_id = $2 RETURNING ` + identityColumns

// DeleteIdentity unlinks one of a user's identities and returns it, so its
// refresh token can be revoked.
func (s *Store) DeleteIdentity(ctx context.Context, id, userID string) (authdomain.Identity, bool, error) {
	rows, err := s.db.Query(ctx, deleteIdentitySQL, id, userID)
	if err != nil {
		return authdomain.Identity{}, false, err
	}
	i, err := pgx.CollectExactlyOneRow(rows, scanIdentity)
	if errors.Is(err, pgx.ErrNoRows) {
		return authdomain.Identity{}, false, nil
	}
	return i, err == nil, err
}
