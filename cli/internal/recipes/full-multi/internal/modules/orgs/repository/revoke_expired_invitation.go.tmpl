package repository

import (
	"context"
	"time"

	orgslib "gorbital.dev/modules/orgs"
)

const revokeExpiredInvitationSQL = `
	UPDATE org_invitations SET revoked_at = $3
	WHERE org_id = $1 AND normalized_email = $2 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at <= $3`

// RevokeExpiredInvitation revokes the open, expired invitation for an
// address, so a new one can be sent.
func (s *Store) RevokeExpiredInvitation(ctx context.Context, orgID orgslib.ID, normalizedEmail string, now time.Time) error {
	_, err := s.db.Exec(ctx, revokeExpiredInvitationSQL, orgID, normalizedEmail, now)
	return err
}
