package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/images/domain"
)

const updateImageSQL = `
	UPDATE images
	SET content_type = $3, size_bytes = $4, status = $5, updated_at = $6
	WHERE org_id = $1 AND id = $2
	RETURNING ` + imageColumns

// UpdateImage saves the measured size, media type and status of an image.
// There is no version column and no optimistic lock: the only update an
// image ever gets is its confirmation, and ConfirmUpload holds the row's
// lock from the SELECT to here.
func (s *Store) UpdateImage(ctx context.Context, i domain.Image) (domain.Image, error) {
	rows, err := s.db.Query(ctx, updateImageSQL,
		i.OrgID, i.ID, i.ContentType, i.SizeBytes, i.Status, i.UpdatedAt)
	if err != nil {
		return domain.Image{}, err
	}
	updated, err := pgx.CollectExactlyOneRow(rows, scanImage)
	if postgres.IsNoRows(err) {
		return domain.Image{}, domain.ErrImageNotFound
	}
	return updated, err
}
