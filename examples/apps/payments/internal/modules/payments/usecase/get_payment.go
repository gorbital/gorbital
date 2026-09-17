package usecase

import (
	"context"
	"errors"

	"example.com/payments/internal/modules/payments/domain"
)

// GetPayment returns the payment with the ID, or domain.ErrPaymentNotFound.
func (s *Service) GetPayment(ctx context.Context, id string) (domain.Payment, error) {
	payment, err := s.store.SelectPayment(ctx, id)
	switch {
	case errors.Is(err, domain.ErrPaymentNotFound):
		return domain.Payment{}, err
	case err != nil:
		return domain.Payment{}, storeError("get", err)
	}
	return payment, nil
}
