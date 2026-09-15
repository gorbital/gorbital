package usecase

import (
	"context"

	projectdomain "gorbital.dev/spikes/openapi/internal/modules/projects/domain"
)

// ProjectRepository is the storage port the use cases need. Implementations
// must scope every read and write to orgID and return domain errors.
type ProjectRepository interface {
	Insert(ctx context.Context, p projectdomain.Project) error
	Get(ctx context.Context, orgID, id string) (projectdomain.Project, error)
	List(ctx context.Context, orgID string) ([]projectdomain.Project, error)
	Update(ctx context.Context, p projectdomain.Project) error
}
