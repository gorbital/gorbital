package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"example.com/shelfie/internal/modules/books/domain"
)

const selectShelvesSQL = `
	SELECT id, owner_id, name, created_at FROM shelves
	WHERE owner_id = $1
	ORDER BY created_at, id`

// SelectShelves returns an owner's shelves, oldest first.
func (s *Store) SelectShelves(ctx context.Context, ownerID string) ([]domain.Shelf, error) {
	rows, err := s.db.Query(ctx, selectShelvesSQL, ownerID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Shelf, error) {
		var sh domain.Shelf
		err := row.Scan(&sh.ID, &sh.OwnerID, &sh.Name, &sh.CreatedAt)
		sh.CreatedAt = sh.CreatedAt.UTC()
		return sh, err
	})
}
