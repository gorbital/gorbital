package usecase

import (
	"context"

	"example.com/plateful/internal/modules/restaurants/domain"
)

// GetRestaurant returns the organisation orgID's own restaurant, as its
// staff see it: the suspension reason included.
func (s *Service) GetRestaurant(ctx context.Context, orgID string) (domain.Restaurant, error) {
	if _, err := memberID(ctx, orgID); err != nil {
		return domain.Restaurant{}, err
	}
	r, err := s.store.SelectRestaurantByOrg(ctx, orgID, false)
	if err != nil {
		return domain.Restaurant{}, storeError("get", err)
	}
	return r, nil
}

// docs:start view-restaurant

// ViewRestaurant returns one restaurant to a signed-in customer, who
// belongs to no organisation at all.
//
// guard.OrgMember would be the wrong guard here: it asks whether the caller
// is a member of the organisation in the path, and a customer is a member
// of none. The route uses guard.Permission(PermBrowse), which every
// signed-in account holds through the "user" role, and the rule about which
// restaurants a customer may see is here instead: one that isn't open isn't
// theirs to look at, and gets the same 404 as one that doesn't exist, so a
// suspension can't be detected by probing IDs.
func (s *Service) ViewRestaurant(ctx context.Context, id string) (domain.Restaurant, error) {
	if _, err := callerID(ctx); err != nil {
		return domain.Restaurant{}, err
	}
	r, err := s.store.SelectRestaurant(ctx, id, false)
	if err != nil {
		return domain.Restaurant{}, storeError("view", err)
	}
	if r.Status != domain.StatusOpen {
		return domain.Restaurant{}, domain.ErrRestaurantNotFound
	}
	r.SuspendedReason = "" // never a customer's business
	return r, nil
}

// docs:end view-restaurant
