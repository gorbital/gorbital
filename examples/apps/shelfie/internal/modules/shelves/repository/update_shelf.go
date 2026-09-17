package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/shelfie/internal/modules/shelves/domain"
)

const updateShelfSQL = `
	UPDATE shelves
	SET name = $3, description = $4, visibility = $5, updated_at = $6, version = version + 1
	WHERE id = $1 AND owner_id = $2 AND version = $7
	RETURNING ` + shelfColumns

// UpdateShelf saves shelf when the stored version is still shelf.Version and
// returns it with the next version. It returns ErrShelfVersionConflict when
// no row has that version (changed, deleted or not the owner's), and
// ErrShelfNameTaken.
func (s *Store) UpdateShelf(ctx context.Context, shelf domain.Shelf) (domain.Shelf, error) {
	rows, err := s.db.Query(ctx, updateShelfSQL,
		shelf.ID, shelf.OwnerID, shelf.Name, shelf.Description, shelf.Visibility, shelf.UpdatedAt, shelf.Version)
	if err != nil {
		return domain.Shelf{}, constraintError(err)
	}
	updated, err := pgx.CollectExactlyOneRow(rows, scanShelf)
	if postgres.IsNoRows(err) {
		return domain.Shelf{}, domain.ErrShelfVersionConflict
	}
	return updated, constraintError(err)
}
