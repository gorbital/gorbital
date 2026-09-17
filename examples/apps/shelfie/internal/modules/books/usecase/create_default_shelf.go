package usecase

import (
	"context"
	"fmt"

	"example.com/shelfie/internal/modules/books/domain"
)

// docs:start create-default-shelf

// CreateDefaultShelf gives a new reader the shelf every account starts
// with. It runs in the transaction that creates the account, so its store
// is bound to that transaction and an error rolls the account back.
func (s *Service) CreateDefaultShelf(ctx context.Context, ownerID string) error {
	shelf := domain.Shelf{ID: newShelfID(), OwnerID: ownerID, Name: domain.DefaultShelfName, CreatedAt: s.clock()}
	if err := s.store.InsertShelf(ctx, shelf); err != nil {
		return fmt.Errorf("books: create the default shelf: %w", err)
	}
	return nil
}

// docs:end create-default-shelf

// newShelfID returns "shf_" and 128 random bits.
func newShelfID() string { return "shf_" + newID()[len("bok_"):] }
