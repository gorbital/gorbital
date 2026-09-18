package usecase

import (
	"context"

	"example.com/plateful/internal/modules/restaurants/domain"
)

// GetRestaurant returns one of the organisation orgID's restaurants.
func (s *Service) GetRestaurant(ctx context.Context, orgID, id string) (domain.Restaurant, error) {
	if _, err := memberID(ctx, orgID); err != nil {
		return domain.Restaurant{}, err
	}
	restaurant, err := s.store.SelectRestaurant(ctx, orgID, id, false)
	if err != nil {
		return domain.Restaurant{}, storeError("get", err)
	}
	return restaurant, nil
}
