package orgshttp_test

import (
	"context"
	"net/http"
	"testing"

	"gorbital.dev/actor"
	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/authhttp"
	"gorbital.dev/gorbital/gorbitaltest"
	"gorbital.dev/gorbital/guard"
	"gorbital.dev/gorbital/orgshttp"
	authlib "gorbital.dev/modules/auth"
)

type orgOut struct {
	Body struct {
		OrgID string `json:"org_id"`
		Perms []string `json:"perms"`
	}
}

func TestSmoke(t *testing.T) {
	auth := authhttp.New()
	probe := gorbital.Module{
		Name:        "probe",
		Permissions: []gorbital.Permission{{Name: "probe.thing.read", Description: "x", OrgRoles: []string{"owner", "admin", "member"}}},
		Routes: func(r *gorbital.Router, _ gorbital.Deps) {
			gorbital.Get(r, "/v1/orgs/{orgId}/probe", func(ctx context.Context, in *struct {
				OrgID string `path:"orgId"`
			}) (*orgOut, error) {
				a, _ := actor.From(ctx)
				out := &orgOut{}
				out.Body.OrgID, out.Body.Perms = a.OrgID, a.Permissions
				return out, nil
			}, guard.OrgMember("probe.thing.read"))
		},
	}
	app := gorbitaltest.NewWithEnv(t, map[string]string{"AUTH_ENCRYPTION_KEYS": authlib.NewKeyringKey("t")},
		gorbital.WithAuth(auth), gorbital.WithModules(orgshttp.Module(auth), probe))
	ada, _ := app.SignUp(t, "ada@example.com")
	bob, _ := app.SignUp(t, "bob@example.com")
	res := ada.Post("/v1/orgs", map[string]string{"name": "Acme"})
	res.AssertStatus(t, http.StatusCreated)
	var org struct{ ID string `json:"id"` }
	res.JSON(t, &org)
	r := ada.Get("/v1/orgs/" + org.ID + "/probe")
	r.AssertStatus(t, 200)
	t.Logf("%s", r.Body)
	bob.Get("/v1/orgs/" + org.ID + "/probe").AssertProblem(t, 404, "org_not_found")
	bob.Get("/v1/orgs/org_nope/probe").AssertProblem(t, 404, "org_not_found")
	app.Client().Get("/v1/orgs/" + org.ID + "/probe").AssertProblem(t, 401, "unauthenticated")
}
