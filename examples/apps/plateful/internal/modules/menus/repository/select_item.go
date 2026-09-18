package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/menus/domain"
)

const (
	selectItemSQL = `SELECT ` + itemColumns + ` FROM menu_items WHERE org_id = $1 AND id = $2`
	forUpdate     = ` FOR UPDATE`
)

// SelectItem returns one of the organisation's items, or ErrItemNotFound.
// The organisation is part of the key, not a filter applied afterwards, so
// another organisation's item is simply not found. lock locks the row until
// the transaction ends.
func (s *Store) SelectItem(ctx context.Context, orgID, id string, lock bool) (domain.Item, error) {
	sql := selectItemSQL
	if lock {
		sql += forUpdate
	}
	rows, err := s.db.Query(ctx, sql, orgID, id)
	if err != nil {
		return domain.Item{}, err
	}
	item, err := pgx.CollectExactlyOneRow(rows, scanItem)
	if postgres.IsNoRows(err) {
		return domain.Item{}, domain.ErrItemNotFound
	}
	return item, err
}
