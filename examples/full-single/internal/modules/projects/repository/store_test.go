package repository_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"apistock.dev/modules/postgres/pgtest"
	"apistock.dev/page"

	"example.com/acme-api/db/migrations"
	projectsdomain "example.com/acme-api/internal/modules/projects/domain"
	projectsrepository "example.com/acme-api/internal/modules/projects/repository"
	projectsusecase "example.com/acme-api/internal/modules/projects/usecase"
)

var now = time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

// newStore returns a store on a fresh database with the users usr_ada and
// usr_bob, and the database's pool.
func newStore(t *testing.T) (*projectsrepository.Store, *pgxpool.Pool) {
	t.Helper()
	pool := pgtest.New(t, pgtest.WithMigrations(migrations.FS))
	for _, id := range []string{"usr_ada", "usr_bob"} {
		_, err := pool.Exec(context.Background(),
			`INSERT INTO auth_users (id, email, email_normalized, created_at, updated_at) VALUES ($1, $2, $2, $3, $3)`,
			id, id+"@example.com", now)
		if err != nil {
			t.Fatal(err)
		}
	}
	return projectsrepository.NewStore(pool), pool
}

// sample returns a valid project with title as its name.
func sample(id, ownerID, title string, at time.Time) projectsdomain.Project {
	return projectsdomain.Project{
		ID: id, OwnerID: ownerID,
		ProjectFields: projectsdomain.ProjectFields{Name: title, Description: "Example description", Status: projectsdomain.StatusActive},
		Version:       1, CreatedAt: at, UpdatedAt: at,
	}
}

func TestProjects(t *testing.T) {
	store, _ := newStore(t)
	ctx := context.Background()

	created, err := store.InsertProject(ctx, sample("prj_1", "usr_ada", "Website", now))
	if err != nil || created.ID != "prj_1" || created.Version != 1 || !created.CreatedAt.Equal(now) {
		t.Fatalf("InsertProject() = %+v, %v", created, err)
	}
	taken := sample("prj_2", "usr_ada", "Other", now)
	taken.Name = "WEBSITE"
	if _, err := store.InsertProject(ctx, taken); !errors.Is(err, projectsdomain.ErrProjectNameTaken) {
		t.Errorf("InsertProject(same name, other case) error = %v, want ErrProjectNameTaken", err)
	}
	if _, err := store.InsertProject(ctx, sample("prj_3", "usr_bob", "Website", now)); err != nil {
		t.Errorf("InsertProject(same fields, another owner) error = %v", err)
	}
	if _, err := store.InsertProject(ctx, sample("prj_4", "usr_ada", "Docs", now)); err != nil {
		t.Fatal(err)
	}

	if p, err := store.SelectProject(ctx, "usr_ada", "prj_1", false); err != nil || p != created {
		t.Errorf("SelectProject() = %+v, %v, want %+v", p, err, created)
	}
	if _, err := store.SelectProject(ctx, "usr_bob", "prj_1", false); !errors.Is(err, projectsdomain.ErrProjectNotFound) {
		t.Errorf("SelectProject(another owner's) error = %v, want ErrProjectNotFound", err)
	}

	next := created
	next.Name, next.UpdatedAt = "Website v2", now.Add(time.Minute)
	next.Status = projectsdomain.StatusArchived
	updated, err := store.UpdateProject(ctx, next)
	if err != nil || updated.Version != 2 || updated.ProjectFields != next.ProjectFields ||
		!updated.UpdatedAt.Equal(next.UpdatedAt) || !updated.CreatedAt.Equal(now) {
		t.Fatalf("UpdateProject() = %+v, %v", updated, err)
	}
	if _, err := store.UpdateProject(ctx, next); !errors.Is(err, projectsdomain.ErrProjectVersionConflict) {
		t.Errorf("UpdateProject(stale version) error = %v, want ErrProjectVersionConflict", err)
	}
	stolen := updated
	stolen.OwnerID = "usr_bob"
	if _, err := store.UpdateProject(ctx, stolen); !errors.Is(err, projectsdomain.ErrProjectVersionConflict) {
		t.Errorf("UpdateProject(another owner) error = %v, want ErrProjectVersionConflict", err)
	}
	docs, err := store.SelectProject(ctx, "usr_ada", "prj_4", false)
	if err != nil {
		t.Fatal(err)
	}
	docs.Name = "website V2"
	if _, err := store.UpdateProject(ctx, docs); !errors.Is(err, projectsdomain.ErrProjectNameTaken) {
		t.Errorf("UpdateProject(taken name) error = %v, want ErrProjectNameTaken", err)
	}

	if err := store.DeleteProject(ctx, "usr_bob", "prj_1"); !errors.Is(err, projectsdomain.ErrProjectNotFound) {
		t.Errorf("DeleteProject(another owner's) error = %v, want ErrProjectNotFound", err)
	}
	if err := store.DeleteProject(ctx, "usr_ada", "prj_1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SelectProject(ctx, "usr_ada", "prj_1", false); !errors.Is(err, projectsdomain.ErrProjectNotFound) {
		t.Errorf("deleted project selected: %v", err)
	}
	if err := store.DeleteProject(ctx, "usr_ada", "prj_1"); !errors.Is(err, projectsdomain.ErrProjectNotFound) {
		t.Errorf("DeleteProject(deleted) error = %v, want ErrProjectNotFound", err)
	}
}

