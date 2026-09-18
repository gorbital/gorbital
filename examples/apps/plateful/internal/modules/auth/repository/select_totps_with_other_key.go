package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	authdomain "example.com/plateful/internal/modules/auth/domain"
)

const selectTOTPsWithOtherKeySQL = `SELECT ` + totpColumns + ` FROM auth_totp WHERE key_id <> $1 ORDER BY user_id LIMIT $2`

// SelectTOTPsWithOtherKey returns up to limit secrets encrypted with a key
// other than keyID, for re-encrypting them with it.
func (s *Store) SelectTOTPsWithOtherKey(ctx context.Context, keyID string, limit int) ([]authdomain.TOTP, error) {
	rows, err := s.db.Query(ctx, selectTOTPsWithOtherKeySQL, keyID, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanTOTP)
}
