package usecase

import (
	"context"

	"example.com/plateful/internal/modules/menus/domain"
)

// GetItem returns one of the organisation orgID's menu items, as its staff
// see it: whether or not the kitchen has it available today.
func (s *Service) GetItem(ctx context.Context, orgID, id string) (domain.Item, error) {
	if _, err := memberID(ctx, orgID); err != nil {
		return domain.Item{}, err
	}
	item, err := s.store.SelectItem(ctx, orgID, id, false)
	if err != nil {
		return domain.Item{}, storeError("get", err)
	}
	return item, nil
}
