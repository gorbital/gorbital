package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"example.com/plateful/internal/modules/reviews/domain"
)

const insertReviewSQL = `
	INSERT INTO reviews (id, org_id, restaurant_id, order_id, customer_id, rating, comment,
	                     hidden, hidden_reason, version, created_at, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	RETURNING ` + reviewColumns

// InsertReview stores a new review, or returns ErrReviewExists when the
// order already has one.
func (s *Store) InsertReview(ctx context.Context, r domain.Review) (domain.Review, error) {
	rows, err := s.db.Query(ctx, insertReviewSQL,
		r.ID, r.OrgID, r.RestaurantID, r.OrderID, r.CustomerID, r.Rating, r.Comment,
		r.Hidden, r.HiddenReason, r.Version, r.CreatedAt, r.UpdatedAt)
	if err == nil {
		r, err = pgx.CollectExactlyOneRow(rows, scanReview)
	}
	return r, constraintError(err)
}
