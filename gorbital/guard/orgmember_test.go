package guard_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"gorbital.dev/actor"
	"gorbital.dev/config"
	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/gorbitaltest"
	"gorbital.dev/gorbital/guard"
	"gorbital.dev/modules/auth"
	"gorbital.dev/modules/orgs"
	"gorbital.dev/modules/postgres/pgtest"
)

type orgInput struct {
	OrgID string `path:"orgId"`
}

type orgSeen struct {
	Body struct {
		OrgID       string   `json:"org_id"`
		Permissions []string `json:"permissions"`
		DBOrg       string   `json:"db_org"`
	}
}

func ExampleOrgMember() {
	s := exampleServer(func(r *gorbital.Router) {
		invoices := r.Group("/v1/orgs/{orgId}/invoices", gorbital.Tags("Invoices"))
		gorbital.Get(invoices, "", catalogBook, guard.OrgMember("invoices.invoice.read"))
	})
	op := s.api.OpenAPI().Paths["/v1/orgs/{orgId}/invoices"].Get
	fmt.Println(op.Extensions["x-gorbital-guards"], op.Errors)
	fmt.Println(s.as("/v1/orgs/org_1/invoices", nil))

	// The path must name the organisation.
	_, err := tryMount(routes(func(r *gorbital.Router) {
		gorbital.Get(r, "/v1/invoices/{id}", catalogBook, guard.OrgMember("invoices.invoice.read"))
	}))
	fmt.Println(err)
	// Output:
	// [authenticated org_member:invoices.invoice.read] [401 403 404 422 500]
	// 401 unauthenticated
	// gorbital: module "books": GET /v1/invoices/{id}: guard.OrgMember needs the organisation ID in the path as {orgId}, such as /v1/orgs/{orgId}/invoices
}

func TestOrgMemberRegistration(t *testing.T) {
	for _, tt := range []struct {
		name string
		opts []gorbital.RouteOption
		path string
		want string
	}{
		{"no organisation in the path", []gorbital.RouteOption{guard.OrgMember("books.book.read")}, "/v1/books/{id}", "{orgId}"},
		{"a public route", []gorbital.RouteOption{guard.Public(), guard.OrgMember("books.book.read")}, "/v1/orgs/{orgId}/books", "public route"},
		{"an invalid permission", []gorbital.RouteOption{guard.OrgMember("read")}, "/v1/orgs/{orgId}/books", `permission name "read" is invalid`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tryMount(routes(func(r *gorbital.Router) {
				gorbital.Get(r, tt.path, ok[struct{}], tt.opts...)
			}))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Mount() error = %v, want one mentioning %s", err, tt.want)
			}
		})
	}
}

// fakeOrgs is an organisation authorizer with one member per organisation,
// holding permissions.
type fakeOrgs struct {
	members map[string]string // org → user
	perms   []string
	stepUp  []string
	err     error // returned for every request when set
	wrong   bool  // returns a context acting in another organisation
}

func (f fakeOrgs) AuthorizeOrg(ctx context.Context, orgID, permission string) (context.Context, error) {
	if f.err != nil {
		return ctx, f.err
	}
	a, _ := actor.From(ctx)
	if f.members[orgID] != a.ID {
		return ctx, orgs.ErrOrgNotFound
	}
	a.OrgID, a.Permissions, a.StepUp = orgID, f.perms, f.stepUp
	if f.wrong {
		a.OrgID = "org_somewhereelse"
	}
	ctx = actor.With(ctx, a)
	return ctx, actor.Require(ctx, permission)
}

// orgsModule gives the app a as its organisation authorizer.
func orgsModule(name string, a gorbital.OrgAuthorizer) gorbital.Module {
	return gorbital.Module{Name: name, Platform: func(p *gorbital.Platform) error { return p.SetOrgAuthorizer(a) }}
}

// booksInOrgs is a module whose route reports what the handler sees.
func booksInOrgs() gorbital.Module {
	return gorbital.Module{
		Name: "books",
		Permissions: []gorbital.Permission{
			{Name: "books.book.read", Description: "See books", OrgRoles: []string{orgs.RoleMember}},
		},
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			gorbital.Get(r, "/v1/orgs/{orgId}/books", func(ctx context.Context, _ *orgInput) (*orgSeen, error) {
				out := &orgSeen{}
				a, _ := actor.From(ctx)
				out.Body.OrgID, out.Body.Permissions = a.OrgID, a.Permissions
				// The connection carries the organisation for row-level
				// security.
				if err := d.DB.QueryRow(ctx, "SELECT current_setting('gorbital.org_id', true)").Scan(&out.Body.DBOrg); err != nil {
					return nil, err
				}
				return out, nil
			}, guard.OrgMember("books.book.read"))
		},
	}
}

