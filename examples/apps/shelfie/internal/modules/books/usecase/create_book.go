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
	if err := s.checkShelf(ctx, reader); err != nil {
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

// docs:start check-shelf

// checkShelf returns ErrShelfFull when reader's shelf holds as many books as
// the limit allows now. Two books added at the same moment can both pass:
// the limit keeps shelves reasonable, it isn't a quota to enforce exactly.
func (s *Service) checkShelf(ctx context.Context, reader string) error {
	if s.shelfLimit == nil {
		return nil
	}
	n, err := s.store.CountBooks(ctx, reader)
	if err != nil {
		return storeError("count", err)
	}
	if n >= s.shelfLimit.Get(ctx) {
		return domain.ErrShelfFull
	}
	return nil
}

// docs:end check-shelf
