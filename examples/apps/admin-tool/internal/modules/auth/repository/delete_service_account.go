package repository

import "context"

// Its API keys go with it (ON DELETE CASCADE).
const deleteServiceAccountSQL = `DELETE FROM auth_service_accounts WHERE id = $1 AND org_id IS NOT DISTINCT FROM NULLIF($2, '')`

// DeleteServiceAccount removes a service account of orgID with its API keys
// and reports whether it existed.
func (s *Store) DeleteServiceAccount(ctx context.Context, orgID, id string) (bool, error) {
	tag, err := s.db.Exec(ctx, deleteServiceAccountSQL, id, orgID)
	return tag.RowsAffected() == 1, err
}
