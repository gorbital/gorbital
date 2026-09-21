package usecase

import (
	"context"

	"example.com/shelfie/internal/modules/partners/domain"
)

// MaxPurchases is how many purchases the list returns.
const MaxPurchases = 100

// ListPurchases returns the signed-in reader's purchases, newest first.
func (s *Service) ListPurchases(ctx context.Context) ([]domain.Purchase, error) {
	reader, err := readerID(ctx)
	if err != nil {
		return nil, err
	}
	list, err := s.store.SelectPurchases(ctx, reader, MaxPurchases)
	if err != nil {
		return nil, storeError("list", err)
	}
	return list, nil
}
