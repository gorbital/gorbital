package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"example.com/shelfie/internal/modules/profiles/domain"
)

const upsertProfileSQL = `
	INSERT INTO profiles (user_id, display_name, country, created_at, updated_at)
	VALUES ($1, $2, $3, $4, $5)
	ON CONFLICT (user_id) DO UPDATE SET display_name = EXCLUDED.display_name, country = EXCLUDED.country, updated_at = EXCLUDED.updated_at
	RETURNING ` + profileColumns

// UpsertProfile creates or changes a reader's profile; a suspension stays.
func (s *Store) UpsertProfile(ctx context.Context, p domain.Profile) (domain.Profile, error) {
	rows, err := s.db.Query(ctx, upsertProfileSQL, p.UserID, p.DisplayName, p.Country, p.CreatedAt, p.UpdatedAt)
	if err != nil {
		return domain.Profile{}, err
	}
	return pgx.CollectExactlyOneRow(rows, scanProfile)
}
