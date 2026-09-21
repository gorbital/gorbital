package usecase

import (
	"context"

	"example.com/shelfie/internal/modules/shelves/domain"
)

// CreateShelf adds a shelf owned by the signed-in user.
func (s *Service) CreateShelf(ctx context.Context, f domain.ShelfFields) (domain.Shelf, error) {
	owner, err := ownerID(ctx)
	if err != nil {
		return domain.Shelf{}, err
	}
	shelf, err := domain.NewShelf(s.newID(), owner, f, s.clock())
	if err != nil {
		return domain.Shelf{}, err
	}
	created, err := s.store.InsertShelf(ctx, shelf)
	if err != nil {
		return domain.Shelf{}, storeError("create", err)
	}
	s.audit(ctx, ActionCreated, created.ID, nil)
	return created, nil
}
