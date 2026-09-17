package usecase

import (
	"context"
	"errors"

	"example.com/mobile-backend/internal/modules/trips/domain"
)

// docs:start get

// GetTrip returns the caller's trip id, or domain.ErrTripNotFound, which is
// also the answer for another traveller's trip.
func (s *Service) GetTrip(ctx context.Context, id string) (domain.Trip, error) {
	traveller, err := travellerID(ctx)
	if err != nil {
		return domain.Trip{}, err
	}
	t, err := s.store.SelectTrip(ctx, traveller, id)
	if errors.Is(err, domain.ErrTripNotFound) {
		return domain.Trip{}, err
	}
	if err != nil {
		return domain.Trip{}, storeError("get", err)
	}
	return t, nil
}

// docs:end get
