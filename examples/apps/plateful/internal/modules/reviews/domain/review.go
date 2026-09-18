// Package domain holds the reviews module's reviews, the rating a
// restaurant's reviews add up to, and their rules. It imports only the
// standard library.
package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Field limits and the window a review may be changed in. The migration's
// CHECK constraints match the lengths and the rating range.
const (
	MinRating        = 1
	MaxRating        = 5
	MaxCommentLength = 2000
	MaxReasonLength  = 500

	// EditWindow is how long after writing a review its author may change
	// it. A day is long enough to fix a rating typed in anger or a comment
	// typed on a phone, and short enough that a restaurant answering a
	// review can trust that the review it answered is the one people read.
	EditWindow = 24 * time.Hour
)

// A Review is one diner's verdict on one delivered order.
//
// Who may do what with it is unusual enough to be worth stating here. The
// review belongs to OrgID, the restaurant's organisation, but CustomerID —
// the only account that may write or change it — is a member of no
// organisation. The organisation's own staff may neither change it nor hide
// it. Hiding is platform staff's alone (usecase.PermModerate).
type Review struct {
	ID           string
	OrgID        string
	RestaurantID string
	// OrderID is the delivered order being reviewed, unique across the
	// table: one order, one review.
	OrderID string
	// CustomerID is the account that placed the order. It never appears in
	// a response: a diner reading reviews has no business knowing who wrote
	// them.
	CustomerID string
	Rating     int
	Comment    string
	// Hidden takes the review out of the public list and out of the
	// restaurant's rating. The row stays, so the decision can be reviewed
	// and undone.
	Hidden       bool
	HiddenReason string
	// Version increases with every change. An update must name the version
	// it read, so it can't overwrite a change it hasn't seen.
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewReview returns a review of the order by customerID, or a
// *ValidationError.
func NewReview(id, orgID, restaurantID, orderID, customerID string, rating int, comment string, now time.Time) (Review, error) {
	r := Review{
		ID: id, OrgID: orgID, RestaurantID: restaurantID, OrderID: orderID,
		CustomerID: customerID, Rating: rating, Comment: strings.TrimSpace(comment),
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := r.validate(); err != nil {
		return Review{}, err
	}
	return r, nil
}

// docs:start review-edit

// Edit returns the review with a new rating and comment, or
// ErrReviewWindowClosed once EditWindow has passed since it was written.
//
// The window is measured from CreatedAt, not UpdatedAt: editing a review
// must not buy another day to edit it again, which is what measuring from
// the last change would do.
func (r Review) Edit(rating int, comment string, now time.Time) (Review, error) {
	if now.Sub(r.CreatedAt) > EditWindow {
		return r, ErrReviewWindowClosed
	}
	next := r
	next.Rating, next.Comment, next.UpdatedAt = rating, strings.TrimSpace(comment), now
	if err := next.validate(); err != nil {
		return r, err
	}
	return next, nil
}

// docs:end review-edit

// Hide takes the review out of the public list and records why. It reports
// whether anything changed, so hiding a review twice is harmless and counts
// once against the restaurant's rating.
func (r Review) Hide(reason string, now time.Time) (Review, bool, error) {
	reason = strings.TrimSpace(reason)
	if msg := checkText(reason, 1, MaxReasonLength); msg != "" {
		return r, false, &ValidationError{Errors: []FieldError{{Field: "reason", Message: msg}}}
	}
	if r.Hidden {
		return r, false, nil
	}
	next := r
	next.Hidden, next.HiddenReason, next.UpdatedAt = true, reason, now
	return next, true, nil
}

func (r Review) validate() error {
	var errs []FieldError
	if r.Rating < MinRating || r.Rating > MaxRating {
		errs = append(errs, FieldError{Field: "rating", Message: fmt.Sprintf("must be between %d and %d", MinRating, MaxRating)})
	}
	if msg := checkText(r.Comment, 0, MaxCommentLength); msg != "" {
		errs = append(errs, FieldError{Field: "comment", Message: msg})
	}
	if len(errs) > 0 {
		return &ValidationError{Errors: errs}
	}
	return nil
}

// docs:start restaurant-rating

// A Rating is what a restaurant's visible reviews add up to: how many there
// are and what they sum to. The average is derived, never stored.
type Rating struct {
	RestaurantID string
	OrgID        string
	Count        int
	Sum          int64
	UpdatedAt    time.Time
}

// Average returns the mean rating, or 0 for a restaurant nobody has reviewed
// yet.
//
// It divides the stored sum by the stored count every time it is asked, so
// the answer is exactly the mean of the rows that exist. Keeping a running
// average instead would round once per review and drift, and could never be
// corrected by hiding one.
func (a Rating) Average() float64 {
	if a.Count <= 0 {
		return 0
	}
	return float64(a.Sum) / float64(a.Count)
}

// docs:end restaurant-rating

// Contribution is what one review adds to a restaurant's rating: one review
// and its stars, or nothing at all when the review is hidden.
func (r Review) Contribution() (count int, sum int64) {
	if r.Hidden {
		return 0, 0
	}
	return 1, int64(r.Rating)
}

// checkText returns why s isn't a valid text of min to max characters, or "".
func checkText(s string, minLen, maxLen int) string {
	switch n := utf8.RuneCountInString(s); {
	case !utf8.ValidString(s) || strings.ContainsRune(s, 0):
		return "must be valid text"
	case n < minLen:
		return "is required"
	case n > maxLen:
		return fmt.Sprintf("must be at most %d characters", maxLen)
	}
	return ""
}
