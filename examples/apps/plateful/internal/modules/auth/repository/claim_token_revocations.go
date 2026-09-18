package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	authdomain "example.com/plateful/internal/modules/auth/domain"
)

//nolint:gosec // SQL text, not a credential
const (
	tokenRevocationColumns   = `id, provider, subject, client_id, key_id, token_ciphertext, attempts, next_attempt_at, last_error, created_at`
	claimTokenRevocationsSQL = `
	UPDATE auth_token_revocations SET next_attempt_at = $2
	WHERE id IN (
		SELECT id FROM auth_token_revocations
		WHERE next_attempt_at <= $1
		ORDER BY next_attempt_at
		LIMIT $3
		FOR UPDATE SKIP LOCKED
	)
	RETURNING ` + tokenRevocationColumns
)

// ClaimTokenRevocations returns up to limit revocations due at now and
// leases them until leaseUntil, so concurrent workers skip them and a
// crashed worker's revocations come back.
func (s *Store) ClaimTokenRevocations(ctx context.Context, now, leaseUntil time.Time, limit int) ([]authdomain.TokenRevocation, error) {
	rows, err := s.db.Query(ctx, claimTokenRevocationsSQL, now, leaseUntil, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanTokenRevocation)
}

func scanTokenRevocation(row pgx.CollectableRow) (authdomain.TokenRevocation, error) {
	var r authdomain.TokenRevocation
	err := row.Scan(&r.ID, &r.Provider, &r.Subject, &r.ClientID, &r.KeyID, &r.TokenCiphertext, &r.Attempts, &r.NextAttemptAt, &r.LastError, &r.CreatedAt)
	return r, err
}
