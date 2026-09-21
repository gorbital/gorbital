package repository

import (
	"context"
	"time"
)

const revokeInvitationSQL = `UPDATE org_invitations SET revoked_at = $2 WHERE id = $1 AND accepted_at IS NULL AND revoked_at IS NULL`

// RevokeInvitation marks an invitation revoked.
func (s *Store) RevokeInvitation(ctx context.Context, id string, now time.Time) error {
	_, err := s.db.Exec(ctx, revokeInvitationSQL, id, now)
	return err
}
