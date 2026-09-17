package orgshttp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/actor"
	"gorbital.dev/audit"
	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/flags"
	orgslib "gorbital.dev/modules/orgs"
	"gorbital.dev/modules/postgres"
)

// testMigrations are the test app's own migrations: the projects table, one
// per organisation, as `orb gen module --org` writes it.
var testMigrations = fstest.MapFS{
	"20260930000001_projects.sql": {Data: []byte(`-- +goose Up
CREATE TABLE projects (
    id          text        PRIMARY KEY,
    org_id      text        NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    created_by  text        NOT NULL,
    name        text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    description text        NOT NULL DEFAULT '',
    version     bigint      NOT NULL DEFAULT 1,
    created_at  timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL,
    UNIQUE (org_id, id)
);
CREATE UNIQUE INDEX projects_org_name ON projects (org_id, lower(name));

-- +goose Down
DROP TABLE projects;
`)},
}

// The test projects' permissions, held by every organisation role.
const (
	permProjectsRead  = "projects.project.read"
	permProjectsWrite = "projects.project.write"
)

var (
	errProjectNotFound  = errors.New("projects: not found")
	errProjectNameTaken = errors.New("projects: name taken")
	errProjectConflict  = errors.New("projects: version conflict")
)

// project is a project as the API returns it.
type project struct {
	ID          string    `json:"id"`
	OrgID       string    `json:"org_id"`
	CreatedBy   string    `json:"created_by"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Version     int64     `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
}

type projectOutput struct{ Body project }

type projectListOutput struct {
	Body struct {
		Items []project `json:"items"`
	}
}

type orgPath struct {
	OrgID string `path:"orgId" maxLength:"64"`
}

type projectPath struct {
	OrgID string `path:"orgId" maxLength:"64"`
	ID    string `path:"id" maxLength:"64"`
}

type createProjectInput struct {
	OrgID string `path:"orgId" maxLength:"64"`
	Body  struct {
		Name        string `json:"name" minLength:"1" maxLength:"100"`
		Description string `json:"description,omitempty" maxLength:"500"`
	}
}

type updateProjectInput struct {
	OrgID string `path:"orgId" maxLength:"64"`
	ID    string `path:"id" maxLength:"64"`
	Body  struct {
		Name    string `json:"name" minLength:"1" maxLength:"100"`
		Version int64  `json:"version" minimum:"1"`
	}
}

// testProjects is a small organisation-scoped module: every route is
// guarded by guard.OrgMember, and every query names the organisation, so
// isolation holds with and without row-level security.
func testProjects() gorbital.Module {
	all := []string{orgslib.RoleOwner, orgslib.RoleAdmin, orgslib.RoleMember}
	return gorbital.Module{
		Name: "projects",
		Errors: []httpx.Mapping{
			{Err: errProjectNotFound, Status: http.StatusNotFound, Code: "project_not_found", Detail: "the organisation has no project with this ID"},
			{Err: errProjectNameTaken, Status: http.StatusConflict, Code: "project_name_taken", Detail: "the organisation has a project with this name"},
			{Err: errProjectConflict, Status: http.StatusConflict, Code: "project_version_conflict", Detail: "the project changed since you read it"},
		},
		Permissions: []gorbital.Permission{
			{Name: permProjectsRead, Description: "See projects", OrgRoles: all},
			{Name: permProjectsWrite, Description: "Create, change and delete projects", OrgRoles: all},
		},
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			p := projects{db: d.DB, audit: d.Audit}
			g := r.Group("/v1/orgs/{orgId}/projects", gorbital.Tags("Projects"))
			gorbital.Post(g, "", p.create, gorbital.Status(http.StatusCreated), guard.OrgMember(permProjectsWrite))
			gorbital.Get(g, "", p.list, guard.OrgMember(permProjectsRead))
			gorbital.Get(g, "/{id}", p.get, guard.OrgMember(permProjectsRead))
			gorbital.Patch(g, "/{id}", p.update, guard.OrgMember(permProjectsWrite))
			gorbital.Delete(g, "/{id}", p.delete, gorbital.Status(http.StatusNoContent), guard.OrgMember(permProjectsWrite))
		},
	}
}

type projects struct {
	db    *pgxpool.Pool
	audit audit.Recorder
}

const projectColumns = `id, org_id, created_by, name, description, version, created_at`

func scanProject(row pgx.Row) (project, error) {
	var p project
	err := row.Scan(&p.ID, &p.OrgID, &p.CreatedBy, &p.Name, &p.Description, &p.Version, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, errProjectNotFound
	}
	return p, err
}

func (p projects) create(ctx context.Context, in *createProjectInput) (*projectOutput, error) {
	a, _ := actor.From(ctx)
	id := make([]byte, 8)
	_, _ = rand.Read(id)
	row := p.db.QueryRow(ctx, `INSERT INTO projects (id, org_id, created_by, name, description, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, now(), now()) RETURNING `+projectColumns,
		"prj_"+hex.EncodeToString(id), in.OrgID, a.ID, in.Body.Name, in.Body.Description)
	created, err := scanProject(row)
	if _, taken := postgres.UniqueViolation(err); taken {
		return nil, errProjectNameTaken
	}
	if err != nil {
		return nil, err
	}
	_ = p.audit.Record(ctx, audit.Event{Action: "projects.project.created", ResourceType: "project", ResourceID: created.ID, Outcome: audit.OutcomeSuccess})
	return &projectOutput{Body: created}, nil
}

func (p projects) list(ctx context.Context, in *orgPath) (*projectListOutput, error) {
	rows, err := p.db.Query(ctx, `SELECT `+projectColumns+` FROM projects WHERE org_id = $1 ORDER BY created_at, id`, in.OrgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := &projectListOutput{}
	out.Body.Items = []project{}
	for rows.Next() {
		item, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out.Body.Items = append(out.Body.Items, item)
	}
	return out, rows.Err()
}

func (p projects) get(ctx context.Context, in *projectPath) (*projectOutput, error) {
	found, err := scanProject(p.db.QueryRow(ctx, `SELECT `+projectColumns+` FROM projects WHERE org_id = $1 AND id = $2`, in.OrgID, in.ID))
	if err != nil {
		return nil, err
	}
	return &projectOutput{Body: found}, nil
}

func (p projects) update(ctx context.Context, in *updateProjectInput) (*projectOutput, error) {
	updated, err := scanProject(p.db.QueryRow(ctx, `UPDATE projects SET name = $3, version = version + 1, updated_at = now()
		WHERE org_id = $1 AND id = $2 AND version = $4 RETURNING `+projectColumns, in.OrgID, in.ID, in.Body.Name, in.Body.Version))
	if errors.Is(err, errProjectNotFound) {
		if _, getErr := p.get(ctx, &projectPath{OrgID: in.OrgID, ID: in.ID}); getErr == nil {
			return nil, errProjectConflict
		}
	}
	if err != nil {
		return nil, err
	}
	return &projectOutput{Body: updated}, nil
}

func (p projects) delete(ctx context.Context, in *projectPath) (*struct{}, error) {
	tag, err := p.db.Exec(ctx, `DELETE FROM projects WHERE org_id = $1 AND id = $2`, in.OrgID, in.ID)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, errProjectNotFound
	}
	return nil, nil
}

// exampleFlags declares the golden app's example client flag.
func exampleFlags() gorbital.Module {
	return gorbital.Module{
		Name: "example_flags",
		Flags: func(reg *flags.Registry) {
			flags.Bool(reg, "example.ping_time", flags.Describe("An example client flag."), flags.Client())
		},
	}
}
