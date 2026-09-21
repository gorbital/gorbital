package repository

import (
	"context"
	"time"
)

const consumeWebAuthnCeremonySQL = `UPDATE auth_webauthn_ceremonies SET consumed_at = $2 WHERE id = $1`

// ConsumeWebAuthnCeremony marks a ceremony as used, whether or not its
// response was valid.
func (s *Store) ConsumeWebAuthnCeremony(ctx context.Context, id string, now time.Time) error {
	_, err := s.db.Exec(ctx, consumeWebAuthnCeremonySQL, id, now)
	return err
}
