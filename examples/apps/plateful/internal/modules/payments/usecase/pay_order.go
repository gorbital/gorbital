package usecase

import (
	"context"
	"errors"

	"gorbital.dev/audit"

	"example.com/plateful/internal/modules/payments/domain"
)

// docs:start pay-order

// PayOrder pays for the order orderID on behalf of the caller, and reports
// whether this call was the one that created the payment.
//
// The ownership check is here rather than in a guard, and that is the whole
// shape of a customer route. A customer belongs to no organisation, so
// guard.OrgMember has nothing to ask about them; the route's
// guard.Permission(PermPay) only says "some signed-in account", which every
// account is. What makes this order the caller's is a column in the orders
// table, and a guard can't read the database for a row the route names —
// guard.Request sees the path, the headers and the query, and nothing else.
// So the rule lives where the row is read, and an order belonging to
// somebody else answers ErrOrderNotFound, the same as one that doesn't
// exist, so order IDs can't be probed by paying for them.
//
// The operation is idempotent without any help from the client: an order
// that already has a pending payment gets that payment back rather than a
// second one, and the response is a 201 either way. The Idempotency-Key
// header is the client's own retry story on top of that, and gorbital's
// middleware handles it for every POST without this module doing anything.
func (s *Service) PayOrder(ctx context.Context, orderID string) (domain.Payment, bool, error) {
	caller, err := callerID(ctx)
	if err != nil {
		return domain.Payment{}, false, err
	}

	var paid domain.Payment
	created := false
	err = s.store.InTx(ctx, func(tx Store) error {
		order, err := tx.SelectOrder(ctx, orderID)
		if err != nil {
			return err
		}
		// Not the caller's order: the same answer as an order that isn't
		// there, so nothing is learned by asking.
		if order.CustomerID != caller {
			return domain.ErrOrderNotFound
		}

		switch existing, err := tx.SelectPaymentByOrder(ctx, orderID, true); {
		case err == nil && existing.Status == domain.StatusPending:
			paid = existing // already asked for; ask the provider once only
			return nil
		case err == nil:
			// Authorised, captured, refunded or failed: this order's money
			// has moved on, and a second attempt isn't a retry of the first.
			return domain.ErrPaymentNotPayable
		case !errors.Is(err, domain.ErrPaymentNotFound):
			return err
		}

		if !order.Payable() {
			return domain.ErrPaymentNotPayable
		}
		ref, err := s.provider.Pay(ctx, PayRequest{
			OrderID: orderID, CustomerID: caller,
			AmountMinor: order.TotalMinor, Currency: order.Currency,
		})
		if err != nil {
			return err
		}
		payment, err := domain.NewPayment(s.newID(), order.OrgID, orderID, caller,
			order.TotalMinor, order.Currency, ref, s.clock())
		if err != nil {
			return err
		}
		created = true
		paid, err = tx.InsertPayment(ctx, payment)
		return err
	})
	if err != nil {
		return domain.Payment{}, false, storeError("pay", err)
	}
	if created {
		// The organisation is the restaurant's, not the customer's: the
		// customer is in none, so the recorder has nothing to copy.
		s.audit(ctx, audit.Event{
			Action: ActionCreated, ResourceID: paid.ID, OrgID: paid.OrgID,
			Metadata: map[string]any{"order_id": paid.OrderID, "amount_minor": paid.AmountMinor, "currency": paid.Currency},
		})
	}
	return paid, created, nil
}

// docs:end pay-order
