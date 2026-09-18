package usecase

import (
	"context"

	"example.com/plateful/internal/modules/orders/domain"
)

// docs:start assign-courier

// AssignCourier gives one of the restaurant's orders to a courier.
//
// This is the platform's most awkward shape, and the tutorial should say so:
// the order belongs to an organisation, the courier belongs to none, and one
// transaction has to change both. The caller is restaurant staff, proved by
// guard.OrgMember on the route; the courier is a row in the couriers
// module's platform-scoped table, which this module reads and writes with
// SQL because a module never imports another module's layers, and because
// there is no way to call another module's use case inside this
// transaction.
//
// Both rows are locked, and both change together: an order that names a
// courier who is carrying something else, or a courier holding an order that
// doesn't name them, are the two states this must never leave behind.
func (s *Service) AssignCourier(ctx context.Context, orgID, id, courierID string) (domain.Order, error) {
	if _, err := memberID(ctx, orgID); err != nil {
		return domain.Order{}, err
	}
	var assigned domain.Order
	err := s.tx.InTx(ctx, func(tx Tx) error {
		current, err := tx.SelectOrgOrder(ctx, orgID, id, true)
		if err != nil {
			return err
		}
		next, err := current.AssignCourier(courierID, s.clock())
		if err != nil {
			return err
		}
		if next.CourierID == current.CourierID {
			assigned = current // already theirs
			return nil
		}
		// Claim the courier first: the update fails when somebody else took
		// them between the list and this call.
		if err := tx.SetCourierOrder(ctx, courierID, id); err != nil {
			return err
		}
		assigned, err = tx.UpdateOrder(ctx, next)
		return err
	})
	if err != nil {
		return domain.Order{}, storeError("assign", err)
	}
	s.audit(ctx, ActionCourierAssigned, assigned.ID, map[string]any{"courier_id": courierID})
	return assigned, nil
}

// docs:end assign-courier

// docs:start auto-assign-flag

// autoAssignCourier picks a free courier when an order becomes ready and
// nobody is carrying it. It runs inside the caller's transaction.
//
// The orders.courier_auto_assign flag decides whether it happens at all, and
// it is read with Evaluate rather than Enabled because when nothing is
// assigned the reason is worth a log line: "the flag is off for everyone" and
// "this restaurant is outside the rollout" are different problems, and the
// second is invisible without it. The flag's bucket subject is the
// organisation when the caller acts in one, so a percentage rollout moves
// whole restaurants at a time rather than flickering order by order.
func (s *Service) autoAssignCourier(ctx context.Context, tx Tx, o *domain.Order) error {
	if decision := s.autoAssign.Evaluate(ctx); !decision.Enabled {
		s.logger.DebugContext(ctx, "courier not assigned automatically",
			"order_id", o.ID, "flag", decision.Key, "reason", string(decision.Reason))
		return nil
	}
	free, err := tx.SelectAvailableCouriers(ctx, 1)
	if err != nil || len(free) == 0 {
		return err
	}
	next, err := o.AssignCourier(free[0].ID, s.clock())
	if err != nil {
		return err
	}
	if err := tx.SetCourierOrder(ctx, free[0].ID, o.ID); err != nil {
		return err
	}
	if next, err = tx.UpdateOrder(ctx, next); err != nil {
		return err
	}
	*o = next
	s.audit(ctx, ActionCourierAssigned, o.ID, map[string]any{"courier_id": free[0].ID, "automatic": true})
	return nil
}

// docs:end auto-assign-flag

// AvailableCouriers lists the couriers a restaurant could give an order to.
// The rows belong to no organisation: guard.OrgMember only proves the caller
// is staff of some restaurant, and what comes back is the little a
// restaurant needs to choose.
func (s *Service) AvailableCouriers(ctx context.Context, orgID string, limit int) ([]AvailableCourier, error) {
	if _, err := memberID(ctx, orgID); err != nil {
		return nil, err
	}
	couriers, err := s.store.SelectAvailableCouriers(ctx, limit)
	if err != nil {
		return nil, storeError("couriers", err)
	}
	return couriers, nil
}
