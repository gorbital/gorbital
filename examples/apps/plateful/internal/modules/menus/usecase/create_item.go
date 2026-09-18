package usecase

import (
	"context"

	"example.com/plateful/internal/modules/menus/domain"
)

// docs:start create-item

// CreateItem adds a dish to the organisation orgID's menu.
//
// A new dish is available by default: a restaurant adds it because it means
// to sell it, and the kitchen switches it off later when it runs out.
// Nothing here creates a section — the dish carries its section's name, and
// a heading nobody has used before starts existing the moment this row is
// stored.
//
// The price arrives as minor units (950 for £9.50) and stays an int64 from
// the request body to the bigint column, because a float cannot represent
// 0.10 exactly and a bill has to add up.
func (s *Service) CreateItem(ctx context.Context, orgID string, f domain.ItemFields) (domain.Item, error) {
	member, err := memberID(ctx, orgID)
	if err != nil {
		return domain.Item{}, err
	}
	item, err := domain.NewItem(s.newID(), orgID, member, f, s.clock())
	if err != nil {
		return domain.Item{}, err
	}
	created, err := s.store.InsertItem(ctx, item)
	if err != nil {
		return domain.Item{}, storeError("create", err)
	}
	s.audit(ctx, ActionCreated, created.ID, map[string]any{"section": created.Section})
	return created, nil
}

// docs:end create-item
