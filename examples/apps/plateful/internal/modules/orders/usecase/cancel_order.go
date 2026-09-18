package usecase

import (
	"context"

	"example.com/plateful/internal/modules/orders/domain"
)

// docs:start cancel-order

// CancelOrder is the customer changing their mind. They may, until the
// kitchen has it ready: the state machine already says so (an order in
// "ready" or beyond can't move to "cancelled"), and this only has to prove
// the order is theirs.
//
// The proof is a comparison, not a guard: a customer is a member of no
// organisation, so nothing about membership can decide it. An order that
// belongs to somebody else answers 404 rather than 403, so a customer can't
// discover that an order exists by trying to cancel it.
func (s *Service) CancelOrder(ctx context.Context, id, reason string) (domain.Order, error) {
	customer, err := callerID(ctx)
	if err != nil {
		return domain.Order{}, err
	}
	var cancelled domain.Order
	err = s.tx.InTx(ctx, func(tx Tx) error {
		current, err := tx.SelectOrder(ctx, id, true)
		if err != nil {
			return err
		}
		if current.CustomerID != customer {
			return domain.ErrOrderNotFound
		}
		next, err := current.MoveTo(domain.StatusCancelled, reason, s.clock())
		if err != nil {
			return err
		}
		if cancelled, err = tx.UpdateOrder(ctx, next); err != nil {
			return err
		}
		if current.CourierID != "" {
			return tx.SetCourierOrder(ctx, current.CourierID, "")
		}
		return nil
	})
	if err != nil {
		return domain.Order{}, storeError("cancel", err)
	}
	s.audit(ctx, ActionCancelled, cancelled.ID, map[string]any{"status": string(cancelled.Status)})
	return cancelled, nil
}

// docs:end cancel-order
