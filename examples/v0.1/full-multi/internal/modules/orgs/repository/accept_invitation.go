package repository

import (
	"context"
	"time"
)

const acceptInvitationSQL = `UPDATE org_invitations SET accepted_at = $2 WHERE id = $1 AND accepted_at IS NULL AND revoked_at IS NULL`

// AcceptInvitation marks an invitation accepted.
func (s *Store) AcceptInvitation(ctx context.Context, id string, now time.Time) error {
	_, err := s.db.Exec(ctx, acceptInvitationSQL, id, now)
	return err
}
