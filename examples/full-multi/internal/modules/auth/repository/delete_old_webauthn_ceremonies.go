package repository

import (
	"context"
	"time"
)

const deleteOldWebAuthnCeremoniesSQL = `DELETE FROM auth_webauthn_ceremonies WHERE expires_at < $1`

// DeleteOldWebAuthnCeremonies removes ceremonies that expired before before.
func (s *Store) DeleteOldWebAuthnCeremonies(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.db.Exec(ctx, deleteOldWebAuthnCeremoniesSQL, before)
	return tag.RowsAffected(), err
}
