package usecase

import (
	"context"
	"time"

	"example.com/plateful/internal/modules/reviews/domain"
)

// ListQuery selects one page of a restaurant's visible reviews, newest
// first.
type ListQuery struct {
	RestaurantID string
	// After, when set, starts the page after this position.
	After *Position
	Limit int
}

// Position is where a page ended: the last review's creation time and ID.
type Position struct {
	Time string // RFC 3339
	ID   string
}

// Store reads and writes reviews and the rating they add up to;
// repository.Store implements it with SQL.
type Store interface {
	// SelectOrder returns what this module needs to know about an order, or
	// ErrOrderNotFound. The orders table belongs to the orders module and is
	// read with SQL, never through a Go import.
	SelectOrder(ctx context.Context, orderID string) (domain.OrderFacts, error)
	// InsertReview stores a new review, or returns ErrReviewExists when the
	// order already has one.
	InsertReview(ctx context.Context, r domain.Review) (domain.Review, error)
	// SelectReview returns one review by ID, or ErrReviewNotFound. lock
	// locks the row until the transaction ends.
	SelectReview(ctx context.Context, id string, lock bool) (domain.Review, error)
	// SelectReviews returns up to q.Limit visible reviews of a restaurant,
	// newest first, starting after q.After.
	SelectReviews(ctx context.Context, q ListQuery) ([]domain.Review, error)
	// UpdateReview saves r when the stored version is still r.Version and
	// increments the version. It returns ErrReviewVersionConflict when the
	// version changed or the review is gone.
	UpdateReview(ctx context.Context, r domain.Review) (domain.Review, error)
	// SelectRating returns a restaurant's running total, zero-valued for a
	// restaurant nobody has reviewed.
	SelectRating(ctx context.Context, restaurantID string) (domain.Rating, error)
	// AddToRating adds countDelta reviews and sumDelta stars to a
	// restaurant's running total, creating the row when it is the first.
	// Deltas may be negative, which is how hiding a review takes it out.
	AddToRating(ctx context.Context, restaurantID, orgID string, countDelta int, sumDelta int64, now time.Time) (domain.Rating, error)
	// InTx runs fn in one transaction: it commits when fn returns nil and
	// rolls back otherwise.
	InTx(ctx context.Context, fn func(tx Store) error) error
}
