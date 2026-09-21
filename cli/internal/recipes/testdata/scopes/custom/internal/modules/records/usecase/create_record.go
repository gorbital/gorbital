package usecase

import (
	"context"

	"example.com/app/internal/modules/records/domain"
)

// CreateRecord adds a record created by the signed-in caller, when the
// policy allows it.
func (s *Service) CreateRecord(ctx context.Context, f domain.RecordFields) (domain.Record, error) {
	creator, err := actorID(ctx)
	if err != nil {
		return domain.Record{}, err
	}
	record, err := domain.NewRecord(s.newID(), creator, f, s.clock())
	if err != nil {
		return domain.Record{}, err
	}
	if err := s.policy.CanWrite(ctx, record); err != nil {
		return domain.Record{}, err
	}
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
