package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/reviews/domain"
)

const updateReviewSQL = `
	UPDATE reviews
	SET rating = $2, comment = $3, hidden = $4, hidden_reason = $5, updated_at = $6,
	    version = version + 1
	WHERE id = $1 AND version = $7
	RETURNING ` + reviewColumns

// UpdateReview saves r when the stored version is still r.Version and
// returns it with the next version. It returns ErrReviewVersionConflict when
// no row has that version (changed or deleted).
func (s *Store) UpdateReview(ctx context.Context, r domain.Review) (domain.Review, error) {
	rows, err := s.db.Query(ctx, updateReviewSQL,
		r.ID, r.Rating, r.Comment, r.Hidden, r.HiddenReason, r.UpdatedAt, r.Version)
	if err != nil {
		return domain.Review{}, constraintError(err)
	}
	updated, err := pgx.CollectExactlyOneRow(rows, scanReview)
	if postgres.IsNoRows(err) {
		return domain.Review{}, domain.ErrReviewVersionConflict
	}
	return updated, constraintError(err)
}
