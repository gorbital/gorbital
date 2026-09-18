package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	authdomain "example.com/admin-tool/internal/modules/auth/domain"
)

//nolint:gosec // SQL text, not a credential
const selectTokenRevocationsWithOtherKeySQL = `
	SELECT ` + tokenRevocationColumns + ` FROM auth_token_revocations
	WHERE key_id <> $1
	ORDER BY id LIMIT $2`

// SelectTokenRevocationsWithOtherKey returns up to limit queued tokens
// encrypted with a key other than keyID.
func (s *Store) SelectTokenRevocationsWithOtherKey(ctx context.Context, keyID string, limit int) ([]authdomain.TokenRevocation, error) {
	rows, err := s.db.Query(ctx, selectTokenRevocationsWithOtherKeySQL, keyID, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanTokenRevocation)
}
