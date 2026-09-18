package domain

import (
	"errors"
	"strings"
)

// Errors of the reviews use cases. module.go maps each to an HTTP status and
// a problem code, which are public API.
var (
	// ErrUnauthenticated reports a write without a signed-in account. The
	// public list never returns it: reading a restaurant's reviews needs no
	// caller at all.
	ErrUnauthenticated = errors.New("reviews: a signed-in account is required")

	// ErrInvalidReview reports invalid fields. The error is a
	// *ValidationError.
	ErrInvalidReview = errors.New("reviews: invalid review")

	// ErrReviewNotFound reports a review that doesn't exist, or one that
	// belongs to somebody else. The two aren't told apart, so a customer
	// can't find other people's reviews by trying IDs.
	ErrReviewNotFound = errors.New("reviews: review not found")

	// ErrReviewExists reports a second review of the same order. One order,
	// one review: the unique index on reviews.order_id is what actually
	// enforces it, so two requests that race still leave one review.
	ErrReviewExists = errors.New("reviews: the order has already been reviewed")

	// ErrOrderNotFound reports an order that doesn't exist, and an order
	// that belongs to another customer. Deliberately the same error: a
	// customer who could tell the two apart could probe for other people's
	// order IDs.
	ErrOrderNotFound = errors.New("reviews: order not found")

	// ErrOrderNotDelivered reports a review of an order that hasn't arrived.
	// Only a delivered order has been experienced, so only a delivered order
	// can be rated.
	ErrOrderNotDelivered = errors.New("reviews: the order has not been delivered")

	// ErrReviewWindowClosed reports a change after EditWindow. The customer
	// keeps what they wrote; they no longer get to rewrite it under a
	// restaurant that has since replied to it.
	ErrReviewWindowClosed = errors.New("reviews: the review can no longer be changed")

	// ErrReviewVersionConflict reports an update to a version that is no
	// longer current.
	ErrReviewVersionConflict = errors.New("reviews: review was changed since it was read")
)

// FieldError is one invalid field.
type FieldError struct {
	Field   string
	Message string
}

// ValidationError lists every invalid field of a review.
type ValidationError struct {
	Errors []FieldError
}

func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Errors))
	for i, f := range e.Errors {
		parts[i] = f.Field + " " + f.Message
	}
	return "reviews: invalid review: " + strings.Join(parts, "; ")
}

// Unwrap returns ErrInvalidReview.
func (e *ValidationError) Unwrap() error { return ErrInvalidReview }
