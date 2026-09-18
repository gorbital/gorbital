package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	authdomain "example.com/plateful/internal/modules/auth/domain"
)

//nolint:gosec // SQL text, not a credential
const (
	identityColumns = `id, user_id, provider, subject, email, private_email, name, COALESCE(refresh_key_id, ''), refresh_token_ciphertext,
		COALESCE(refresh_client_id, ''), created_at, last_used_at`
	selectIdentitiesSQL = `SELECT ` + identityColumns + ` FROM auth_identities WHERE user_id = $1 ORDER BY created_at, id`
)

// SelectIdentities returns a user's linked identities, oldest first.
func (s *Store) SelectIdentities(ctx context.Context, userID string) ([]authdomain.Identity, error) {
	rows, err := s.db.Query(ctx, selectIdentitiesSQL, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanIdentity)
}

func scanIdentity(row pgx.CollectableRow) (authdomain.Identity, error) {
	var i authdomain.Identity
	err := row.Scan(&i.ID, &i.UserID, &i.Provider, &i.Subject, &i.Email, &i.PrivateEmail, &i.Name, &i.RefreshKeyID, &i.RefreshTokenCiphertext,
		&i.RefreshClientID, &i.CreatedAt, &i.LastUsedAt)
	return i, err
}
