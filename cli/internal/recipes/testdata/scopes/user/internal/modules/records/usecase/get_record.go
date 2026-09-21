package usecase

import (
	"context"

	"example.com/app/internal/modules/records/domain"
)

// GetRecord returns one of the signed-in user's records.
func (s *Service) GetRecord(ctx context.Context, id string) (domain.Record, error) {
	owner, err := ownerID(ctx)
	if err != nil {
		return domain.Record{}, err
	}
	record, err := s.store.SelectRecord(ctx, owner, id, false)
	if err != nil {
		return domain.Record{}, storeError("get", err)
	}
	return record, nil
}
