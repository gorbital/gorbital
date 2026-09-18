package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	authdomain "example.com/invoicing/internal/modules/auth/domain"
)

// Two cleanups at once record each key once (SKIP LOCKED).
//
//nolint:gosec // SQL text, not a credential
const markAPIKeysExpiredSQL = `
	UPDATE auth_api_keys SET expiry_recorded_at = $1
	WHERE id IN (
		SELECT id FROM auth_api_keys
		WHERE expires_at <= $1 AND revoked_at IS NULL AND expiry_recorded_at IS NULL
		ORDER BY expires_at LIMIT $2 FOR UPDATE SKIP LOCKED
	)
	RETURNING ` + apiKeyColumns

// MarkAPIKeysExpired marks up to limit keys that expired by now, and weren't
// revoked or marked before, and returns them.
func (s *Store) MarkAPIKeysExpired(ctx context.Context, now time.Time, limit int) ([]authdomain.APIKey, error) {
	rows, err := s.db.Query(ctx, markAPIKeysExpiredSQL, now, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanAPIKey)
}
