package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/shelfie/internal/modules/profiles/domain"
)

const selectProfileSQL = `SELECT ` + profileColumns + ` FROM profiles WHERE user_id = $1`

// SelectProfile returns a reader's profile, or ErrProfileIncomplete.
func (s *Store) SelectProfile(ctx context.Context, userID string) (domain.Profile, error) {
	rows, err := s.db.Query(ctx, selectProfileSQL, userID)
	if err != nil {
		return domain.Profile{}, err
	}
	p, err := pgx.CollectExactlyOneRow(rows, scanProfile)
	if postgres.IsNoRows(err) {
		return domain.Profile{}, domain.ErrProfileIncomplete
	}
	return p, err
}
