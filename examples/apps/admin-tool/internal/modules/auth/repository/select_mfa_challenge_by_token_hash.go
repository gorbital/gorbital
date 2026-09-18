package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	authdomain "example.com/admin-tool/internal/modules/auth/domain"
)

//nolint:gosec // SQL text, not a credential
const selectMFAChallengeByTokenHashSQL = `
	SELECT id, user_id, token_hash, attempts, max_attempts, expires_at, consumed_at, COALESCE(host(ip), ''), user_agent, created_at
	FROM auth_mfa_challenges
	WHERE token_hash = $1
	FOR UPDATE`

// SelectMFAChallengeByTokenHash locks the challenge with a token's hash.
func (s *Store) SelectMFAChallengeByTokenHash(ctx context.Context, tokenHash []byte) (authdomain.MFAChallenge, bool, error) {
	var c authdomain.MFAChallenge
	err := s.db.QueryRow(ctx, selectMFAChallengeByTokenHashSQL, tokenHash).Scan(
		&c.ID, &c.UserID, &c.TokenHash, &c.Attempts, &c.MaxAttempts, &c.ExpiresAt, &c.ConsumedAt, &c.IP, &c.UserAgent, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return authdomain.MFAChallenge{}, false, nil
	}
	return c, err == nil, err
}
