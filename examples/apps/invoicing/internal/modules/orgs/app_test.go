package orgshttp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	authhttp "example.com/invoicing/internal/modules/auth"
	"gorbital.dev/config"
	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/flagshttp"
	"gorbital.dev/gorbital/opshttp"
	authlib "gorbital.dev/modules/auth"
	"gorbital.dev/modules/postgres/pgtest"

	"example.com/invoicing/internal/modules/orgs/usecase"
)

// These tests are a v0.1 multi-tenant app's HTTP tests of organisations
// (examples/v0.1/full-multi/internal/app: orgs_test.go, orgs_service_accounts_test.go,
// org_settings_test.go, org_flags_test.go, rls_test.go and the organisation
// parts of projects_test.go), run against an app built with gorbital.New,
// authhttp, opshttp, flagshttp and this package. The golden app's projects
// module is replaced by testProjects, an organisation-scoped module guarded
// by guard.OrgMember, as `orb gen module --org` writes them.

const (
	testAppName  = "acme-api"
	testPassword = "correct horse battery staple"
	pingTimeFlag = "/ops/flags/example.ping_time"
)

// repo is the repository root, relative to this package.

// testEncryptionKeys is AUTH_ENCRYPTION_KEYS of the test apps.
var testEncryptionKeys = authlib.NewKeyringKey("orgshttp")

// testApp is an app with every built-in module and the test projects.
type testApp struct {
	*gorbital.App
	auth *authhttp.Authenticator
	orgs *module
	cfg  gorbital.Config
}

// Orgs returns the organisations use cases, as a v0.1 app's App.Orgs does.
func (a *testApp) Orgs() *usecase.Service { return a.orgs.service() }

// command runs one of sign-in's commands, as gorbital.Main does.
func (a *testApp) command(t *testing.T, name string, args ...string) string {
	t.Helper()
	for _, c := range a.auth.Commands() {
		if c.Name == name {
			var out strings.Builder
			if err := c.Run(context.Background(), a.cfg, args, &out); err != nil {
				t.Fatalf("%s %v: %v", name, args, err)
			}
			return out.String()
		}
	}
	t.Fatalf("no command %q", name)
	return ""
}

// appOptions are the options of the test app, as main.go adds them.
func appOptions(auth *authhttp.Authenticator, orgs *module, extra ...gorbital.Module) []gorbital.Option {
	return []gorbital.Option{
		gorbital.WithName(testAppName),
		gorbital.WithAuth(auth),
		gorbital.WithModules(opshttp.Module(), flagshttp.Module(), orgs.gorbitalModule(), testProjects(), exampleFlags()),
		gorbital.WithModules(extra...),
		gorbital.WithMigrations(testMigrations),
		gorbital.WithLogger(slog.New(slog.DiscardHandler)),
	}
}

// testConfig loads a test app's configuration: development, with env's
// values.
func testConfig(t *testing.T, env map[string]string) gorbital.Config {
	t.Helper()
	full := map[string]string{
		"APP_ENV":              "development",
		"AUTH_ENCRYPTION_KEYS": testEncryptionKeys,
		"APP_DB_MAX_CONNS":     "4",
		"LOG_ARCHIVE_DIR":      t.TempDir(),
		"STORAGE_LOCAL_DIR":    t.TempDir(),
	}
	maps.Copy(full, env)
	cfg, err := gorbital.LoadConfig(config.Source{Getenv: func(k string) string { return full[k] }, ReadFile: os.ReadFile})
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	return cfg
}

func newApp(t *testing.T, env map[string]string, extra ...gorbital.Module) *testApp {
	t.Helper()
	a, _ := newAppWithURL(t, env, extra...)
	return a
}

// newAppWithURL builds the app on a new, migrated database and returns the
// database URL, for tests that read tables directly. The app connects as
// appRole, the URL as the test server's user.
func newAppWithURL(t *testing.T, env map[string]string, extra ...gorbital.Module) (*testApp, string) {
	t.Helper()
	ctx := context.Background()
	dbURL := pgtest.NewDatabase(t)
	full := map[string]string{"DATABASE_URL": dbURL}
	maps.Copy(full, env)
	cfg := testConfig(t, full)
	auth, orgs := authhttp.New(), newModule(nil)
	orgs.auth = auth
	opts := appOptions(auth, orgs, extra...)
	if err := gorbital.Migrate(ctx, cfg, io.Discard, opts...); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	cfg.DatabaseURL = config.NewSecret(asAppRole(t, dbURL))
	a, err := gorbital.New(ctx, cfg, opts...)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := a.Close(context.WithoutCancel(ctx)); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return &testApp{App: a, auth: auth, orgs: orgs, cfg: cfg}, dbURL
}

