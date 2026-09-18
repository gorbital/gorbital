package usecase

import (
	"context"
	"errors"

	"example.com/plateful/internal/modules/restaurants/domain"
)

// SaveRestaurantInput is the profile the restaurant's staff send. Version is
// the version they read, or 0 to create the profile the organisation
// doesn't have yet.
type SaveRestaurantInput struct {
	Version int64
	Fields  domain.RestaurantFields
	Status  domain.Status
}

// docs:start save-restaurant

// SaveRestaurant creates or replaces the organisation orgID's restaurant
// profile.
//
// The restaurant is the organisation's own row, so it exists as soon as the
// organisation does, but not every organisation is a restaurant yet: a new
// account's personal workspace is one too. An organisation becomes a
// restaurant the first time its staff save a profile, with version 0; it
// starts onboarding, and nothing can be ordered from it until they publish
// it. After that, the version is the organisation's.
func (s *Service) SaveRestaurant(ctx context.Context, orgID string, in SaveRestaurantInput) (domain.Restaurant, error) {
	if _, err := memberID(ctx, orgID); err != nil {
		return domain.Restaurant{}, err
	}
	var saved domain.Restaurant
	created := false
	err := s.store.InTx(ctx, func(tx Store) error {
		current, err := tx.SelectRestaurant(ctx, orgID, true)
		switch {
		case errors.Is(err, domain.ErrRestaurantNotFound):
			if in.Version != 0 {
				return domain.ErrRestaurantVersionConflict
			}
			r, err := domain.NewRestaurant(orgID, in.Fields, s.limit(ctx), s.clock())
			if err != nil {
				return err
			}
			created = true
			saved, err = tx.CreateRestaurant(ctx, r)
			return err
		case err != nil:
			return err
		case current.Version != in.Version:
			return domain.ErrRestaurantVersionConflict
		}
		next, changed, err := current.Apply(in.Fields, in.Status, s.limit(ctx), s.clock())
		if err != nil {
			return err
		}
		if len(changed) == 0 {
			saved = current
			return nil
		}
		saved, err = tx.UpdateRestaurant(ctx, next)
		return err
	})
	if err != nil {
		return domain.Restaurant{}, storeError("save", err)
	}
	action := ActionUpdated
	if created {
		action = ActionCreated
	}
	s.audit(ctx, action, saved.ID, map[string]any{"status": string(saved.Status)})
	return saved, nil
}

// docs:end save-restaurant