func TestOrgMember(t *testing.T) {
	org, other := string(orgs.NewID()), string(orgs.NewID())
	member := gorbitaltest.User("usr_ada")
	stranger := gorbitaltest.User("usr_bob")
	path := func(org string) string { return "/v1/orgs/" + org + "/books" }

	app := gorbitaltest.New(t, gorbital.WithModules(booksInOrgs(), orgsModule("orgs", fakeOrgs{
		members: map[string]string{org: "usr_ada", other: "usr_carol"}, perms: []string{"books.book.read", "books.book.write"},
	})))
	res := app.As(member).Get(path(org))
	res.AssertStatus(t, http.StatusOK)
	var seen orgSeen
	res.JSON(t, &seen.Body)
	if seen.Body.OrgID != org || seen.Body.DBOrg != org || strings.Join(seen.Body.Permissions, ",") != "books.book.read,books.book.write" {
		t.Errorf("handler saw %+v, want the actor and the connection acting in %s with the role's permissions", seen.Body, org)
	}
	for _, tt := range []struct {
		name   string
		client *gorbitaltest.Client
		path   string
		status int
		code   string
	}{
		{"another organisation's member", app.As(stranger), path(org), 404, "org_not_found"},
		{"a member of another organisation", app.As(member), path(other), 404, "org_not_found"},
		{"an organisation that doesn't exist", app.As(member), path(string(orgs.NewID())), 404, "org_not_found"},
		{"a malformed organisation ID", app.As(member), path("org_1"), 404, "org_not_found"},
		{"without an actor", app.Client(), path(org), 401, "unauthenticated"},
	} {
		t.Run(tt.name, func(t *testing.T) { tt.client.Get(tt.path).AssertProblem(t, tt.status, tt.code) })
	}

	// The authorizer's refusals and failures.
	for _, tt := range []struct {
		name   string
		orgs   fakeOrgs
		status int
		code   string
	}{
		{"the role lacks the permission", fakeOrgs{members: map[string]string{org: "usr_ada"}}, 403, "forbidden"},
		{"the role needs a second factor", fakeOrgs{members: map[string]string{org: "usr_ada"}, stepUp: []string{"books.book.read"}}, 403, "mfa_required"},
		{"the authorizer fails", fakeOrgs{err: errors.New("database unavailable")}, 500, "internal_error"},
		{"the authorizer acts in another organisation", fakeOrgs{members: map[string]string{org: "usr_ada"}, perms: []string{"books.book.read"}, wrong: true}, 500, "internal_error"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			app := gorbitaltest.New(t, gorbital.WithModules(booksInOrgs(), orgsModule("orgs", tt.orgs)))
			app.As(member).Get(path(org)).AssertProblem(t, tt.status, tt.code)
		})
	}
}

// TestOrgMemberNeedsOrganisations checks that an app whose routes use
// guard.OrgMember doesn't start without an organisations module, nor with
// two.
func TestOrgMemberNeedsOrganisations(t *testing.T) {
	for _, tt := range []struct {
		name    string
		modules []gorbital.Module
		want    string
	}{
		{"no organisations module", []gorbital.Module{booksInOrgs()}, "guard.Scope on GET /v1/orgs/{orgId}/books needs a scope"},
		{"two authorizers", []gorbital.Module{booksInOrgs(), orgsModule("orgs", fakeOrgs{}), orgsModule("more_orgs", fakeOrgs{})}, "the app already has a scope"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			url := pgtest.NewDatabase(t)
			cfg, err := gorbital.LoadConfig(config.Source{Getenv: func(k string) string {
				return map[string]string{"APP_ENV": "development", "DATABASE_URL": url, "LOG_ARCHIVE_DIR": t.TempDir(), "STORAGE_LOCAL_DIR": t.TempDir()}[k]
			}, ReadFile: os.ReadFile})
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			opts := []gorbital.Option{gorbital.WithModules(tt.modules...), gorbital.WithLogger(slog.New(slog.DiscardHandler))}
			if err := gorbital.Migrate(ctx, cfg, io.Discard, opts...); err != nil {
				t.Fatal(err)
			}
			a, err := gorbital.New(ctx, cfg, opts...)
			if err == nil {
				_ = a.Close(ctx)
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("New() error = %v, want %q", err, tt.want)
			}
		})
	}
}

// TestOrgMemberWithoutAnApp checks a route mounted by hand, with no
// organisations to ask: members get 500, never access.
func TestOrgMemberWithoutAnApp(t *testing.T) {
	s := exampleServer(func(r *gorbital.Router) {
		gorbital.Get(r, "/v1/orgs/{orgId}/books", catalogBook, guard.OrgMember("books.book.read"))
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/orgs/"+string(orgs.NewID())+"/books", nil)
	req = req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{UserID: "usr_1", SessionID: "ses_1", Permissions: []string{"books.book.read"}}))
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("GET = %d %s, want 500", rec.Code, rec.Body)
	}
}
