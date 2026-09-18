package usecase

import (
	"context"

	"example.com/plateful/internal/modules/reviews/domain"
)

// WriteReviewInput is what a customer says about their order.
type WriteReviewInput struct {
	Rating  int
	Comment string
}

// docs:start write-review

// WriteReview stores a customer's review of a delivered order.
//
// The route can only get the caller this far. guard.Permission(PermWrite)
// says the caller is a signed-in account whose platform role is "user" —
// that is every account on the platform, so by itself it authorises
// nothing. guard.OrgMember is not an alternative: the caller is a customer,
// a member of no organisation, and the row they are about to create belongs
// to the restaurant's. So the real authorisation is here, and it is a fact
// about data rather than about roles: the order names this account as its
// customer.
//
// The order of the two refusals matters. Ownership is checked before state,
// and an order belonging to somebody else answers ErrOrderNotFound, exactly
// as an order that doesn't exist does. Checking state first would leak: a
// stranger who tried IDs would learn which of them are delivered orders.
//
// The rating is added to the restaurant's running total inside the same
// transaction as the review, so the two can never disagree — see
// repository/upsert_rating.go for why the total lives in this module.
func (s *Service) WriteReview(ctx context.Context, orderID string, in WriteReviewInput) (domain.Review, error) {
	customer, err := callerID(ctx)
	if err != nil {
		return domain.Review{}, err
	}
	var saved domain.Review
	err = s.store.InTx(ctx, func(tx Store) error {
		order, err := tx.SelectOrder(ctx, orderID)
		if err != nil {
			return err
		}
		if err := order.Reviewable(customer); err != nil {
			return err
		}
		r, err := domain.NewReview(s.newID(), order.OrgID, orderID, customer, in.Rating, in.Comment, s.clock())
		if err != nil {
			return err
		}
		if saved, err = tx.InsertReview(ctx, r); err != nil {
			return err
		}
		count, sum := saved.Contribution()
		_, err = tx.AddToRating(ctx, saved.OrgID, count, sum, saved.UpdatedAt)
		return err
	})
	if err != nil {
		return domain.Review{}, storeError("write", err)
	}
	s.audit(ctx, ActionCreated, saved.ID, saved.OrgID, map[string]any{
		"order_id": saved.OrderID, "restaurant_id": saved.OrgID, "rating": saved.Rating,
	})
	return saved, nil
}

// docs:end write-review
