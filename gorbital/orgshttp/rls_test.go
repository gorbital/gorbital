package orgshttp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"gorbital.dev/actor"
	"gorbital.dev/config"
	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/authhttp"
	"gorbital.dev/modules/postgres"
)

// TestRowLevelSecurity turns row-level security on under a running app and
// checks that requests through guard.OrgMember, the purge and migrations
// still work, and that a query missing its org_id filter sees only the
// organisation its connection carries (ADR-0061). It is full-multi's
// TestRowLevelSecurity without the golden app's seed command.
func TestRowLevelSecurity(t *testing.T) {
	a, dbURL := newAppWithURL(t, nil)
	enableRowLevelSecurity(t, dbURL)
	roleURL := asAppRole(t, dbURL)
	h := a.Handler()
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)

	ada, _ := signIn(t, a, "ada@example.com", "")
	bob, _ := signIn(t, a, "bob@example.com", "")
	adaOrg, bobOrg := personalWorkspace(t, h, ada), personalWorkspace(t, h, bob)

	// Requests after guard.OrgMember carry their organisation: every
	// operation works, including writes the policy checks.
	var adaProject string
	for _, name := range []string{"Apollo", "Gemini"} {
		r := do(t, h, "POST", "/v1/orgs/"+adaOrg+"/projects", `{"name":"`+name+`","description":"Example description"}`, ada...)
		if r.code != http.StatusCreated {
			t.Fatalf("create %s = %d %s", name, r.code, r.body)
		}
		adaProject, _ = r.json["id"].(string)
	}
	if r := do(t, h, "POST", "/v1/orgs/"+bobOrg+"/projects", `{"name":"Apollo","description":"Example description"}`, bob...); r.code != http.StatusCreated {
		t.Fatalf("create in bob's workspace = %d %s", r.code, r.body)
	}
	item := "/v1/orgs/" + adaOrg + "/projects/" + adaProject
	if r := do(t, h, "GET", "/v1/orgs/"+adaOrg+"/projects", "", ada...); r.code != http.StatusOK || len(r.json["items"].([]any)) != 2 {
		t.Errorf("list = %d %s, want ada's 2 projects", r.code, r.body)
	}
	if r := do(t, h, "PATCH", item, `{"version":1,"name":"Gemini 2"}`, ada...); r.code != http.StatusOK {
		t.Errorf("update = %d %s", r.code, r.body)
	}
	if r := do(t, h, "GET", item, "", bob...); r.code != http.StatusNotFound {
		t.Errorf("another organisation's member reads = %d %s, want 404", r.code, r.body)
	}
	// Organisations' own settings and flags work under the policies too.
	if r := do(t, h, "PUT", "/v1/orgs/"+adaOrg+"/settings/orgs.invitation_ttl", `{"value":"48h","version":0,"reason":"rls"}`, ada...); r.code != http.StatusOK {
		t.Errorf("set an organisation setting = %d %s", r.code, r.body)
	}
	if r := do(t, h, "DELETE", item, "", ada...); r.code != http.StatusNoContent {
		t.Errorf("delete = %d %s", r.code, r.body)
	}

	// A query that forgets its org_id filter, run as the app's role, sees
	// only the organisation of its context; writes to another organisation
	// are refused.
	pool, err := postgres.Open(ctx, config.NewSecret(roleURL), postgres.WithMaxConns(2))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	in := func(org string) context.Context {
		return actor.With(ctx, actor.Actor{Kind: actor.KindUser, ID: "usr_test", OrgID: org})
	}
	for _, c := range []struct {
		name string
		ctx  context.Context
		want int
	}{
		{"ada's organisation", in(adaOrg), 1},
		{"bob's organisation", in(bobOrg), 1},
		{"postgres.WithOrg, as guard.OrgMember sets it", postgres.WithOrg(ctx, bobOrg), 1},
		{"no organisation", ctx, 0},
		{"a system path that bypasses row-level security", postgres.WithoutRowLevelSecurity(ctx, "test"), 2},
	} {
		var n int
		if err := pool.QueryRow(c.ctx, "SELECT count(*) FROM projects").Scan(&n); err != nil || n != c.want {
			t.Errorf("%s: projects without an org_id filter = %d, %v; want %d", c.name, n, err, c.want)
		}
	}
	_, err = pool.Exec(in(adaOrg), `INSERT INTO projects (id, org_id, created_by, name, created_at, updated_at) VALUES ('prj_forged', $1, 'usr_test', 'Forged', now(), now())`, bobOrg)
	if pgErr := (*pgconn.PgError)(nil); !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Errorf("insert into another organisation = %v, want a row-level security violation", err)
	}

	// Migrations run as the app's role.
	roleCfg := testConfig(t, map[string]string{"DATABASE_URL": roleURL})
	auth := authhttp.New()
	orgs := newModule(auth)
	if err := gorbital.Migrate(ctx, roleCfg, io.Discard, appOptions(auth, orgs)...); err != nil {
		t.Errorf("Migrate() as the app's role = %v", err)
	}

	// The purge job removes an organisation's rows through its foreign
	// keys, without a bypass.
	team := newOrg(t, h, "Short-lived", bob)
	if r := do(t, h, "POST", "/v1/orgs/"+team+"/projects", `{"name":"Doomed","description":"Example description"}`, bob...); r.code != http.StatusCreated {
		t.Fatalf("create a project = %d %s", r.code, r.body)
	}
	if r := do(t, h, "DELETE", "/v1/orgs/"+team, "", bob...); r.code != http.StatusNoContent {
		t.Fatalf("delete the organisation = %d %s", r.code, r.body)
	}
	if _, err := admin.Exec(ctx, "UPDATE orgs SET purge_after = now() - interval '1 minute' WHERE id = $1", team); err != nil {
		t.Fatal(err)
	}
	system := actor.With(ctx, actor.System("orgs_purge"))
	if n, err := a.Orgs().Purge(system); err != nil || n != 1 {
		t.Errorf("Purge() = %d, %v; want 1", n, err)
	}
	var left int
	if err := admin.QueryRow(ctx, "SELECT count(*) FROM projects WHERE org_id = $1", team).Scan(&left); err != nil || left != 0 {
		t.Errorf("purged organisation's projects = %d, %v; want 0", left, err)
	}

	// The check behind startup's warnings reports a role that bypasses the
	// policies, and nothing for the app's role.
	for _, c := range []struct {
		url      string
		bypasses bool
	}{{roleURL, false}, {dbURL, true}} {
		conn, err := pgx.Connect(ctx, c.url)
		if err != nil {
			t.Fatal(err)
		}
		r, err := postgres.CheckRowLevelSecurity(ctx, conn)
		_ = conn.Close(ctx)
		if err != nil || r.Bypasses != c.bypasses || !slices.Contains(r.Forced, "projects") {
			t.Errorf("row-level security as %s = %+v, %v; want bypasses %v and projects forced", c.url, r, err, c.bypasses)
		}
	}
}

// TestRowLevelSecurityCoversOrganisationTables checks which tables
// row_level_security.sql protects in an app on gorbital.Main with
// organisations: every table with a NOT NULL org_id except the two it leaves
// out, forced, and the same when run again.
func TestRowLevelSecurityCoversOrganisationTables(t *testing.T) {
	_, dbURL := newAppWithURL(t, nil)
	ctx := context.Background()
	for range 2 {
		enableRowLevelSecurity(t, dbURL)
	}
	conn, err := pgx.Connect(ctx, asAppRole(t, dbURL))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	r, err := postgres.CheckRowLevelSecurity(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	if r.Bypasses || !slices.Contains(r.Forced, "projects") || len(r.NotForced) != 0 || !slices.Equal(r.Unprotected, []string{"org_invitations", "org_members"}) {
		t.Errorf("row-level security = %+v; want projects forced and only org_invitations and org_members left out", r)
	}
	var policies int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM pg_policies WHERE tablename = 'projects'").Scan(&policies); err != nil || policies != 1 {
		t.Errorf("projects policies = %d, %v; want 1 after running twice", policies, err)
	}
}
