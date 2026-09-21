package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	authdomain "example.com/shelfie/internal/modules/auth/domain"
)

// Exactly one of $1 (a user) and $2 (a service account) is set; the other
// matches nothing.
//
//nolint:gosec // SQL text, not a credential
const selectAPIKeysSQL = `SELECT ` + apiKeyColumns + ` FROM auth_api_keys
	WHERE user_id = NULLIF($1, '') OR service_account_id = NULLIF($2, '')
	ORDER BY created_at DESC, id`

// SelectAPIKeys returns the API keys of a user or a service account, newest
// first, including expired and revoked ones cleanup hasn't removed yet.
func (s *Store) SelectAPIKeys(ctx context.Context, userID, serviceAccountID string) ([]authdomain.APIKey, error) {
	rows, err := s.db.Query(ctx, selectAPIKeysSQL, userID, serviceAccountID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanAPIKey)
}
