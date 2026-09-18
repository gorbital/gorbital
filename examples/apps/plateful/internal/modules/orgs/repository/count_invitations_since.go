package repository

import (
	"context"
	"time"

	orgslib "gorbital.dev/modules/orgs"
)

const countInvitationsSinceSQL = `SELECT count(*) FROM org_invitations WHERE org_id = $1 AND sent_at > $2`

// CountInvitationsSince counts an organisation's invitations sent or resent
// after since.
func (s *Store) CountInvitationsSince(ctx context.Context, orgID orgslib.ID, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRow(ctx, countInvitationsSinceSQL, orgID, since).Scan(&n)
	return n, err
}