func TestSelectProjectsPages(t *testing.T) {
	store, _ := newStore(t)
	ctx := context.Background()
	// A minute apart, except the last two, which share a time to check that
	// the ID breaks ties. The last one has another status.
	for i, title := range []string{"beta", "Alpha", "gamma", "delta", "Epsilon"} {
		p := sample(fmt.Sprintf("prj_%d", i), "usr_ada", title, now.Add(time.Duration(min(i, 3))*time.Minute))
		if i == 4 {
			p.Status = projectsdomain.StatusArchived
		}
		if _, err := store.InsertProject(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.InsertProject(ctx, sample("prj_bob", "usr_bob", "zeta", now)); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		sort   page.SortField
		filter projectsdomain.Status
		want   []string
	}{
		{sort: page.SortField{Field: "created_at", Desc: true}, want: []string{"prj_4", "prj_3", "prj_2", "prj_1", "prj_0"}},
		{sort: page.SortField{Field: "created_at"}, want: []string{"prj_0", "prj_1", "prj_2", "prj_3", "prj_4"}},
		{sort: page.SortField{Field: "name"}, want: []string{"prj_1", "prj_0", "prj_3", "prj_4", "prj_2"}},
		{sort: page.SortField{Field: "name", Desc: true}, want: []string{"prj_2", "prj_4", "prj_3", "prj_0", "prj_1"}},
		{sort: page.SortField{Field: "updated_at"}, filter: projectsdomain.StatusActive, want: []string{"prj_0", "prj_1", "prj_2", "prj_3"}},
		{sort: page.SortField{Field: "updated_at"}, filter: projectsdomain.StatusArchived, want: []string{"prj_4"}},
	}
	for _, tt := range tests {
		var got []string
		var after *projectsusecase.Position
		for range 10 {
			items, err := store.SelectProjects(ctx, projectsusecase.ListQuery{OwnerID: "usr_ada", Status: tt.filter, Sort: tt.sort, After: after, Limit: 2})
			if err != nil {
				t.Fatalf("SelectProjects(%+v) error = %v", tt.sort, err)
			}
			for _, p := range items {
				got = append(got, p.ID)
			}
			if len(items) < 2 {
				break
			}
			last := items[len(items)-1]
			after = &projectsusecase.Position{ID: last.ID, Text: last.Name, Time: last.CreatedAt}
			if tt.sort.Field == "updated_at" {
				after.Time = last.UpdatedAt
			}
		}
		if !slices.Equal(got, tt.want) {
			t.Errorf("SelectProjects(%+v) pages = %v, want %v", tt, got, tt.want)
		}
	}

	if _, err := store.SelectProjects(ctx, projectsusecase.ListQuery{OwnerID: "usr_ada", Sort: page.SortField{Field: "owner_id"}, Limit: 2}); !errors.Is(err, page.ErrInvalidSort) {
		t.Errorf("SelectProjects(unknown sort) error = %v, want ErrInvalidSort", err)
	}
}

func TestDeletingAccountDeletesItsProjects(t *testing.T) {
	store, pool := newStore(t)
	ctx := context.Background()
	if _, err := store.InsertProject(ctx, sample("prj_1", "usr_ada", "Website", now)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM auth_users WHERE id = 'usr_ada'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SelectProject(ctx, "usr_ada", "prj_1", false); !errors.Is(err, projectsdomain.ErrProjectNotFound) {
		t.Errorf("project of a purged account selected: %v", err)
	}
}

func TestInTxRollsBack(t *testing.T) {
	store, _ := newStore(t)
	ctx := context.Background()
	errFail := errors.New("fail")
	err := store.InTx(ctx, func(tx projectsusecase.Store) error {
		if _, err := tx.InsertProject(ctx, sample("prj_1", "usr_ada", "Website", now)); err != nil {
			return err
		}
		return errFail
	})
	if !errors.Is(err, errFail) {
		t.Fatalf("InTx() error = %v", err)
	}
	if _, err := store.SelectProject(ctx, "usr_ada", "prj_1", false); !errors.Is(err, projectsdomain.ErrProjectNotFound) {
		t.Errorf("rolled back insert is visible: %v", err)
	}
}
