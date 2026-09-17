package usecase

import (
	"context"

	"example.com/shelfie/internal/modules/shelves/domain"
)

// GetShelf returns one of the signed-in user's shelves.
func (s *Service) GetShelf(ctx context.Context, id string) (domain.Shelf, error) {
	owner, err := ownerID(ctx)
	if err != nil {
		return domain.Shelf{}, err
	}
	shelf, err := s.store.SelectShelf(ctx, owner, id, false)
	if err != nil {
		return domain.Shelf{}, storeError("get", err)
	}
	return shelf, nil
}
