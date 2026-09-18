package repository

import (
	"context"

	authdomain "example.com/invoicing/internal/modules/auth/domain"
)

const updateServiceAccountSQL = `
	UPDATE auth_service_accounts SET name = $3, description = $4, roles = $5, disabled_at = $6, updated_at = $7
	WHERE id = $1 AND org_id IS NOT DISTINCT FROM NULLIF($2, '')`

// UpdateServiceAccount stores a service account's name, description, roles,
// disabled time and update time.
func (s *Store) UpdateServiceAccount(ctx context.Context, a authdomain.ServiceAccount) error {
	_, err := s.db.Exec(ctx, updateServiceAccountSQL, a.ID, a.OrgID, a.Name, a.Description, a.Roles, a.DisabledAt, a.UpdatedAt)
	return err
}
