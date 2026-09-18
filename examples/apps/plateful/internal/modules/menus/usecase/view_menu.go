package usecase

import (
	"context"

	"example.com/plateful/internal/modules/menus/domain"
)

// docs:start view-menu

// ViewMenu returns the published menu of the restaurant restaurantID to a
// signed-in customer: its sections in order, each with the dishes that are
// available right now.
//
// guard.OrgMember would be the wrong guard on this route, and it is worth
// being precise about why. That guard asks one question — is the caller a
// member of the organisation in the path, with a role that holds the
// permission — and for a customer the answer is always no, because a
// customer belongs to no organisation at all. There is no role to give them
// and no organisation to give it in; putting them in every restaurant's
// organisation to let them read a menu would hand them the staff's routes
// too. So the route carries guard.Permission(PermBrowse), a platform
// permission the "user" role holds, and the rule about what a customer may
// read lives here instead.
//
// That rule is two steps. The restaurant is resolved first, and only if it
// is open: a restaurant that is onboarding, paused or suspended answers the
// same 404 restaurant_not_found as one that never existed, so nobody can
// find out that a restaurant was suspended by watching which IDs change
// their answer. Only then is its organisation's menu read, and only the
// available dishes, because the kitchen's switch is exactly what a customer
// should be told about.
func (s *Service) ViewMenu(ctx context.Context, restaurantID string) ([]domain.Section, error) {
	if _, err := callerID(ctx); err != nil {
		return nil, err
	}
	if err := s.store.SelectOpenRestaurant(ctx, restaurantID); err != nil {
		return nil, storeError("view menu", err)
	}
	// A restaurant is an organisation, so its ID is the one the menu
	// belongs to.
	items, err := s.store.SelectMenu(ctx, restaurantID)
	if err != nil {
		return nil, storeError("view menu", err)
	}
	return domain.GroupSections(items), nil
}

// docs:end view-menu
