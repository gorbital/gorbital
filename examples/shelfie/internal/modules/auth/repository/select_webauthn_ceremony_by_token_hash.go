package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	authdomain "example.com/shelfie/internal/modules/auth/domain"
)

//nolint:gosec // SQL text, not a credential
const selectWebAuthnCeremonyByTokenHashSQL = `
	SELECT id, token_hash, COALESCE(user_id, ''), purpose, COALESCE(mfa_challenge_id, ''), session_data::text, expires_at, consumed_at, created_at
	FROM auth_webauthn_ceremonies
	WHERE token_hash = $1
	FOR UPDATE`

// SelectWebAuthnCeremonyByTokenHash locks the ceremony with a token's hash.
func (s *Store) SelectWebAuthnCeremonyByTokenHash(ctx context.Context, tokenHash []byte) (authdomain.WebAuthnCeremony, bool, error) {
	var c authdomain.WebAuthnCeremony
	var session string
	err := s.db.QueryRow(ctx, selectWebAuthnCeremonyByTokenHashSQL, tokenHash).Scan(
		&c.ID, &c.TokenHash, &c.UserID, &c.Purpose, &c.MFAChallengeID, &session, &c.ExpiresAt, &c.ConsumedAt, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return authdomain.WebAuthnCeremony{}, false, nil
	}
	c.SessionData = []byte(session)
	return c, err == nil, err
}
