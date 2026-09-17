package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/shelfie/internal/modules/shelves/domain"
)

const (
	selectShelfSQL = `SELECT ` + shelfColumns + ` FROM shelves WHERE id = $1 AND owner_id = $2`
	forUpdate      = ` FOR UPDATE`
)

// SelectShelf returns one of ownerID's shelves, or ErrShelfNotFound.
// lock locks the row until the transaction ends.
func (s *Store) SelectShelf(ctx context.Context, ownerID, id string, lock bool) (domain.Shelf, error) {
	sql := selectShelfSQL
	if lock {
		sql += forUpdate
	}
	rows, err := s.db.Query(ctx, sql, id, ownerID)
	if err != nil {
		return domain.Shelf{}, err
	}
	shelf, err := pgx.CollectExactlyOneRow(rows, scanShelf)
	if postgres.IsNoRows(err) {
		return domain.Shelf{}, domain.ErrShelfNotFound
	}
	return shelf, err
}
