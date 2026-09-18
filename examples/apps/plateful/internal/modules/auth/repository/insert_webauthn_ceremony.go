package repository

import (
	"context"

	authdomain "example.com/plateful/internal/modules/auth/domain"
)

const insertWebAuthnCeremonySQL = `
	INSERT INTO auth_webauthn_ceremonies (id, token_hash, user_id, purpose, mfa_challenge_id, session_data, expires_at, created_at)
	VALUES ($1, $2, NULLIF($3, ''), $4, NULLIF($5, ''), $6, $7, $8)`

// InsertWebAuthnCeremony stores a started passkey ceremony; only the token's
// hash is stored.
func (s *Store) InsertWebAuthnCeremony(ctx context.Context, c authdomain.WebAuthnCeremony) error {
	_, err := s.db.Exec(ctx, insertWebAuthnCeremonySQL, c.ID, c.TokenHash, c.UserID, c.Purpose, c.MFAChallengeID, string(c.SessionData), c.ExpiresAt, c.CreatedAt)
	return err
}
