package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	projectdomain "gorbital.dev/spikes/openapi/internal/modules/projects/domain"
	projectrepository "gorbital.dev/spikes/openapi/internal/modules/projects/repository"
	projectusecase "gorbital.dev/spikes/openapi/internal/modules/projects/usecase"
)

func newService() *projectusecase.Service {
	n := 0
	newID := func() string { n++; return "prj_" + string(rune('a'+n)) }
	now := func() time.Time { return time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC) }
	return projectusecase.NewService(projectrepository.NewMemory(), newID, now)
}

func TestCreateAndArchive(t *testing.T) {
	ctx := context.Background()
	svc := newService()

	p, err := svc.Create(ctx, "org_a", "  Website  ")
	if err != nil {
		t.Fatalf("Create(org_a, Website) error = %v", err)
	}
	if p.Name != "Website" {
		t.Errorf("Create(org_a, Website).Name = %q, want %q", p.Name, "Website")
	}

	if _, err := svc.Create(ctx, "org_a", "Website"); !errors.Is(err, projectdomain.ErrNameTaken) {
		t.Errorf("Create duplicate error = %v, want ErrNameTaken", err)
	}
	if _, err := svc.Create(ctx, "org_b", "Website"); err != nil {
		t.Errorf("Create same name in other org error = %v, want nil", err)
	}

	if _, err := svc.Archive(ctx, "org_a", p.ID); err != nil {
		t.Fatalf("Archive(%s) error = %v", p.ID, err)
	}
	if _, err := svc.Archive(ctx, "org_a", p.ID); !errors.Is(err, projectdomain.ErrAlreadyArchived) {
		t.Errorf("Archive twice error = %v, want ErrAlreadyArchived", err)
	}
	if _, err := svc.Get(ctx, "org_b", p.ID); !errors.Is(err, projectdomain.ErrNotFound) {
		t.Errorf("Get from other org error = %v, want ErrNotFound", err)
	}
}
