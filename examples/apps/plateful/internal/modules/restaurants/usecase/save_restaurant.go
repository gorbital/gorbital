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
// gorbital has no hook for "an organisation was created" — orgshttp gives a
// new account its personal workspace and creates organisations on request,
// without telling the app's modules — so the profile can't appear by itself
// when the restaurateur signs up. It appears the first time they save one,
// with version 0, and starts onboarding: nothing can be ordered from it
// until its staff publish it.
func (s *Service) SaveRestaurant(ctx context.Context, orgID string, in SaveRestaurantInput) (domain.Restaurant, error) {
	member, err := memberID(ctx, orgID)
	if err != nil {
		return domain.Restaurant{}, err
	}
	var saved domain.Restaurant
	created := false
	err = s.store.InTx(ctx, func(tx Store) error {
		current, err := tx.SelectRestaurantByOrg(ctx, orgID, true)
		switch {
		case errors.Is(err, domain.ErrRestaurantNotFound):
			if in.Version != 0 {
				return domain.ErrRestaurantVersionConflict
			}
			r, err := domain.NewRestaurant(s.newID(), orgID, member, in.Fields, s.limit(ctx), s.clock())
			if err != nil {
				return err
			}
			created = true
			saved, err = tx.InsertRestaurant(ctx, r)
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
