package repository

import (
	"context"

	"gorbital.dev/modules/postgres"

	authdomain "example.com/admin-tool/internal/modules/auth/domain"
)

//nolint:gosec // SQL text, not a credential
const insertIdentitySQL = `
	INSERT INTO auth_identities (id, user_id, provider, subject, email, private_email, name, refresh_key_id, refresh_token_ciphertext, refresh_client_id, created_at, last_used_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), $9, NULLIF($10, ''), $11, $11)`

// InsertIdentity links an identity to a user, or returns ErrIdentityTaken
// when another request linked it first.
func (s *Store) InsertIdentity(ctx context.Context, i authdomain.Identity) error {
	_, err := s.db.Exec(ctx, insertIdentitySQL, i.ID, i.UserID, i.Provider, i.Subject, i.Email, i.PrivateEmail, i.Name,
		i.RefreshKeyID, i.RefreshTokenCiphertext, i.RefreshClientID, i.CreatedAt)
	if _, taken := postgres.UniqueViolation(err); taken {
		return authdomain.ErrIdentityTaken
	}
	return err
}
