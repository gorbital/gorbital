package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"example.com/app/internal/modules/records/domain"
)

const insertRecordSQL = `
	INSERT INTO records (id, owner_id, title, note, state, version, created_at, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	RETURNING ` + recordColumns

// InsertRecord stores a new record, or returns ErrRecordTitleTaken when the
// owner already uses the value, ignoring case.
func (s *Store) InsertRecord(ctx context.Context, record domain.Record) (domain.Record, error) {
	rows, err := s.db.Query(ctx, insertRecordSQL,
		record.ID, record.OwnerID, record.Title, record.Note, record.State, record.Version, record.CreatedAt, record.UpdatedAt)
	if err == nil {
		record, err = pgx.CollectExactlyOneRow(rows, scanRecord)
	}
	return record, constraintError(err)
}
