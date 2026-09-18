package usecase

import (
	"context"

	"example.com/plateful/internal/modules/restaurants/domain"
)

// CreateRestaurant adds a restaurant to the organisation orgID, created by the member
// acting in it.
func (s *Service) CreateRestaurant(ctx context.Context, orgID string, f domain.RestaurantFields) (domain.Restaurant, error) {
	member, err := memberID(ctx, orgID)
	if err != nil {
		return domain.Restaurant{}, err
	}
	restaurant, err := domain.NewRestaurant(s.newID(), orgID, member, f, s.clock())
	if err != nil {
		return domain.Restaurant{}, err
	}
	created, err := s.store.InsertRestaurant(ctx, restaurant)
	if err != nil {
		return domain.Restaurant{}, storeError("create", err)
	}
	s.audit(ctx, ActionCreated, created.ID, nil)
	return created, nil
}
