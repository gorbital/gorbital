package usecase

import (
	"context"

	"example.com/plateful/internal/modules/orders/domain"
)

// CreateOrder adds an order to the organisation orgID, created by the member
// acting in it.
func (s *Service) CreateOrder(ctx context.Context, orgID string, f domain.OrderFields) (domain.Order, error) {
	member, err := memberID(ctx, orgID)
	if err != nil {
		return domain.Order{}, err
	}
	order, err := domain.NewOrder(s.newID(), orgID, member, f, s.clock())
	if err != nil {
		return domain.Order{}, err
	}
	created, err := s.store.InsertOrder(ctx, order)
	if err != nil {
		return domain.Order{}, storeError("create", err)
	}
	s.audit(ctx, ActionCreated, created.ID, nil)
	return created, nil
}
