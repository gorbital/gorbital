package usecase

import (
	"context"

	"example.com/app/internal/modules/records/domain"
)

// GetRecord returns one of the organisation orgID's records.
func (s *Service) GetRecord(ctx context.Context, orgID, id string) (domain.Record, error) {
	if _, err := memberID(ctx, orgID); err != nil {
		return domain.Record{}, err
	}
	record, err := s.store.SelectRecord(ctx, orgID, id, false)
	if err != nil {
		return domain.Record{}, storeError("get", err)
	}
	return record, nil
}
