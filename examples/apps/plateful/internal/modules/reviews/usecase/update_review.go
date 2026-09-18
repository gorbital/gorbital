package usecase

import (
	"context"

	"example.com/plateful/internal/modules/reviews/domain"
)

// UpdateReviewInput is the change a customer sends, with the version they
// read.
type UpdateReviewInput struct {
	Version int64
	Rating  int
	Comment string
}

// docs:start update-review

// UpdateReview changes a review its author wrote within domain.EditWindow.
//
// Ownership is the same fact as in WriteReview and is checked the same way:
// a review belonging to another account answers ErrReviewNotFound, not a
// 403, so reviews can't be enumerated. The window and the version are two
// different refusals — the window says the review is now history, the
// version says somebody else's change came first.
func (s *Service) UpdateReview(ctx context.Context, id string, in UpdateReviewInput) (domain.Review, error) {
	customer, err := callerID(ctx)
	if err != nil {
		return domain.Review{}, err
	}
	var saved domain.Review
	err = s.store.InTx(ctx, func(tx Store) error {
		current, err := tx.SelectReview(ctx, id, true)
		if err != nil {
			return err
		}
		if current.CustomerID != customer {
			return domain.ErrReviewNotFound
		}
		if current.Version != in.Version {
			return domain.ErrReviewVersionConflict
		}
		next, err := current.Edit(in.Rating, in.Comment, s.clock())
		if err != nil {
			return err
		}
		if saved, err = tx.UpdateReview(ctx, next); err != nil {
			return err
		}
		// Only the stars move the running total, and only while the review
		// is visible: a hidden review contributes nothing, so re-rating one
		// changes nothing to add up.
		oldCount, oldSum := current.Contribution()
		newCount, newSum := saved.Contribution()
		if oldCount == newCount && oldSum == newSum {
			return nil
		}
		_, err = tx.AddToRating(ctx, saved.RestaurantID, saved.OrgID, newCount-oldCount, newSum-oldSum, saved.UpdatedAt)
		return err
	})
	if err != nil {
		return domain.Review{}, storeError("update", err)
	}
	s.audit(ctx, ActionUpdated, saved.ID, saved.OrgID, map[string]any{
		"order_id": saved.OrderID, "restaurant_id": saved.RestaurantID, "rating": saved.Rating,
	})
	return saved, nil
}

// docs:end update-review
