package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	authdomain "example.com/plateful/internal/modules/auth/domain"
)

// Empty password hashes are stored as NULL.
//
//nolint:gosec // SQL text, not a credential
const insertUserSQL = `
	INSERT INTO auth_users (id, email, email_normalized, password_hash, email_verified_at, password_changed_at, created_at, updated_at)
	VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6, $6, $6)
	RETURNING ` + userColumns

// InsertUser creates an account, or returns ErrEmailTaken when the address
// already has one.
func (s *Store) InsertUser(ctx context.Context, u authdomain.User) (authdomain.User, error) {
	rows, err := s.db.Query(ctx, insertUserSQL, u.ID, u.Email, u.NormalizedEmail, u.PasswordHash, u.EmailVerifiedAt, u.CreatedAt)
	if err == nil {
		u, err = pgx.CollectExactlyOneRow(rows, scanUser)
	}
	if _, taken := postgres.UniqueViolation(err); taken {
		return authdomain.User{}, authdomain.ErrEmailTaken
	}
	return u, err
}
