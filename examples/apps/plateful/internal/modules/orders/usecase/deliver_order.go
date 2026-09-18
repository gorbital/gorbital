package usecase

import (
	"context"

	"example.com/plateful/internal/modules/orders/domain"
)

// docs:start courier-advance

// CourierAdvance is a courier collecting an order and, later, delivering it.
//
// The route's guard proves the caller is signed in and holds
// orders.order.deliver, a platform permission the "user" role grants — which
// is every account on the platform, couriers and diners alike. That is all a
// guard can say here: guard.OrgMember is wrong, because a courier belongs to
// no organisation, and there is no role that means "the courier of this
// order".
//
// So the ownership check is this function's, and it is two steps: the caller
// must have a courier profile (the couriers module's platform-scoped table,
// read with SQL), and the order must name that courier. An order that names
// somebody else answers 404, the same as one that doesn't exist, so a
// courier can't learn what other orders are out there by trying IDs.
//
// Delivering frees the courier in the same transaction, so a courier can
// never be left holding an order that is already at the door.
func (s *Service) CourierAdvance(ctx context.Context, id string, to domain.Status) (domain.Order, error) {
	user, err := callerID(ctx)
	if err != nil {
		return domain.Order{}, err
	}
	if to != domain.StatusCollected && to != domain.StatusDelivered {
		return domain.Order{}, domain.ErrInvalidTransition
	}
	courier, err := s.store.CourierOfUser(ctx, user)
	if err != nil {
		return domain.Order{}, storeError("courier", err)
	}

	var moved domain.Order
	err = s.tx.InTx(ctx, func(tx Tx) error {
		current, err := tx.SelectOrder(ctx, id, true)
		if err != nil {
			return err
		}
		if current.CourierID != courier {
			return domain.ErrOrderNotFound
		}
		next, err := current.MoveTo(to, "", s.clock())
		if err != nil {
			return err
		}
		if moved, err = tx.UpdateOrder(ctx, next); err != nil {
			return err
		}
		if to == domain.StatusDelivered {
			return tx.SetCourierOrder(ctx, courier, "")
		}
		return nil
	})
	if err != nil {
		return domain.Order{}, storeError("deliver", err)
	}

	// docs:end courier-advance
	action := ActionAdvanced
	if to == domain.StatusDelivered {
		action = ActionDelivered
	}
	s.audit(ctx, action, moved.ID, map[string]any{"status": string(moved.Status), "courier_id": courier})
	return moved, nil
}
