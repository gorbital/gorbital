package usecase

import (
	"context"

	"example.com/shelfie/internal/modules/books/domain"
)

// ListShelves returns the signed-in reader's shelves, oldest first.
func (s *Service) ListShelves(ctx context.Context) ([]domain.Shelf, error) {
	reader, err := readerID(ctx)
	if err != nil {
		return nil, err
	}
	shelves, err := s.store.SelectShelves(ctx, reader)
	if err != nil {
		return nil, storeError("list shelves", err)
	}
	return shelves, nil
}
