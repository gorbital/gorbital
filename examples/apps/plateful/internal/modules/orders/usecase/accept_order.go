package usecase

import (
	"context"

	"example.com/plateful/internal/modules/orders/domain"
)

// docs:start accept-order

// AcceptOrder is the restaurant taking an order on. It is not an update with
// a different name: two rules decide whether it can happen, and neither
// belongs in a handler.
//
// The first is the state machine's — only an order still in "placed" can be
// accepted, so a second tap on a busy Friday gets 409 rather than quietly
// re-accepting something already in the oven.
//
// The second spans two modules. The money is the payments module's: its
// order_payments table holds the status the provider's webhook writes. A
// restaurant must not start cooking before that says "authorised", so this
// reads it — with SQL, inside the same transaction that locks the order,
// because a module never imports another module's layers. gorbital has no
// way to express a rule that spans two modules in one place; the honest
// thing is to write it where the decision is made and say where the other
// half lives (domain.ErrPaymentNotAuthorised).
func (s *Service) AcceptOrder(ctx context.Context, orgID, id string) (domain.Order, error) {
	return s.move(ctx, orgID, id, domain.StatusAccepted, "", ActionAccepted, func(ctx context.Context, tx Tx, o domain.Order) error {
		status, err := tx.PaymentStatus(ctx, orgID, o.ID)
		switch {
		case err != nil:
			return err
		case status != paymentAuthorised && status != paymentCaptured:
			return domain.ErrPaymentNotAuthorised
		}
		return nil
	})
}

// Payment statuses of the payments module's order_payments table that let a
// restaurant start cooking. They are that module's public API, repeated here
// as the only thing this module needs of it.
const (
	paymentAuthorised = "authorised"
	paymentCaptured   = "captured"
)

// docs:end accept-order

// RejectOrder is the restaurant turning an order down, with a reason the
// customer and the audit log both keep.
func (s *Service) RejectOrder(ctx context.Context, orgID, id, reason string) (domain.Order, error) {
	return s.move(ctx, orgID, id, domain.StatusRejected, reason, ActionRejected, nil)
}

// AdvanceOrder moves an order along the kitchen's own steps: preparing and
// ready. The state machine decides whether the step is possible; this only
// says who asked.
//
// When an order becomes ready and nobody is carrying it, the
// orders.courier_auto_assign flag decides whether the platform picks a
// courier itself. That flag is server-side: no client reads it, and turning
// it on is an operational decision, not a feature a customer sees.
func (s *Service) AdvanceOrder(ctx context.Context, orgID, id string, to domain.Status) (domain.Order, error) {
	if to != domain.StatusPreparing && to != domain.StatusReady {
		return domain.Order{}, domain.ErrInvalidTransition
	}
	return s.move(ctx, orgID, id, to, "", ActionAdvanced, func(ctx context.Context, tx Tx, o domain.Order) error {
		if to != domain.StatusReady || o.CourierID != "" {
			return nil
		}
		return s.autoAssignCourier(ctx, tx, &o)
	})
}
