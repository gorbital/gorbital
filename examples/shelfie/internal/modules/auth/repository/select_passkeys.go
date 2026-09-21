package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	authdomain "example.com/shelfie/internal/modules/auth/domain"
)

//nolint:gosec // SQL text, not a credential
const (
	passkeyColumns    = `id, user_id, credential_id, credential::text, name, aaguid, backup_eligible, backup_state, sign_count, created_at, last_used_at`
	selectPasskeysSQL = `SELECT ` + passkeyColumns + ` FROM auth_passkeys WHERE user_id = $1 ORDER BY created_at, id`
)

// SelectPasskeys returns a user's passkeys, oldest first.
func (s *Store) SelectPasskeys(ctx context.Context, userID string) ([]authdomain.Passkey, error) {
	rows, err := s.db.Query(ctx, selectPasskeysSQL, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanPasskey)
}

func scanPasskey(row pgx.CollectableRow) (authdomain.Passkey, error) {
	var p authdomain.Passkey
	var record string
	err := row.Scan(&p.ID, &p.UserID, &p.CredentialID, &record, &p.Name, &p.AAGUID, &p.BackupEligible, &p.BackupState, &p.SignCount, &p.CreatedAt, &p.LastUsedAt)
	p.Record = []byte(record)
	return p, err
}
