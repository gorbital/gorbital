package repository

import (
	"context"

	authdomain "example.com/invoicing/internal/modules/auth/domain"
)

// A confirmed secret is never replaced: the WHERE clause leaves its row alone.
const upsertPendingTOTPSQL = `
	INSERT INTO auth_totp (user_id, key_id, secret_ciphertext, created_at)
	VALUES ($1, $2, $3, $4)
	ON CONFLICT (user_id) DO UPDATE
	SET key_id = EXCLUDED.key_id, secret_ciphertext = EXCLUDED.secret_ciphertext,
		created_at = EXCLUDED.created_at, last_used_step = NULL
	WHERE auth_totp.confirmed_at IS NULL`

// UpsertPendingTOTP stores an unconfirmed secret, replacing an unconfirmed
// one, and reports false when the user's secret is already confirmed.
func (s *Store) UpsertPendingTOTP(ctx context.Context, t authdomain.TOTP) (bool, error) {
	tag, err := s.db.Exec(ctx, upsertPendingTOTPSQL, t.UserID, t.KeyID, t.SecretCiphertext, t.CreatedAt)
	return tag.RowsAffected() == 1, err
}
