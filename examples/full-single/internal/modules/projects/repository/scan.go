package repository

import (
	"github.com/jackc/pgx/v5"

	projectsdomain "example.com/acme-api/internal/modules/projects/domain"
)

const projectColumns = `id, owner_id, name, description, status, version, created_at, updated_at`

func scanProject(row pgx.CollectableRow) (projectsdomain.Project, error) {
	var p projectsdomain.Project
	var status string
	err := row.Scan(&p.ID, &p.OwnerID, &p.Name, &p.Description, &status, &p.Version, &p.CreatedAt, &p.UpdatedAt)
	p.Status = projectsdomain.Status(status)
	p.CreatedAt, p.UpdatedAt = p.CreatedAt.UTC(), p.UpdatedAt.UTC()
	return p, err
}
