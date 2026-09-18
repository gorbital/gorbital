package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	authdomain "example.com/plateful/internal/modules/auth/domain"
)

const (
	totpColumns   = `user_id, key_id, secret_ciphertext, confirmed_at, last_used_step, created_at`
	selectTOTPSQL = `SELECT ` + totpColumns + ` FROM auth_totp WHERE user_id = $1`
)

// SelectTOTP returns a user's authenticator app secret; lock locks it until
// the transaction ends.
func (s *Store) SelectTOTP(ctx context.Context, userID string, lock bool) (authdomain.TOTP, bool, error) {
	sql := selectTOTPSQL
	if lock {
		sql += forUpdate
	}
	rows, err := s.db.Query(ctx, sql, userID)
	if err != nil {
		return authdomain.TOTP{}, false, err
	}
	t, err := pgx.CollectExactlyOneRow(rows, scanTOTP)
	if errors.Is(err, pgx.ErrNoRows) {
		return authdomain.TOTP{}, false, nil
	}
	return t, err == nil, err
}

func scanTOTP(row pgx.CollectableRow) (authdomain.TOTP, error) {
	var t authdomain.TOTP
	err := row.Scan(&t.UserID, &t.KeyID, &t.SecretCiphertext, &t.ConfirmedAt, &t.LastUsedStep, &t.CreatedAt)
	return t, err
}
