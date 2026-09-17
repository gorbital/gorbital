package repository

import (
	"context"

	"example.com/shelfie/internal/modules/books/domain"
)

const insertShelfSQL = `
	INSERT INTO shelves (id, owner_id, name, created_at)
	VALUES ($1, $2, $3, $4)
	ON CONFLICT (owner_id, name) DO NOTHING`

// InsertShelf stores a shelf; a name the owner already has is left as it is.
func (s *Store) InsertShelf(ctx context.Context, shelf domain.Shelf) error {
	_, err := s.db.Exec(ctx, insertShelfSQL, shelf.ID, shelf.OwnerID, shelf.Name, shelf.CreatedAt)
	return err
}
