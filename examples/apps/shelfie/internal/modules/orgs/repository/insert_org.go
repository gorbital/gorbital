package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	orgsdomain "example.com/shelfie/internal/modules/orgs/domain"
)

const insertOrgSQL = `
	INSERT INTO orgs AS o (id, name, personal, created_by, version, created_at, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7)
	RETURNING ` + orgColumns

// InsertOrg creates an organisation.
func (s *Store) InsertOrg(ctx context.Context, o orgsdomain.Org) (orgsdomain.Org, error) {
	rows, err := s.db.Query(ctx, insertOrgSQL, o.ID, o.Name, o.Personal, o.CreatedBy, o.Version, o.CreatedAt, o.UpdatedAt)
	if err != nil {
		return orgsdomain.Org{}, err
	}
	return pgx.CollectExactlyOneRow(rows, scanOrg)
}
