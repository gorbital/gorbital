package repository

import (
	"context"

	authdomain "example.com/admin-tool/internal/modules/auth/domain"
)

const insertMFAChallengeSQL = `
	INSERT INTO auth_mfa_challenges (id, user_id, token_hash, max_attempts, expires_at, ip, user_agent, created_at)
	VALUES ($1, $2, $3, $4, $5, NULLIF($6, '')::inet, $7, $8)`

// InsertMFAChallenge stores a sign-in waiting for a second factor; only the
// token's hash is stored.
func (s *Store) InsertMFAChallenge(ctx context.Context, c authdomain.MFAChallenge) error {
	_, err := s.db.Exec(ctx, insertMFAChallengeSQL, c.ID, c.UserID, c.TokenHash, c.MaxAttempts, c.ExpiresAt, c.IP, c.UserAgent, c.CreatedAt)
	return err
}
