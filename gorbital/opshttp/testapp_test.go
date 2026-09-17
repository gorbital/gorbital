package opshttp_test

// The app these tests run against, ported with them from a v0.1 golden app's
// internal/app tests: the operations API, flags and mail events modules
// built with gorbital.New, the golden app's example module (the
// example.ping_message setting, the example.ping_time flag, the heartbeat
// job and GET /v1/ping), bearer tokens for signed-in users holding roles, and
// the app's background workers. opshttp, flagshttp and mailevents each keep
// their own copy, so each module's tests are self-contained and orb eject
// copies them with the module.
//
// Tokens authenticate as a session that verified a second factor, with the
// permissions of the user role and the role asked for.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/riverqueue/river"

	"gorbital.dev/config"
	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/flagshttp"
	"gorbital.dev/gorbital/guard"
	"gorbital.dev/gorbital/mailevents"
	"gorbital.dev/gorbital/opshttp"
	"gorbital.dev/modules/auth"
	"gorbital.dev/modules/flags"
	"gorbital.dev/modules/jobs"
	"gorbital.dev/modules/postgres/pgtest"
	"gorbital.dev/modules/settings"
)

// signInPermissions are the permissions of the ops roles that sign-in
// (gorbital.dev/gorbital/authhttp) declares, such as ops.auth.read for
// /ops/auth/providers and ops.auth.write for rate-limit resets, with the
// roles v0.1 apps grant them to.
var signInPermissions = map[string][]string{
	"platform_admin": {"ops.auth.read", "ops.auth.write", "ops.service_accounts.read", "ops.service_accounts.write"},
	"ops_viewer":     {"ops.auth.read", "ops.service_accounts.read"},
}

// A testApp is an app with the built-in modules and the example module, on
// its own database.
type testApp struct {
	*gorbital.App
	URL     string // the database's URL
	tokens  *testTokens
	modules []gorbital.Module
}

// testAppOptions configure newTestApp.
type testAppOptions struct {
	// Env are environment variables on top of development's defaults.
	Env map[string]string
	// Ops are the options of opshttp.Module.
	Ops []opshttp.Option
	// Gorbital are more options of gorbital.New.
	Gorbital []gorbital.Option
	// SignInMethods, when set, are the sign-in methods the test
	// authenticator reports (gorbital.Platform.SignInMethods). Without it,
	// the authenticator doesn't report any.
	SignInMethods []gorbital.SignInMethod
}

// newTestApp builds the app on a new, migrated database, and closes it when the
// test ends. Without GORBITAL_TEST_DATABASE_URL the test is skipped.
func newTestApp(t testing.TB, o testAppOptions) *testApp {
	t.Helper()
	url := pgtest.NewDatabase(t)
	env := map[string]string{
		"APP_ENV":           "development",
		"APP_ADDR":          "127.0.0.1:0",
		"DATABASE_URL":      url,
		"APP_DB_MAX_CONNS":  "8",
		"APP_JOB_WORKERS":   "4",
		"LOG_ARCHIVE_DIR":   t.TempDir(),
		"STORAGE_LOCAL_DIR": t.TempDir(),
	}
	maps.Copy(env, o.Env)
	cfg, err := gorbital.LoadConfig(config.Source{Getenv: func(key string) string { return env[key] }, ReadFile: os.ReadFile})
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	a, err := buildTestApp(t, cfg, o)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	a.URL = url
	return a
}

// buildTestApp builds the app on cfg's database, which must exist, migrating it
// first; it returns gorbital.New's error instead of failing the test.
func buildTestApp(t testing.TB, cfg gorbital.Config, o testAppOptions) (*testApp, error) {
	t.Helper()
	tokens := &testTokens{principals: map[string]auth.Principal{}}
	modules := []gorbital.Module{opshttp.Module(o.Ops...), flagshttp.Module(), mailevents.Module(), exampleModule()}
	logger := slog.New(slog.NewTextHandler(t.Output(), &slog.HandlerOptions{Level: slog.LevelError}))
	var authenticator gorbital.Authenticator = tokens
	if o.SignInMethods != nil {
		authenticator = reportingTokens{testTokens: tokens, methods: o.SignInMethods}
	}
	opts := append([]gorbital.Option{gorbital.WithName("acme-api"), gorbital.WithLogger(logger), gorbital.WithAuth(authenticator), gorbital.WithModules(modules...)}, o.Gorbital...)
	ctx := context.Background()
	if err := gorbital.Migrate(ctx, cfg, io.Discard, opts...); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	app, err := gorbital.New(ctx, cfg, opts...)
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() {
		if err := app.Close(context.WithoutCancel(ctx)); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return &testApp{App: app, tokens: tokens, modules: modules}, nil
}

// SignIn returns the Authorization header of a new session for email, whose
// user holds role (none when empty) besides the user role, and the user's
// ID.
func (a *testApp) SignIn(t testing.TB, email, role string) ([]string, string) {
	t.Helper()
	id := "usr_" + strings.NewReplacer("@", "_", ".", "_").Replace(email)
	perms := gorbital.Grants("user", a.modules...)
	if role != "" {
		perms = append(perms, gorbital.Grants(role, a.modules...)...)
		if len(gorbital.Grants(role, a.modules...)) == 0 {
			t.Fatalf("SignIn: no module grants the role %q", role)
		}
	}
	perms = append(perms, signInPermissions[role]...)
	slices.Sort(perms)
	token := a.tokens.issue(auth.Principal{
		UserID: id, SessionID: "ses_" + id, Permissions: slices.Compact(perms),
		SignedInAt: time.Now(), MFAVerified: true, MFAVerifiedAt: time.Now(),
	})
	return []string{"Authorization", "Bearer " + token}, id
}

// SignOut ends the session of headers, as signing out does.
func (a *testApp) SignOut(headers []string) {
	a.tokens.revoke(strings.TrimPrefix(headers[1], "Bearer "))
}

// StartWorkers runs the app, its HTTP server on a free port and its
// background workers, until the test ends.
func (a *testApp) StartWorkers(t testing.TB) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Run() error = %v", err)
			}
		case <-time.After(30 * time.Second):
			t.Error("the app didn't stop")
		}
	})
}

