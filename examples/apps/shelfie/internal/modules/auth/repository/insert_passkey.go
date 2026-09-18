package repository

import (
	"context"

	authdomain "example.com/shelfie/internal/modules/auth/domain"
)

//nolint:gosec // SQL text, not a credential
const insertPasskeySQL = `
	INSERT INTO auth_passkeys (id, user_id, credential_id, credential, name, aaguid, backup_eligible, backup_state, sign_count, created_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`

// InsertPasskey stores a verified passkey.
func (s *Store) InsertPasskey(ctx context.Context, p authdomain.Passkey) error {
	_, err := s.db.Exec(ctx, insertPasskeySQL, p.ID, p.UserID, p.CredentialID, string(p.Record), p.Name, p.AAGUID,
		p.BackupEligible, p.BackupState, p.SignCount, p.CreatedAt)
	return err
}
