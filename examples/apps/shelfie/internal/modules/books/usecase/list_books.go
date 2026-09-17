package usecase

import (
	"context"

	"example.com/shelfie/internal/modules/books/domain"
)

// Page sizes of ListBooks.
const (
	DefaultLimit = 20
	MaxLimit     = 100
)

// ListBooks returns the signed-in reader's books, newest first: up to limit
// (DefaultLimit when zero, at most MaxLimit), with status when it isn't
// empty.
func (s *Service) ListBooks(ctx context.Context, status domain.Status, limit int) ([]domain.Book, error) {
	reader, err := readerID(ctx)
	if err != nil {
		return nil, err
	}
	if status != "" && !status.Valid() {
		return nil, domain.ErrInvalidStatus
	}
	if limit <= 0 {
		limit = DefaultLimit
	}
	books, err := s.store.SelectBooks(ctx, ListQuery{OwnerID: reader, Status: status, Limit: min(limit, MaxLimit)})
	if err != nil {
		return nil, storeError("list", err)
	}
	return books, nil
}
