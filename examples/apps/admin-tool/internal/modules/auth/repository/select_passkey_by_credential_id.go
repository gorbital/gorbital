package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	authdomain "example.com/admin-tool/internal/modules/auth/domain"
)

//nolint:gosec // SQL text, not a credential
const selectPasskeyByCredentialIDSQL = `SELECT ` + passkeyColumns + ` FROM auth_passkeys WHERE credential_id = $1 FOR UPDATE`

// SelectPasskeyByCredentialID returns the passkey with a credential ID,
// locked until the transaction ends so simultaneous sign-ins with it update
// its signature counter one after the other.
func (s *Store) SelectPasskeyByCredentialID(ctx context.Context, credentialID []byte) (authdomain.Passkey, bool, error) {
	rows, err := s.db.Query(ctx, selectPasskeyByCredentialIDSQL, credentialID)
	if err != nil {
		return authdomain.Passkey{}, false, err
	}
	p, err := pgx.CollectExactlyOneRow(rows, scanPasskey)
	if errors.Is(err, pgx.ErrNoRows) {
		return authdomain.Passkey{}, false, nil
	}
	return p, err == nil, err
}
