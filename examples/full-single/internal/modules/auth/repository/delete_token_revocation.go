package repository

import "context"

//nolint:gosec // SQL text, not a credential
const deleteTokenRevocationSQL = `DELETE FROM auth_token_revocations WHERE id = $1`

// DeleteTokenRevocation removes a revocation that succeeded or was abandoned.
func (s *Store) DeleteTokenRevocation(ctx context.Context, id string) error {
	_, err := s.db.Exec(ctx, deleteTokenRevocationSQL, id)
	return err
}