// newPool connects to dbURL as the test server's user until the test ends.
func newPool(t *testing.T, dbURL string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
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

var (
	sixDigits       = regexp.MustCompile(`\b(\d{6})\b`)
	invitationToken = regexp.MustCompile(`#token=([A-Za-z0-9_-]+)`)
)

// emailed returns the first match of pattern's group in the newest email
// queued for to.
func emailed(t *testing.T, a *testApp, to string, pattern *regexp.Regexp) string {
	t.Helper()
	rows, err := a.Deps().DB.Query(context.Background(), `SELECT args FROM river_job WHERE kind = 'gorbital.mail.send' ORDER BY id DESC`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var args struct {
			Message struct {
				To   []struct{ Email string } `json:"to"`
				Text string                   `json:"text"`
			} `json:"message"`
		}
		if json.Unmarshal(raw, &args) != nil || len(args.Message.To) == 0 || args.Message.To[0].Email != to {
			continue
		}
		if m := pattern.FindStringSubmatch(args.Message.Text); m != nil {
			return m[1]
		}
	}
	t.Fatalf("no email for %s matching %s", to, pattern)
	return ""
}

// emailedInvitation returns the token in the newest invitation email queued
// for to.
func emailedInvitation(t *testing.T, a *testApp, to string) string {
	t.Helper()
	return emailed(t, a, to, invitationToken)
}

// signIn creates a verified account through sign-in's endpoints, holding
// role when one is given, signs it in with a bearer token (with a second
// factor for roles that require one), and returns the header to send and
// the user ID.
func signIn(t *testing.T, a *testApp, email, role string) ([]string, string) {
	t.Helper()
	h := a.Handler()
	if r := do(t, h, "POST", "/v1/auth/register", fmt.Sprintf(`{"email":%q,"password":%q}`, email, testPassword)); r.code != http.StatusAccepted {
		t.Fatalf("register %s = %d %s", email, r.code, r.body)
	}
	if r := do(t, h, "POST", "/v1/auth/verify-email", fmt.Sprintf(`{"email":%q,"code":%q}`, email, emailed(t, a, email, sixDigits))); r.code != http.StatusNoContent {
		t.Fatalf("verify %s = %d %s", email, r.code, r.body)
	}
	login := func() response {
		return do(t, h, "POST", "/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":%q,"transport":"bearer"}`, email, testPassword))
	}
	r := login()
	token, _ := r.json["token"].(string)
	if r.code != http.StatusOK || token == "" {
		t.Fatalf("login %s = %d %s", email, r.code, r.body)
	}
	bearer := []string{"Authorization", "Bearer " + token}
	if role != "" {
		a.command(t, "grant-role", email, role)
		setup := do(t, h, "POST", "/v1/auth/mfa/totp", fmt.Sprintf(`{"password":%q}`, testPassword), bearer...)
		secret, _ := setup.json["secret"].(string)
		code, err := authlib.TOTPCode(secret, time.Now())
		if secret == "" || err != nil {
			t.Fatalf("authenticator app setup = %d %s", setup.code, setup.body)
		}
		confirm := do(t, h, "POST", "/v1/auth/mfa/totp/confirm", fmt.Sprintf(`{"code":%q}`, code), bearer...)
		codes, _ := confirm.json["recovery_codes"].([]any)
		if len(codes) == 0 {
			t.Fatalf("confirm authenticator app = %d %s", confirm.code, confirm.body)
		}
		mfa, _ := login().json["mfa"].(map[string]any)
		challenge, _ := mfa["challenge_token"].(string)
		r = do(t, h, "POST", "/v1/auth/login/mfa", fmt.Sprintf(`{"challenge_token":%q,"recovery_code":%q,"transport":"bearer"}`, challenge, codes[0]))
		token, _ = r.json["token"].(string)
		if r.code != http.StatusOK || token == "" {
			t.Fatalf("login with a second factor %s = %d %s", email, r.code, r.body)
		}
		bearer = []string{"Authorization", "Bearer " + token}
	}
	me := do(t, h, "GET", "/v1/auth/me", "", bearer...)
	user, _ := me.json["user"].(map[string]any)
	id, _ := user["id"].(string)
	if me.code != http.StatusOK || id == "" {
		t.Fatalf("GET /v1/auth/me as %s = %d %s", email, me.code, me.body)
	}
	return bearer, id
}

// personalWorkspace returns the ID of the signed-in user's personal
// workspace, created with their account.
func personalWorkspace(t *testing.T, h http.Handler, headers []string) string {
	t.Helper()
	r := do(t, h, "GET", "/v1/orgs", "", headers...)
	items, _ := r.json["items"].([]any)
	if r.code != http.StatusOK || len(items) == 0 || items[0].(map[string]any)["personal"] != true {
		t.Fatalf("GET /v1/orgs = %d %s, want the personal workspace first", r.code, r.body)
	}
	return items[0].(map[string]any)["id"].(string)
}

// projectsOf returns the collection of the projects in the signed-in user's
// personal workspace.
func projectsOf(t *testing.T, h http.Handler, headers []string) string {
	t.Helper()
	return "/v1/orgs/" + personalWorkspace(t, h, headers) + "/projects"
}

// newOrg creates an organisation owned by the signed-in user.
func newOrg(t *testing.T, h http.Handler, name string, owner []string) string {
	t.Helper()
	r := do(t, h, "POST", "/v1/orgs", fmt.Sprintf(`{"name":%q}`, name), owner...)
	id, _ := r.json["id"].(string)
	if r.code != http.StatusCreated {
		t.Fatalf("create %s = %d %s", name, r.code, r.body)
	}
	return id
}

// join invites email into org as role and accepts as member.
func join(t *testing.T, a *testApp, org, email, role string, owner, member []string) {
	t.Helper()
	h := a.Handler()
	if r := do(t, h, "POST", "/v1/orgs/"+org+"/invitations", fmt.Sprintf(`{"email":%q,"role":%q}`, email, role), owner...); r.code != http.StatusCreated {
		t.Fatalf("invite %s = %d %s", email, r.code, r.body)
	}
	if r := do(t, h, "POST", "/v1/invitations/accept", fmt.Sprintf(`{"token":%q}`, emailedInvitation(t, a, email)), member...); r.code != http.StatusOK {
		t.Fatalf("%s accepts = %d %s", email, r.code, r.body)
	}
}

// createKey creates an API key with a POST to path and returns the key and
// its ID.
func createKey(t *testing.T, h http.Handler, path, body string, headers []string) (key, id string) {
	t.Helper()
	r := do(t, h, "POST", path, body, headers...)
	key, _ = r.json["key"].(string)
	apiKey, _ := r.json["api_key"].(map[string]any)
	id, _ = apiKey["id"].(string)
	if r.code != http.StatusCreated || !strings.HasPrefix(key, authlib.APIKeyPrefix) || id == "" {
		t.Fatalf("POST %s = %d %s, want 201 with a key", path, r.code, r.body)
	}
	return key, id
}

// noSecretsStored fails when a key's secret or an invitation token appears
// in audit events, or a key's in queued jobs.
func noSecretsStored(t *testing.T, pool *pgxpool.Pool, keys ...string) {
	t.Helper()
	var text string
	err := pool.QueryRow(context.Background(), `
		SELECT COALESCE((SELECT string_agg(e::text, ' ') FROM audit_events e), '') || COALESCE((SELECT string_agg(j.args::text, ' ') FROM river_job j), '')`).Scan(&text)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range keys {
		if strings.Contains(text, k[len(authlib.APIKeyPrefix)+27:]) {
			t.Errorf("an API key's secret is stored in audit events or jobs")
		}
	}
}

// appRole is the database role test apps run as: not a superuser and
// without BYPASSRLS, as the app's role must be in production, so row-level
// security policies apply once they are on (ADR-0061). Roles belong to the
// whole server, so every test shares it.
const appRole = "gorbital_app_test"

// asAppRole creates appRole when the server has none, grants it the
// migrated database at dbURL, and returns dbURL connecting as appRole.
func asAppRole(t *testing.T, dbURL string) string {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	role := pgx.Identifier{appRole}.Sanitize()
	err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		// Test binaries run in parallel; the lock lets one create the role.
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('gorbital_app_test.role', 0))"); err != nil {
			return err
		}
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)", appRole).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			if _, err := tx.Exec(ctx, "CREATE ROLE "+role+" NOLOGIN NOSUPERUSER NOBYPASSRLS"); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `
			GRANT USAGE ON SCHEMA public TO `+role+`;
			GRANT SELECT, INSERT, UPDATE, DELETE, TRUNCATE ON ALL TABLES IN SCHEMA public TO `+role+`;
			GRANT USAGE, SELECT, UPDATE ON ALL SEQUENCES IN SCHEMA public TO `+role)
		return err
	})
	if err != nil {
		t.Fatalf("set up database role %s: %v", appRole, err)
	}
	u, err := url.Parse(dbURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("options", "-c role="+appRole)
	u.RawQuery = strings.ReplaceAll(q.Encode(), "+", "%20") // pgx doesn't read + as a space
	return u.String()
}
