package authhttp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/actor"
	"gorbital.dev/config"
	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"
	"gorbital.dev/httpx"
	authlib "gorbital.dev/modules/auth"
	"gorbital.dev/modules/postgres/pgtest"

	"example.com/plateful/internal/modules/auth/usecase"
)

// These tests are a v0.1 app's HTTP tests of sign-in
// (examples/v0.1/full-single/internal/app), run against an app built with
// gorbital.New and this package: the move into the library is proven by the
// same assertions passing. Test apps are named like the golden app, whose
// name appears in emails and authenticator app URIs.

const (
	testAppName  = "acme-api"
	testPassword = "correct horse battery"
)

// testEncryptionKeys is AUTH_ENCRYPTION_KEYS for every test app in this
// process, so two instances on one database share them.
var testEncryptionKeys = authlib.NewKeyringKey("test")

// signedInEndpoint is an endpoint any signed-in user can call (a user-owned
// resource), for the API key tests.
const signedInEndpoint = "/v1/projects"

// testEnv returns the environment of a test app: development, with the
// test keys, and env's values over them.
func testEnv(t *testing.T, env map[string]string) map[string]string {
	t.Helper()
	full := map[string]string{
		"APP_ENV":              "development",
		"AUTH_ENCRYPTION_KEYS": testEncryptionKeys,
		"APP_DB_MAX_CONNS":     "4",
		"LOG_ARCHIVE_DIR":      t.TempDir(),
		"STORAGE_LOCAL_DIR":    t.TempDir(),
	}
	maps.Copy(full, env)
	return full
}

// testConfig loads the configuration of a test app from env.
func testConfig(t *testing.T, env map[string]string) gorbital.Config {
	t.Helper()
	full := testEnv(t, env)
	cfg, err := gorbital.LoadConfig(config.Source{Getenv: func(k string) string { return full[k] }, ReadFile: os.ReadFile})
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	return cfg
}

// testApp is an app on gorbital.New with sign-in from this package.
type testApp struct {
	*gorbital.App
	auth *Authenticator
	cfg  gorbital.Config
	url  string
}

// Auth returns the sign-in use cases, as a v0.1 app's App.Auth does.
func (a *testApp) Auth() *usecase.Service { return a.auth.service() }

// appOptions are the options every test app is built with.
func appOptions(auth *Authenticator) []gorbital.Option {
	return []gorbital.Option{
		gorbital.WithName(testAppName),
		gorbital.WithAuth(auth),
		gorbital.WithModules(projectsModule()),
		gorbital.WithLogger(slog.New(slog.DiscardHandler)),
	}
}

// newApp creates a migrated database on the Docker PostgreSQL server and
// builds the app on it. configure changes the configuration and the
// authenticator before the app is built. Tests are skipped when the server
// isn't configured.
func newApp(t *testing.T, env map[string]string, configure ...func(*gorbital.Config, *Authenticator)) *testApp {
	t.Helper()
	a, _ := newAppWithURL(t, env, configure...)
	return a
}

// newAppWithURL is newApp that also returns the database URL, for tests
// that read tables directly.
func newAppWithURL(t *testing.T, env map[string]string, configure ...func(*gorbital.Config, *Authenticator)) (*testApp, string) {
	t.Helper()
	url := pgtest.NewDatabase(t)
	full := map[string]string{"DATABASE_URL": url}
	maps.Copy(full, env)
	return buildApp(t, testConfig(t, full), configure...), url
}

// buildApp migrates cfg's database and builds an app on it.
func buildApp(t *testing.T, cfg gorbital.Config, configure ...func(*gorbital.Config, *Authenticator)) *testApp {
	t.Helper()
	ctx := context.Background()
	auth := New()
	for _, c := range configure {
		c(&cfg, auth)
	}
	opts := appOptions(auth)
	if err := gorbital.Migrate(ctx, cfg, io.Discard, opts...); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	a, err := gorbital.New(ctx, cfg, opts...)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := a.Close(context.WithoutCancel(ctx)); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return &testApp{App: a, auth: auth, cfg: cfg, url: cfg.DatabaseURL.Reveal()}
}

type response struct {
	code   int
	header http.Header
	body   string
	json   map[string]any
}

// do sends a request; headers are name/value pairs.
func do(t *testing.T, h http.Handler, method, path, body string, headers ...string) response {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	r := response{code: rec.Code, header: rec.Header(), body: rec.Body.String()}
	_ = json.Unmarshal(rec.Body.Bytes(), &r.json)
	return r
}

