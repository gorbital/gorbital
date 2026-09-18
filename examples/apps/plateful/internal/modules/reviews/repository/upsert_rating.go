package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/reviews/domain"
)

// docs:start rating-upsert

// addToRatingSQL adds one review's contribution to a restaurant's running
// total, creating the row when it is the restaurant's first review.
//
// Three decisions are worth reading off this statement.
//
// The total lives in this module's own table, not in a rating column on
// restaurants. It is derived entirely from reviews, and the module that owns
// the rows owns what is computed from them; writing to the restaurants table
// would make this module a second writer of another module's data, and a
// restaurant would be locked by every review anybody left.
//
// It is recomputed in the transaction that writes the review, which is why
// this is called from inside usecase.WriteReview's InTx and not after it.
// The number a diner reads is therefore never stale and never disagrees with
// the rows on the same page. The alternative is a job that re-adds the
// column every few minutes: a smaller transaction and one less row to lock
// per review, at the cost of an average that is briefly wrong. A busier
// platform would take that trade; Plateful does not, because a restaurant
// with four reviews shows a visibly wrong average for as long as the job is
// behind.
//
// It stores the count and the sum rather than the average. The average is
// two integers divided when it is read (domain.Rating.Average), so it is
// exact, and so hiding a review can be undone by adding its stars back —
// neither of which is true of a stored mean.
//
// The deltas are applied by the database (review_count + $3), not read,
// changed in Go and written back, so two reviews landing at the same instant
// both count. The row is the only thing they contend on.
const addToRatingSQL = `
	INSERT INTO restaurant_ratings (restaurant_id, org_id, review_count, rating_sum, updated_at)
	VALUES ($1, $2, $3, $4, $5)
	ON CONFLICT (restaurant_id) DO UPDATE
	SET review_count = restaurant_ratings.review_count + $3,
	    rating_sum   = restaurant_ratings.rating_sum + $4,
	    updated_at   = $5
	RETURNING restaurant_id, org_id, review_count, rating_sum, updated_at`

// AddToRating adds countDelta reviews and sumDelta stars to a restaurant's
// running total. The deltas are negative when a review is hidden, which is
// how moderation takes it back out.
func (s *Store) AddToRating(ctx context.Context, restaurantID, orgID string, countDelta int, sumDelta int64, now time.Time) (domain.Rating, error) {
	rows, err := s.db.Query(ctx, addToRatingSQL, restaurantID, orgID, countDelta, sumDelta, now)
	if err != nil {
		return domain.Rating{}, err
	}
	return pgx.CollectExactlyOneRow(rows, scanRating)
}

// docs:end rating-upsert

const selectRatingSQL = `
	SELECT restaurant_id, org_id, review_count, rating_sum, updated_at
	FROM restaurant_ratings WHERE restaurant_id = $1`

// SelectRating returns a restaurant's running total. A restaurant nobody has
// reviewed has no row, and gets a zero-valued Rating rather than an error:
// "no reviews yet" is an answer the public list shows, not a failure.
func (s *Store) SelectRating(ctx context.Context, restaurantID string) (domain.Rating, error) {
	rows, err := s.db.Query(ctx, selectRatingSQL, restaurantID)
	if err != nil {
		return domain.Rating{}, err
	}
	a, err := pgx.CollectExactlyOneRow(rows, scanRating)
	if postgres.IsNoRows(err) {
		return domain.Rating{RestaurantID: restaurantID}, nil
	}
	return a, err
}

func scanRating(row pgx.CollectableRow) (domain.Rating, error) {
	var a domain.Rating
	err := row.Scan(&a.RestaurantID, &a.OrgID, &a.Count, &a.Sum, &a.UpdatedAt)
	a.UpdatedAt = a.UpdatedAt.UTC()
	return a, err
}
