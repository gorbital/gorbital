package usecase

import (
	"context"

	"example.com/app/internal/modules/records/domain"
)

// CreateRecord adds a record owned by the signed-in user.
func (s *Service) CreateRecord(ctx context.Context, f domain.RecordFields) (domain.Record, error) {
	owner, err := ownerID(ctx)
	if err != nil {
		return domain.Record{}, err
	}
	record, err := domain.NewRecord(s.newID(), owner, f, s.clock())
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
