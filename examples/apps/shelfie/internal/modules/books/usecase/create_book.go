package usecase

import (
	"context"

	"example.com/shelfie/internal/modules/books/domain"
)

// docs:start create-book

// CreateBook adds a book to the signed-in reader's shelf.
func (s *Service) CreateBook(ctx context.Context, f domain.Fields) (domain.Book, error) {
	reader, err := readerID(ctx)
	if err != nil {
		return domain.Book{}, err
	}
	book, err := domain.NewBook(s.newID(), reader, f, s.clock())
	if err != nil {
		return domain.Book{}, err
	}
	created, err := s.store.InsertBook(ctx, book)
	if err != nil {
		return domain.Book{}, storeError("create", err)
	}
	s.audit(ctx, ActionCreated, created.ID)
	return created, nil
}

// docs:end create-book
