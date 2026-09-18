package usecase

import (
	"context"

	"example.com/plateful/internal/modules/payments/domain"
)

// GetOrderPayment returns the payment for the order orderID to the customer
// who placed it.
//
// The ownership check has the same shape as PayOrder's, and for the same
// reason: the route's guard.Permission(PermPay) says only that somebody is
// signed in. A payment of somebody else's order answers
// ErrPaymentNotFound — not "forbidden", which would confirm that it exists.
func (s *Service) GetOrderPayment(ctx context.Context, orderID string) (domain.Payment, error) {
	caller, err := callerID(ctx)
	if err != nil {
		return domain.Payment{}, err
	}
	payment, err := s.store.SelectPaymentByOrder(ctx, orderID, false)
	if err != nil {
		return domain.Payment{}, storeError("get", err)
	}
	if payment.CustomerID != caller {
		return domain.Payment{}, domain.ErrPaymentNotFound
	}
	return payment, nil
}
