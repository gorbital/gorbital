package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"example.com/plateful/internal/modules/images/domain"
)

const insertImageSQL = `
	INSERT INTO images (id, org_id, created_by, purpose, storage_key, content_type,
	                    size_bytes, status, created_at, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	RETURNING ` + imageColumns

// InsertImage stores a new pending image.
func (s *Store) InsertImage(ctx context.Context, i domain.Image) (domain.Image, error) {
	rows, err := s.db.Query(ctx, insertImageSQL,
		i.ID, i.OrgID, i.CreatedBy, i.Purpose, i.StorageKey, i.ContentType,
		i.SizeBytes, i.Status, i.CreatedAt, i.UpdatedAt)
	if err != nil {
		return domain.Image{}, err
	}
	return pgx.CollectExactlyOneRow(rows, scanImage)
}
