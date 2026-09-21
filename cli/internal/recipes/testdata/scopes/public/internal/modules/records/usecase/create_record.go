package usecase

import (
	"context"

	"example.com/app/internal/modules/records/domain"
)

// CreateRecord adds a record, which everyone can then read. The route
// requires PermWrite: publishing is not public.
func (s *Service) CreateRecord(ctx context.Context, f domain.RecordFields) (domain.Record, error) {
	record, err := domain.NewRecord(s.newID(), f, s.clock())
	if err != nil {
		return domain.Record{}, err
	}
	created, err := s.store.InsertRecord(ctx, record)
	if err != nil {
		return domain.Record{}, storeError("create", err)
	}
	s.audit(ctx, ActionCreated, created.ID, nil)
	return created, nil
}
