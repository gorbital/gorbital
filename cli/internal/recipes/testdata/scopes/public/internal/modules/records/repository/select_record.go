package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/app/internal/modules/records/domain"
)

const (
	selectRecordSQL = `SELECT ` + recordColumns + ` FROM records WHERE id = $1`
	forUpdate       = ` FOR UPDATE`
)

// SelectRecord returns the record with this ID, or ErrRecordNotFound. lock
// locks the row until the transaction ends.
func (s *Store) SelectRecord(ctx context.Context, id string, lock bool) (domain.Record, error) {
	sql := selectRecordSQL
	if lock {
		sql += forUpdate
	}
	rows, err := s.db.Query(ctx, sql, id)
	if err != nil {
		return domain.Record{}, err
	}
	record, err := pgx.CollectExactlyOneRow(rows, scanRecord)
	if postgres.IsNoRows(err) {
		return domain.Record{}, domain.ErrRecordNotFound
	}
	return record, err
}
