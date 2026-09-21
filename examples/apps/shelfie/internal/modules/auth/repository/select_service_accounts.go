package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	authdomain "example.com/shelfie/internal/modules/auth/domain"
)

// An empty $1 selects the platform's service accounts.
const selectServiceAccountsSQL = `SELECT ` + serviceAccountColumns + ` FROM auth_service_accounts
	WHERE org_id IS NOT DISTINCT FROM NULLIF($1, '') ORDER BY created_at, id`

// SelectServiceAccounts returns the service accounts of an organisation, or
// of the platform for an empty orgID, oldest first.
func (s *Store) SelectServiceAccounts(ctx context.Context, orgID string) ([]authdomain.ServiceAccount, error) {
	rows, err := s.db.Query(ctx, selectServiceAccountsSQL, orgID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanServiceAccount)
}
