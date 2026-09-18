package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/images/domain"
)

const (
	selectImageSQL = `SELECT ` + imageColumns + ` FROM images WHERE org_id = $1 AND id = $2`
	forUpdate      = ` FOR UPDATE`
)

// SelectImage returns the organisation's image, or ErrImageNotFound. The
// organisation is part of the WHERE clause rather than checked afterwards,
// so another organisation's image is never read into memory at all. lock
// locks the row until the transaction ends.
func (s *Store) SelectImage(ctx context.Context, orgID, id string, lock bool) (domain.Image, error) {
	sql := selectImageSQL
	if lock {
		sql += forUpdate
	}
	rows, err := s.db.Query(ctx, sql, orgID, id)
	if err != nil {
		return domain.Image{}, err
	}
	i, err := pgx.CollectExactlyOneRow(rows, scanImage)
	if postgres.IsNoRows(err) {
		return domain.Image{}, domain.ErrImageNotFound
	}
	return i, err
}
