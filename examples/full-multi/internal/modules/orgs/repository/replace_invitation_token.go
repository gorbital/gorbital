package repository

import (
	"context"
	"time"
)

//nolint:gosec // SQL text, not a credential
const replaceInvitationTokenSQL = `UPDATE org_invitations SET token_hash = $2, sent_at = $3, expires_at = $4 WHERE id = $1`

// ReplaceInvitationToken gives an invitation a new token and expiry; the old
// link stops working.
func (s *Store) ReplaceInvitationToken(ctx context.Context, id string, tokenHash []byte, now, expiresAt time.Time) error {
	_, err := s.db.Exec(ctx, replaceInvitationTokenSQL, id, tokenHash, now, expiresAt)
	return err
}
