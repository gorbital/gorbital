package usecase

import (
	"context"

	"gorbital.dev/audit"

	"example.com/plateful/internal/modules/payments/domain"
)

// docs:start refund-payment

// RefundPayment gives a customer their money back and records why.
//
// This is the platform's operation, not a tenant's and not a customer's:
// the route carries no organisation (/v1/platform/payments/{id}/refund),
// guard.Permission(PermRefund) admits only the platform_admin role, and
// guard.RecentReauth() means a stolen session that has been idle can't
// spend the platform's money. The domain decides what may be refunded — a
// payment that never took anything, and one already given back, are both
// ErrPaymentNotRefundable — so a second refund of the same payment is
// refused by the same rule that refuses the first impossible one.
//
// The provider is asked inside the transaction, so a provider that refuses
// leaves no row claiming the money came back. A provider that succeeds and
// a commit that then fails is the other way round, and is what the
// provider's own refund event is for.
func (s *Service) RefundPayment(ctx context.Context, id, reason string) (domain.Payment, error) {
	if _, err := callerID(ctx); err != nil {
		return domain.Payment{}, err
	}
	var err error
	var refunded domain.Payment
	err = s.store.InTx(ctx, func(tx Store) error {
		current, err := tx.SelectPayment(ctx, id, true)
		if err != nil {
			return err
		}
		next, err := current.Refund(s.clock())
		if err != nil {
			return err
		}
		if err := s.provider.Refund(ctx, current.ProviderRef, reason); err != nil {
			return err
		}
		refunded, err = tx.UpdatePayment(ctx, next)
		return err
	})
	if err != nil {
		return domain.Payment{}, storeError("refund", err)
	}
	// The actor is the operator, whom the recorder copies from the request,
	// but the organisation still has to be set by hand: platform staff act
	// in none, and the payment's is the restaurant's. The reason is what an
	// operator wrote about a customer's money, so the event keeps it: that
	// is the record of why the refund was made.
	s.audit(ctx, audit.Event{
		Action: ActionRefunded, ResourceID: refunded.ID, OrgID: refunded.OrgID,
		Metadata: map[string]any{"order_id": refunded.OrderID, "amount_minor": refunded.AmountMinor, "reason": reason},
	})
	return refunded, nil
}

// docs:end refund-payment
