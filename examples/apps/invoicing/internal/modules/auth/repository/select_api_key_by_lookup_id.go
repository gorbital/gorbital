package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	authdomain "example.com/invoicing/internal/modules/auth/domain"
)

// Keys of deleted accounts are never returned. A service account's columns
// are empty for a user's key.
//
//nolint:gosec // SQL text, not a credential
const selectAPIKeyByLookupIDSQL = `
	SELECT ` + apiKeyColumnsK + `,
		COALESCE(a.id, ''), COALESCE(a.org_id, ''), COALESCE(a.name, ''), COALESCE(a.description, ''), COALESCE(a.roles, '{}'),
		COALESCE(a.created_by, ''), COALESCE(a.created_at, k.created_at), COALESCE(a.updated_at, k.created_at), a.disabled_at
	FROM auth_api_keys k
	LEFT JOIN auth_users u ON u.id = k.user_id AND u.deleted_at IS NULL
	LEFT JOIN auth_service_accounts a ON a.id = k.service_account_id
	WHERE k.lookup_id = $1 AND (u.id IS NOT NULL OR a.id IS NOT NULL)`

// SelectAPIKeyByLookupID returns the API key with a lookup ID and, for a
// service account's key, the service account; or ErrAPIKeyNotFound.
func (s *Store) SelectAPIKeyByLookupID(ctx context.Context, lookupID string) (authdomain.APIKey, authdomain.ServiceAccount, error) {
	var (
		k authdomain.APIKey
		a authdomain.ServiceAccount
	)
	rows, err := s.db.Query(ctx, selectAPIKeyByLookupIDSQL, lookupID)
	if err != nil {
		return k, a, err
	}
	_, err = pgx.CollectExactlyOneRow(rows, func(row pgx.CollectableRow) (struct{}, error) {
		err := row.Scan(append(apiKeyTargets(&k), &a.ID, &a.OrgID, &a.Name, &a.Description, &a.Roles, &a.CreatedBy, &a.CreatedAt, &a.UpdatedAt, &a.DisabledAt)...)
		return struct{}{}, err
	})
	if postgres.IsNoRows(err) {
		return authdomain.APIKey{}, authdomain.ServiceAccount{}, authdomain.ErrAPIKeyNotFound
	}
	k.ExpiresAt, k.CreatedAt = k.ExpiresAt.UTC(), k.CreatedAt.UTC()
	return k, a, err
}
