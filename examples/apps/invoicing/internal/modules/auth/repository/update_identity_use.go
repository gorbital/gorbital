package repository

import (
	"context"
	"time"
)

const updateIdentityUseSQL = `
	UPDATE auth_identities SET email = $2, private_email = $3, last_used_at = $4
	WHERE id = $1`

// UpdateIdentityUse records a sign-in with an identity and the email address
// the provider sent.
func (s *Store) UpdateIdentityUse(ctx context.Context, id, email string, privateEmail bool, now time.Time) error {
	_, err := s.db.Exec(ctx, updateIdentityUseSQL, id, email, privateEmail, now)
	return err
}
