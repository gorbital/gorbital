package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	authdomain "example.com/plateful/internal/modules/auth/domain"
)

// The organisation must match: a platform service account (empty $2) is
// never found in an organisation, nor one organisation's in another.
const (
	selectServiceAccountSQL       = `SELECT ` + serviceAccountColumns + ` FROM auth_service_accounts WHERE id = $1 AND org_id IS NOT DISTINCT FROM NULLIF($2, '')`
	selectServiceAccountForUpdate = selectServiceAccountSQL + ` FOR UPDATE`
)

// SelectServiceAccount returns a service account of orgID (the platform
// for an empty orgID), or ErrServiceAccountNotFound. lock locks the row
// until the transaction ends.
func (s *Store) SelectServiceAccount(ctx context.Context, orgID, id string, lock bool) (authdomain.ServiceAccount, error) {
	query := selectServiceAccountSQL
	if lock {
		query = selectServiceAccountForUpdate
	}
	rows, err := s.db.Query(ctx, query, id, orgID)
	if err != nil {
		return authdomain.ServiceAccount{}, err
	}
	a, err := pgx.CollectExactlyOneRow(rows, scanServiceAccount)
	if postgres.IsNoRows(err) {
		return authdomain.ServiceAccount{}, authdomain.ErrServiceAccountNotFound
	}
	return a, err
}
