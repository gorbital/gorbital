package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	authdomain "example.com/admin-tool/internal/modules/auth/domain"
)

// Sessions of deleted accounts are never returned.
//
//nolint:gosec // SQL text, not a credential
const selectSessionByTokenHashSQL = `
	SELECT s.id, s.user_id, s.created_at, s.last_seen_at, s.idle_expires_at, s.absolute_expires_at, s.revoked_at,
		COALESCE(host(s.ip), ''), s.user_agent, s.mfa_verified_at,
		u.id, u.email, u.email_normalized, COALESCE(u.password_hash, ''), u.email_verified_at, u.created_at
	FROM auth_sessions s
	JOIN auth_users u ON u.id = s.user_id AND u.deleted_at IS NULL
	WHERE s.token_hash = $1`

// SelectSessionByTokenHash returns a session and its account, or
// ErrSessionNotFound.
func (s *Store) SelectSessionByTokenHash(ctx context.Context, tokenHash []byte) (authdomain.Session, authdomain.User, error) {
	var sess authdomain.Session
	var u authdomain.User
	err := s.db.QueryRow(ctx, selectSessionByTokenHashSQL, tokenHash).Scan(
		&sess.ID, &sess.UserID, &sess.CreatedAt, &sess.LastSeenAt, &sess.IdleExpiresAt, &sess.AbsoluteExpiresAt, &sess.RevokedAt, &sess.IP, &sess.UserAgent, &sess.MFAVerifiedAt,
		&u.ID, &u.Email, &u.NormalizedEmail, &u.PasswordHash, &u.EmailVerifiedAt, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return authdomain.Session{}, authdomain.User{}, authdomain.ErrSessionNotFound
	}
	sess.TokenHash = tokenHash
	return sess, u, err
}
