package usecase

import (
	"context"

	"example.com/shelfie/internal/modules/books/domain"
)

// GetBook returns one of the signed-in reader's books.
func (s *Service) GetBook(ctx context.Context, id string) (domain.Book, error) {
	reader, err := readerID(ctx)
	if err != nil {
		return domain.Book{}, err
	}
	book, err := s.store.SelectBook(ctx, reader, id)
	if err != nil {
		return domain.Book{}, storeError("get", err)
	}
	return book, nil
}
