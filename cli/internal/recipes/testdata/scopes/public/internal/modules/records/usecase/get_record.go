package usecase

import (
	"context"

	"example.com/app/internal/modules/records/domain"
)

// GetRecord returns one record, to anyone who asks.
func (s *Service) GetRecord(ctx context.Context, id string) (domain.Record, error) {
	record, err := s.store.SelectRecord(ctx, id, false)
	if err != nil {
		return domain.Record{}, storeError("get", err)
	}
	return record, nil
}
