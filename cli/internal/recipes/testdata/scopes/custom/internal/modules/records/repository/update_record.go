package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/app/internal/modules/records/domain"
)

const updateRecordSQL = `
	UPDATE records
	SET title = $2, note = $3, state = $4, updated_at = $5, version = version + 1
	WHERE id = $1 AND version = $6
	RETURNING ` + recordColumns

// UpdateRecord saves record when the stored version is still record.Version and
// returns it with the next version. It returns ErrRecordVersionConflict when
// no row has that version (changed, deleted), and
// ErrRecordTitleTaken.
func (s *Store) UpdateRecord(ctx context.Context, record domain.Record) (domain.Record, error) {
	rows, err := s.db.Query(ctx, updateRecordSQL,
		record.ID, record.Title, record.Note, record.State, record.UpdatedAt, record.Version)
	if err != nil {
		return domain.Record{}, constraintError(err)
	}
	updated, err := pgx.CollectExactlyOneRow(rows, scanRecord)
	if postgres.IsNoRows(err) {
		return domain.Record{}, domain.ErrRecordVersionConflict
	}
	return updated, constraintError(err)
}