// signIn creates a verified account holding role (none when empty), signs
// it in with a bearer token, and returns the header to send and the user ID.
func signIn(t *testing.T, a *testApp, email, role string) ([]string, string) {
	t.Helper()
	ctx := actor.With(context.Background(), actor.System("test"))
	u, err := a.Auth().CreateUser(ctx, email, testPassword, true)
	if err != nil {
		t.Fatalf("CreateUser(%s) error = %v", email, err)
	}
	if role != "" {
		if err := a.Auth().GrantRole(ctx, u.ID, role); err != nil {
			t.Fatalf("GrantRole(%s) error = %v", role, err)
		}
	}
	// A role requiring two-factor authentication needs a session signed in
	// with it.
	if role != "" && a.Auth().Catalog().RequiresMFA(role) {
		enrollment, _, err := a.Auth().EnrollTOTP(ctx, u.ID)
		if err != nil {
			t.Fatalf("EnrollTOTP(%s) error = %v", email, err)
		}
		return []string{"Authorization", "Bearer " + signInWithTOTP(t, a.Handler(), email, testPassword, enrollment.Secret)}, u.ID
	}
	r := do(t, a.Handler(), "POST", "/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":%q,"transport":"bearer"}`, email, testPassword))
	token, _ := r.json["token"].(string)
	if r.code != http.StatusOK || token == "" {
		t.Fatalf("POST /v1/auth/login as %s = %d %s", email, r.code, r.body)
	}
	return []string{"Authorization", "Bearer " + token}, u.ID
}

// projectsOf returns the collection of the signed-in user's projects.
func projectsOf(*testing.T, http.Handler, []string) string { return "/v1/projects" }

// projectsModule stands in for the golden app's example resources: a
// user-scoped projects collection under /v1/projects, whose permissions the
// user role grants, and GET /v1/flags, which needs flags.flag.read like the
// golden app's flags endpoint. The API key tests use them to check scopes.
func projectsModule() gorbital.Module {
	const read, write, flagsRead = "projects.project.read", "projects.project.write", "flags.flag.read"
	type project struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Version int    `json:"version"`
	}
	var mu sync.Mutex
	store := map[string]map[string]*project{} // owner → ID → project
	next := 0
	owner := func(ctx context.Context) map[string]*project {
		a, _ := actor.From(ctx)
		if store[a.ID] == nil {
			store[a.ID] = map[string]*project{}
		}
		return store[a.ID]
	}
	type listOutput struct {
		Body struct {
			Projects []project `json:"projects"`
		}
	}
	type projectOutput struct{ Body project }
	type createInput struct {
		Body struct {
			Name string `json:"name" maxLength:"100"`
		}
	}
	type idInput struct {
		ID string `path:"id"`
	}
	type updateInput struct {
		ID   string `path:"id"`
		Body struct {
			Version int    `json:"version"`
			Name    string `json:"name" maxLength:"100"`
		}
	}
	type flagsOutput struct {
		Body struct {
			Flags map[string]any `json:"flags"`
		}
	}
	return gorbital.Module{
		Name: "projects",
		Permissions: []gorbital.Permission{
			{Name: read, Description: "See your projects", Roles: []string{"user"}},
			{Name: write, Description: "Create, change and delete your projects", Roles: []string{"user"}},
			{Name: flagsRead, Description: "Read the feature flags shown to clients", Roles: []string{"user"}},
		},
		Routes: func(r *gorbital.Router, _ gorbital.Deps) {
			// Like the golden app's projects use case (ownerID), only users
			// own projects: other actors get 401 before the permission check.
			usersOnly := guard.New(guard.Spec{Name: "user_only", Statuses: []int{http.StatusUnauthorized}, Check: func(ctx context.Context, _ guard.Request) error {
				if a, ok := actor.From(ctx); !ok || a.Kind != actor.KindUser || a.ID == "" {
					return httpx.NewProblem(http.StatusUnauthorized, "unauthenticated", "authentication is required")
				}
				return nil
			}})
			projects := r.Group("/v1/projects", gorbital.Tags("Projects"), usersOnly)
			gorbital.Get(projects, "", func(ctx context.Context, _ *struct{}) (*listOutput, error) {
				mu.Lock()
				defer mu.Unlock()
				out := &listOutput{}
				out.Body.Projects = []project{}
				for _, p := range owner(ctx) {
					out.Body.Projects = append(out.Body.Projects, *p)
				}
				return out, nil
			}, guard.Permission(read))
			gorbital.Post(projects, "", func(ctx context.Context, in *createInput) (*projectOutput, error) {
				mu.Lock()
				defer mu.Unlock()
				next++
				p := &project{ID: fmt.Sprintf("prj_%d", next), Name: in.Body.Name, Version: 1}
				owner(ctx)[p.ID] = p
				return &projectOutput{Body: *p}, nil
			}, gorbital.Status(http.StatusCreated), guard.Permission(write))
			gorbital.Get(projects, "/{id}", func(ctx context.Context, in *idInput) (*projectOutput, error) {
				mu.Lock()
				defer mu.Unlock()
				p, ok := owner(ctx)[in.ID]
				if !ok {
					return nil, httpx.NewProblem(http.StatusNotFound, "project_not_found", "no project of yours has this ID")
				}
				return &projectOutput{Body: *p}, nil
			}, guard.Permission(read))
			gorbital.Patch(projects, "/{id}", func(ctx context.Context, in *updateInput) (*projectOutput, error) {
				mu.Lock()
				defer mu.Unlock()
				p, ok := owner(ctx)[in.ID]
				if !ok {
					return nil, httpx.NewProblem(http.StatusNotFound, "project_not_found", "no project of yours has this ID")
				}
				p.Name, p.Version = in.Body.Name, p.Version+1
				return &projectOutput{Body: *p}, nil
			}, guard.Permission(write))
			gorbital.Delete(projects, "/{id}", func(ctx context.Context, in *idInput) (*struct{}, error) {
				mu.Lock()
				defer mu.Unlock()
				delete(owner(ctx), in.ID)
				return nil, nil
			}, gorbital.Status(http.StatusNoContent), guard.Permission(write))
			gorbital.Get(r.Group("/v1/flags", gorbital.Tags("Flags")), "", func(context.Context, *struct{}) (*flagsOutput, error) {
				out := &flagsOutput{}
				out.Body.Flags = map[string]any{}
				return out, nil
			}, guard.Permission(flagsRead))
		},
	}
}

// fromClient sends a request whose connection comes from remote.
func fromClient(t *testing.T, h http.Handler, method, path, body, remote string, headers ...string) response {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = remote
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Add(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	r := response{code: rec.Code, header: rec.Header(), body: rec.Body.String()}
	_ = json.Unmarshal(rec.Body.Bytes(), &r.json)
	return r
}

// runCommand runs one of the authenticator's commands, as gorbital.Main
// does after Setup, with the app's configuration, and returns its output.
func runCommand(t *testing.T, a *testApp, name string, args ...string) (string, error) {
	t.Helper()
	for _, c := range a.auth.Commands() {
		if c.Name == name {
			var out strings.Builder
			err := c.Run(context.Background(), a.cfg, args, &out)
			return out.String(), err
		}
	}
	t.Fatalf("no command %q", name)
	return "", nil
}

// auditEvent is a row of audit_events, as GET /ops/audit lists it.
type auditEvent struct {
	Action, ActorKind, ActorID, ResourceType, ResourceID, Outcome, IP, UserAgent string
	Metadata                                                                     map[string]any
}

// auditEvents returns the audit events whose action starts with prefix,
// newest first. /ops/audit is the operations module's (Phase 4), so the
// tests read the table.
func auditEvents(t *testing.T, databaseURL, prefix string) []auditEvent {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	rows, err := pool.Query(context.Background(), `
		SELECT action, actor_kind, actor_id, COALESCE(resource_type, ''), COALESCE(resource_id, ''), outcome,
		       COALESCE(host(ip), ''), COALESCE(user_agent, ''), COALESCE(metadata, '{}'::jsonb)
		FROM audit_events WHERE starts_with(action, $1) ORDER BY occurred_at DESC, id DESC`, prefix)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []auditEvent
	for rows.Next() {
		var e auditEvent
		if err := rows.Scan(&e.Action, &e.ActorKind, &e.ActorID, &e.ResourceType, &e.ResourceID, &e.Outcome, &e.IP, &e.UserAgent, &e.Metadata); err != nil {
			t.Fatal(err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// signInWithTOTP signs in to an account with two-factor authentication and
// returns a bearer token for a session verified with a second factor.
func signInWithTOTP(t *testing.T, h http.Handler, email, password, secret string) string {
	t.Helper()
	r := do(t, h, "POST", "/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, email, password))
	mfa, _ := r.json["mfa"].(map[string]any)
	challenge, _ := mfa["challenge_token"].(string)
	if r.code != http.StatusAccepted || challenge == "" {
		t.Fatalf("POST /v1/auth/login as %s = %d %s, want 202 with a challenge", email, r.code, r.body)
	}
	code, err := authlib.TOTPCode(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	r = do(t, h, "POST", "/v1/auth/login/mfa", fmt.Sprintf(`{"challenge_token":%q,"code":%q,"transport":"bearer"}`, challenge, code))
	token, _ := r.json["token"].(string)
	if r.code != http.StatusOK || token == "" {
		t.Fatalf("POST /v1/auth/login/mfa as %s = %d %s", email, r.code, r.body)
	}
	return token
}
