package usecase

import (
	"context"
	"errors"

	"example.com/mobile-backend/internal/modules/trips/domain"
)

// DeleteTrip removes the caller's trip id, or returns
// domain.ErrTripNotFound.
func (s *Service) DeleteTrip(ctx context.Context, id string) error {
	traveller, err := travellerID(ctx)
	if err != nil {
		return err
	}
	switch err := s.store.DeleteTrip(ctx, traveller, id); {
	case errors.Is(err, domain.ErrTripNotFound):
		return err
	case err != nil:
		return storeError("delete", err)
	}
	s.audit(ctx, ActionDeleted, id)
	return nil
}
