package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/app/internal/modules/records/domain"
)

const (
	selectRecordSQL = `SELECT ` + recordColumns + ` FROM records WHERE id = $1 AND org_id = $2`
	forUpdate       = ` FOR UPDATE`
)

// SelectRecord returns one of orgID's records, or ErrRecordNotFound.
// lock locks the row until the transaction ends.
func (s *Store) SelectRecord(ctx context.Context, orgID, id string, lock bool) (domain.Record, error) {
	sql := selectRecordSQL
	if lock {
		sql += forUpdate
	}
	rows, err := s.db.Query(ctx, sql, id, orgID)
	if err != nil {
		return domain.Record{}, err
	}
	record, err := pgx.CollectExactlyOneRow(rows, scanRecord)
	if postgres.IsNoRows(err) {
		return domain.Record{}, domain.ErrRecordNotFound
	}
	return record, err
}
