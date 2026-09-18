package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/reviews/domain"
)

const (
	selectReviewSQL = `SELECT ` + reviewColumns + ` FROM reviews WHERE id = $1`
	forUpdate       = ` FOR UPDATE`
)

// SelectReview returns one review by ID, or ErrReviewNotFound. lock locks
// the row until the transaction ends, so the rating can't be adjusted twice
// by two requests reading the same review.
func (s *Store) SelectReview(ctx context.Context, id string, lock bool) (domain.Review, error) {
	sql := selectReviewSQL
	if lock {
		sql += forUpdate
	}
	rows, err := s.db.Query(ctx, sql, id)
	if err != nil {
		return domain.Review{}, err
	}
	r, err := pgx.CollectExactlyOneRow(rows, scanReview)
	if postgres.IsNoRows(err) {
		return domain.Review{}, domain.ErrReviewNotFound
	}
	return r, err
}
