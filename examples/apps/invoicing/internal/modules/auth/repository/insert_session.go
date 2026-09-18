package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	authdomain "example.com/invoicing/internal/modules/auth/domain"
)

const insertSessionSQL = `
	INSERT INTO auth_sessions (id, user_id, token_hash, created_at, last_seen_at, idle_expires_at, absolute_expires_at, ip, user_agent, mfa_verified_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, '')::inet, $9, $10)
	RETURNING ` + sessionColumns

// InsertSession starts a session; only the token's hash is stored.
func (s *Store) InsertSession(ctx context.Context, in authdomain.Session) (authdomain.Session, error) {
	rows, err := s.db.Query(ctx, insertSessionSQL, in.ID, in.UserID, in.TokenHash,
		in.CreatedAt, in.LastSeenAt, in.IdleExpiresAt, in.AbsoluteExpiresAt, in.IP, in.UserAgent, in.MFAVerifiedAt)
	if err != nil {
		return authdomain.Session{}, err
	}
	created, err := pgx.CollectExactlyOneRow(rows, scanSession)
	created.TokenHash = in.TokenHash
	return created, err
}
