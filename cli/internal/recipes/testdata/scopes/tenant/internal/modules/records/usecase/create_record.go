package usecase

import (
	"context"

	"example.com/app/internal/modules/records/domain"
)

// CreateRecord adds a record to the organisation orgID, created by the member
// acting in it.
func (s *Service) CreateRecord(ctx context.Context, orgID string, f domain.RecordFields) (domain.Record, error) {
	member, err := memberID(ctx, orgID)
	if err != nil {
		return domain.Record{}, err
	}
	record, err := domain.NewRecord(s.newID(), orgID, member, f, s.clock())
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