// testTokens is the test authenticator: bearer tokens issued by SignIn.
type testTokens struct {
	mu         sync.Mutex
	principals map[string]auth.Principal
}

func (s *testTokens) issue(p auth.Principal) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	token := hex.EncodeToString(b)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.principals[token] = p
	return token
}

func (s *testTokens) revoke(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.principals, token)
}

// Middleware implements gorbital.Authenticator.
func (s *testTokens) Middleware(*slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// As sign-in does: audit events carry the client's address and
			// user agent.
			r = r.WithContext(auth.WithClientInfo(r.Context(), auth.ClientInfoFrom(r)))
			token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			s.mu.Lock()
			p, found := s.principals[token]
			s.mu.Unlock()
			if ok && found {
				r = r.WithContext(auth.WithPrincipal(r.Context(), p))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// reportingTokens is the test authenticator reporting sign-in methods.
type reportingTokens struct {
	*testTokens
	methods []gorbital.SignInMethod
}

// SignInMethods implements the optional authenticator method
// gorbital.Platform.SignInMethods calls.
func (s reportingTokens) SignInMethods(gorbital.Config) []gorbital.SignInMethod {
	return slices.Clone(s.methods)
}

// A response is what the app answered.
type response struct {
	Code   int
	Header http.Header
	Body   string
	JSON   map[string]any
}

// do sends a request through h; headers are name and value pairs. The
// client's address is 192.0.2.1, httptest's.
func do(t testing.TB, h http.Handler, method, path, body string, headers ...string) response {
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
	r := response{Code: rec.Code, Header: rec.Header(), Body: rec.Body.String()}
	_ = json.Unmarshal(rec.Body.Bytes(), &r.JSON)
	return r
}

// waitFor fails the test when cond isn't true within 20 seconds.
func waitFor(t testing.TB, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// exampleModule is the golden app's example module: GET /v1/ping answers the
// example.ping_message setting, with the server's time while the
// example.ping_time flag is on for the caller; the heartbeat job logs.
func exampleModule() gorbital.Module {
	var message *settings.Setting[string]
	var serverTime *flags.Flag
	return gorbital.Module{
		Name: "example",
		Settings: func(r *settings.Registry) {
			message = settings.String(r, "example.ping_message", "pong",
				settings.Describe("Reply of GET /v1/ping. An example runtime setting: change it with PUT /ops/settings/example.ping_message."),
				settings.MaxLen(100),
				settings.Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return errors.New("must not be blank")
					}
					return nil
				}),
			)
		},
		Flags: func(r *flags.Registry) {
			serverTime = flags.Bool(r, "example.ping_time",
				flags.Describe("Adds the server's time to GET /v1/ping replies. An example feature flag: turn it on with PUT /ops/flags/example.ping_time."),
				flags.Client(),
			)
		},
		Jobs: func(defs *jobs.Definitions, d gorbital.Deps) {
			jobs.Define(defs, jobs.Definition[heartbeatArgs]{
				Name:        "heartbeat",
				Description: "Logs a heartbeat. An example job: change its schedule in /ops/jobs.",
				Worker:      &heartbeatWorker{logger: d.Logger},
				NewArgs:     func() heartbeatArgs { return heartbeatArgs{} },
				Enabled:     true, Schedule: "@every 1h", Timeout: time.Minute, MaxAttempts: 3, Queue: "default", Priority: 1,
			})
		},
		Routes: func(r *gorbital.Router, _ gorbital.Deps) {
			gorbital.Get(r, "/v1/ping", func(ctx context.Context, _ *struct{}) (*pingOutput, error) {
				out := &pingOutput{}
				out.Body.Message = message.Get(ctx)
				if serverTime.Enabled(ctx) {
					now := time.Now().UTC()
					out.Body.ServerTime = &now
				}
				return out, nil
			}, gorbital.OperationID("ping"), gorbital.Tags("Example"), guard.Public())
		},
	}
}

type pingOutput struct {
	Body struct {
		Message    string     `json:"message"`
		ServerTime *time.Time `json:"server_time,omitempty"`
	}
}

type heartbeatArgs struct{}

func (heartbeatArgs) Kind() string { return "heartbeat" }

type heartbeatWorker struct {
	river.WorkerDefaults[heartbeatArgs]
	logger *slog.Logger
}

func (w *heartbeatWorker) Work(ctx context.Context, job *river.Job[heartbeatArgs]) error {
	w.logger.InfoContext(ctx, "job ran", "job", "heartbeat", "job_id", job.ID)
	return nil
}
