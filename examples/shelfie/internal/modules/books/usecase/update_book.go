package usecase

import (
	"context"

	"example.com/shelfie/internal/modules/books/domain"
)

// UpdateBook changes one of the signed-in reader's books.
func (s *Service) UpdateBook(ctx context.Context, id string, c domain.Changes) (domain.Book, error) {
	reader, err := readerID(ctx)
	if err != nil {
		return domain.Book{}, err
	}
	book, err := s.store.SelectBook(ctx, reader, id)
	if err != nil {
		return domain.Book{}, storeError("update", err)
	}
	if book, err = book.Apply(c, s.clock()); err != nil {
		return domain.Book{}, err
	}
	updated, err := s.store.UpdateBook(ctx, book)
	if err != nil {
		return domain.Book{}, storeError("update", err)
	}
	s.audit(ctx, ActionUpdated, updated.ID)
	return updated, nil
}
