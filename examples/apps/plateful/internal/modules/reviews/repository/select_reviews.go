package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"example.com/plateful/internal/modules/reviews/domain"
	"example.com/plateful/internal/modules/reviews/usecase"
)

// docs:start select-reviews

// selectReviewsSQL is the public list's query, and the only one in this
// module an unauthenticated caller reaches. There is one sort, so there is
// one query and no SQL is ever assembled from anything a request sends.
//
// NOT hidden is in the WHERE clause rather than applied afterwards, and it
// matches the partial index reviews_restaurant_created, so moderated rows
// cost the scan nothing. Pages use keyset pagination: the next page starts
// after the last row's (created_at, id), newest first.
const selectReviewsSQL = `
	SELECT ` + reviewColumns + ` FROM reviews
	WHERE restaurant_id = $1
	  AND NOT hidden
	  AND (NOT $2::boolean OR (created_at, id) < ($3::timestamptz, $4))
	ORDER BY created_at DESC, id DESC
	LIMIT $5`

// SelectReviews returns up to q.Limit visible reviews of a restaurant,
// newest first, starting after q.After.
func (s *Store) SelectReviews(ctx context.Context, q usecase.ListQuery) ([]domain.Review, error) {
	// The unused side of the comparison still has to parse, so a first page
	// passes a timestamp of the right shape rather than an empty string.
	after, afterID := "0001-01-01T00:00:00Z", ""
	if q.After != nil {
		after, afterID = q.After.Time, q.After.ID
	}
	rows, err := s.db.Query(ctx, selectReviewsSQL, q.RestaurantID, q.After != nil, after, afterID, q.Limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanReview)
}

// docs:end select-reviews
