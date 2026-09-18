package repository

import (
	"context"
	"time"
)

//nolint:gosec // SQL text, not a credential
const replaceInvitationTokenSQL = `UPDATE org_invitations SET token_hash = $2, invited_by = $3, sent_at = $4, expires_at = $5 WHERE id = $1`

// ReplaceInvitationToken gives an invitation a new token, inviter and
// expiry; the old link stops working.
func (s *Store) ReplaceInvitationToken(ctx context.Context, id string, tokenHash []byte, invitedBy string, now, expiresAt time.Time) error {
	_, err := s.db.Exec(ctx, replaceInvitationTokenSQL, id, tokenHash, invitedBy, now, expiresAt)
	return err
}
