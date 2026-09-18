package usecase

import "context"

// DeleteItem removes one of the organisation orgID's menu items for good.
//
// Deleting is the rarer of the two ways a dish leaves a menu, and the
// sharper: it is for a dish that should never have existed, a typo or
// somebody else's recipe pasted in by mistake. A kitchen that has simply run
// out sets available to false instead, which keeps the dish, its price and
// its photo for tomorrow.
func (s *Service) DeleteItem(ctx context.Context, orgID, id string) error {
	if _, err := memberID(ctx, orgID); err != nil {
		return err
	}
	if err := s.store.DeleteItem(ctx, orgID, id); err != nil {
		return storeError("delete", err)
	}
	s.audit(ctx, ActionDeleted, id, nil)
	return nil
}
