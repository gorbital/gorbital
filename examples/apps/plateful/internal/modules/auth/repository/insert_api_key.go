package repository

import (
	"context"

	authdomain "example.com/plateful/internal/modules/auth/domain"
)

//nolint:gosec // SQL text, not a credential
const insertAPIKeySQL = `
	INSERT INTO auth_api_keys (id, lookup_id, secret_hash, user_id, service_account_id, name, scopes, expires_at, created_by, created_at)
	VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), $6, $7, $8, $9, $10)`

// InsertAPIKey stores a new API key's hash.
func (s *Store) InsertAPIKey(ctx context.Context, k authdomain.APIKey) error {
	_, err := s.db.Exec(ctx, insertAPIKeySQL, k.ID, k.LookupID, k.SecretHash, k.UserID, k.ServiceAccountID, k.Name, k.Scopes, k.ExpiresAt, k.CreatedBy, k.CreatedAt)
	return err
}
