package usecase

import (
	"context"

	"example.com/plateful/internal/modules/orders/domain"
)

// GetOrder returns one of the organisation orgID's orders.
func (s *Service) GetOrder(ctx context.Context, orgID, id string) (domain.Order, error) {
	if _, err := memberID(ctx, orgID); err != nil {
		return domain.Order{}, err
	}
	order, err := s.store.SelectOrder(ctx, orgID, id, false)
	if err != nil {
		return domain.Order{}, storeError("get", err)
	}
	return order, nil
}
