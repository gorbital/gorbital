package repository

import (
	"context"

	authdomain "example.com/shelfie/internal/modules/auth/domain"
)

const insertServiceAccountSQL = `
	INSERT INTO auth_service_accounts (id, org_id, name, description, roles, created_by, created_at, updated_at)
	VALUES ($1, NULLIF($2, ''), $3, $4, $5, $6, $7, $7)`

// InsertServiceAccount creates a service account.
func (s *Store) InsertServiceAccount(ctx context.Context, a authdomain.ServiceAccount) error {
	_, err := s.db.Exec(ctx, insertServiceAccountSQL, a.ID, a.OrgID, a.Name, a.Description, a.Roles, a.CreatedBy, a.CreatedAt)
	return err
}
