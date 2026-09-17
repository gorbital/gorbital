package usecase

import (
	"context"

	"example.com/mobile-backend/internal/modules/trips/domain"
)

// docs:start add

// AddTrip stores a trip for the caller.
func (s *Service) AddTrip(ctx context.Context, f domain.Fields) (domain.Trip, error) {
	traveller, err := travellerID(ctx)
	if err != nil {
		return domain.Trip{}, err
	}
	t, err := domain.NewTrip(s.newID(), traveller, f, s.clock())
	if err != nil {
		return domain.Trip{}, err
	}
	added, err := s.store.InsertTrip(ctx, t)
	if err != nil {
		return domain.Trip{}, storeError("add", err)
	}
	s.audit(ctx, ActionAdded, added.ID)
	return added, nil
}

// docs:end add
