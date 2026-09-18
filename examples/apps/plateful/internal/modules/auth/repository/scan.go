package repository

import (
	"github.com/jackc/pgx/v5"

	authdomain "example.com/plateful/internal/modules/auth/domain"
)

const userColumns = `id, email, email_normalized, COALESCE(password_hash, ''), email_verified_at, created_at, webauthn_user_handle, banned_at, COALESCE(banned_reason, '')`

func scanUser(row pgx.CollectableRow) (authdomain.User, error) {
	var u authdomain.User
	err := row.Scan(&u.ID, &u.Email, &u.NormalizedEmail, &u.PasswordHash, &u.EmailVerifiedAt, &u.CreatedAt, &u.WebAuthnUserHandle, &u.BannedAt, &u.BannedReason)
	u.CreatedAt = u.CreatedAt.UTC()
	return u, err
}

const sessionColumns = `id, user_id, created_at, last_seen_at, idle_expires_at, absolute_expires_at, revoked_at, COALESCE(host(ip), ''), user_agent, mfa_verified_at`

func scanSession(row pgx.CollectableRow) (authdomain.Session, error) {
	var s authdomain.Session
	err := row.Scan(&s.ID, &s.UserID, &s.CreatedAt, &s.LastSeenAt, &s.IdleExpiresAt, &s.AbsoluteExpiresAt, &s.RevokedAt, &s.IP, &s.UserAgent, &s.MFAVerifiedAt)
	s.CreatedAt, s.LastSeenAt = s.CreatedAt.UTC(), s.LastSeenAt.UTC()
	s.IdleExpiresAt, s.AbsoluteExpiresAt = s.IdleExpiresAt.UTC(), s.AbsoluteExpiresAt.UTC()
	return s, err
}
