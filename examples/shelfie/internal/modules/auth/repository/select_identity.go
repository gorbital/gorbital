package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	authdomain "example.com/shelfie/internal/modules/auth/domain"
)

const (
	selectIdentitySQL       = `SELECT ` + identityColumns + ` FROM auth_identities WHERE provider = $1 AND subject = $2`
	selectIdentityLockedSQL = selectIdentitySQL + ` FOR UPDATE`
)

// SelectIdentity returns the identity of a provider's subject; lock locks it
// until the transaction ends.
func (s *Store) SelectIdentity(ctx context.Context, provider, subject string, lock bool) (authdomain.Identity, bool, error) {
	query := selectIdentitySQL
	if lock {
		query = selectIdentityLockedSQL
	}
	rows, err := s.db.Query(ctx, query, provider, subject)
	if err != nil {
		return authdomain.Identity{}, false, err
	}
	i, err := pgx.CollectExactlyOneRow(rows, scanIdentity)
	if errors.Is(err, pgx.ErrNoRows) {
		return authdomain.Identity{}, false, nil
	}
	return i, err == nil, err
}
