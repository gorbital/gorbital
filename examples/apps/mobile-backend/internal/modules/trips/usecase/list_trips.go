package usecase

import (
	"context"

	"example.com/mobile-backend/internal/modules/trips/domain"
)

// ListTrips returns the caller's trips, newest first, up to MaxList.
func (s *Service) ListTrips(ctx context.Context) ([]domain.Trip, error) {
	traveller, err := travellerID(ctx)
	if err != nil {
		return nil, err
	}
	list, err := s.store.SelectTrips(ctx, traveller, MaxList)
	if err != nil {
		return nil, storeError("list", err)
	}
	return list, nil
}
