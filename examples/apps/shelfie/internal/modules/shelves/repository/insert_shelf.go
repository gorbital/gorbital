package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"example.com/shelfie/internal/modules/shelves/domain"
)

const insertShelfSQL = `
	INSERT INTO shelves (id, owner_id, name, description, visibility, version, created_at, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	RETURNING ` + shelfColumns

// InsertShelf stores a new shelf, or returns ErrShelfNameTaken when the
// owner already uses the value, ignoring case.
func (s *Store) InsertShelf(ctx context.Context, shelf domain.Shelf) (domain.Shelf, error) {
	rows, err := s.db.Query(ctx, insertShelfSQL,
		shelf.ID, shelf.OwnerID, shelf.Name, shelf.Description, shelf.Visibility, shelf.Version, shelf.CreatedAt, shelf.UpdatedAt)
	if err == nil {
		shelf, err = pgx.CollectExactlyOneRow(rows, scanShelf)
	}
	return shelf, constraintError(err)
}
