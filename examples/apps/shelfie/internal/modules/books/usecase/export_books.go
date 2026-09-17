package usecase

import (
	"context"

	"example.com/shelfie/internal/modules/books/domain"
)

// ExportLimit is how many books one export holds: more than any shelf the
// books.shelf_limit setting allows.
const ExportLimit = 100_000

// ExportBooks returns every book on the signed-in reader's shelf, newest
// first, for the export Shelfie Plus unlocks.
func (s *Service) ExportBooks(ctx context.Context) ([]domain.Book, error) {
	reader, err := readerID(ctx)
	if err != nil {
		return nil, err
	}
	books, err := s.store.SelectBooks(ctx, ListQuery{OwnerID: reader, Limit: ExportLimit})
	if err != nil {
		return nil, storeError("export", err)
	}
	return books, nil
}
