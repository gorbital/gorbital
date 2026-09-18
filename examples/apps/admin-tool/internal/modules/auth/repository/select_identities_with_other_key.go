package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	authdomain "example.com/admin-tool/internal/modules/auth/domain"
)

const selectIdentitiesWithOtherKeySQL = `
	SELECT ` + identityColumns + ` FROM auth_identities
	WHERE refresh_key_id IS NOT NULL AND refresh_key_id <> $1
	ORDER BY id LIMIT $2`

// SelectIdentitiesWithOtherKey returns up to limit identities whose refresh
// token is encrypted with a key other than keyID.
func (s *Store) SelectIdentitiesWithOtherKey(ctx context.Context, keyID string, limit int) ([]authdomain.Identity, error) {
	rows, err := s.db.Query(ctx, selectIdentitiesWithOtherKeySQL, keyID, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanIdentity)
}
