package usecase

import (
	"context"

	"example.com/plateful/internal/modules/reviews/domain"
)

// docs:start hide-review

// HideReview takes an abusive review out of the public list and out of the
// restaurant's rating, and records why.
//
// This is platform staff's operation and nobody else's. The route is
// /v1/platform/reviews/{id}/hide, guarded by
// guard.Permission(PermModerate), which only the "platform_admin" role
// holds.
//
// There is deliberately no organisation-scoped route beside it. A restaurant
// that could hide its own bad reviews would leave a review list that says
// nothing, and every diner reading it would be misled by this app. The
// product decision is "the tenant cannot delete its bad reviews", and the
// only way to make that decision visible in code is the absence of a route —
// so it is written down here, next to the route that does exist, rather than
// left to be inferred from a route table that doesn't mention it.
//
// The restaurant's remedy is to ask platform staff, who leave an audit event
// naming themselves and their reason. That trail is the point: moderation
// that can't be inspected is indistinguishable from a tenant deleting its
// own reviews.
//
// Hiding is idempotent. A review hidden twice is counted out of the rating
// once, because the second call finds nothing to change.
func (s *Service) HideReview(ctx context.Context, id, reason string) (domain.Review, error) {
	if _, err := callerID(ctx); err != nil {
		return domain.Review{}, err
	}
	var saved domain.Review
	err := s.store.InTx(ctx, func(tx Store) error {
		current, err := tx.SelectReview(ctx, id, true)
		if err != nil {
			return err
		}
		next, changed, err := current.Hide(reason, s.clock())
		if err != nil {
			return err
		}
		if !changed {
			saved = current
			return nil
		}
		if saved, err = tx.UpdateReview(ctx, next); err != nil {
			return err
		}
		// The review leaves the rating in the transaction that hides it, so
		// the average a diner reads never counts a review they can't see.
		count, sum := current.Contribution()
		_, err = tx.AddToRating(ctx, saved.OrgID, -count, -sum, saved.UpdatedAt)
		return err
	})
	if err != nil {
		return domain.Review{}, storeError("hide", err)
	}
	s.audit(ctx, ActionHidden, saved.ID, saved.OrgID, map[string]any{
		"restaurant_id": saved.OrgID, "reason": saved.HiddenReason,
	})
	return saved, nil
}

// docs:end hide-review
