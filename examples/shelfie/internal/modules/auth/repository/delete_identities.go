package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	authdomain "example.com/shelfie/internal/modules/auth/domain"
)

const deleteIdentitiesSQL = `DELETE FROM auth_identities WHERE user_id = $1 RETURNING ` + identityColumns

// DeleteIdentities unlinks every identity of a user and returns them, so
// their refresh tokens can be revoked.
func (s *Store) DeleteIdentities(ctx context.Context, userID string) ([]authdomain.Identity, error) {
	rows, err := s.db.Query(ctx, deleteIdentitiesSQL, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanIdentity)
}
