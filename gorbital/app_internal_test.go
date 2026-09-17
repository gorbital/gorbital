package gorbital

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/riverqueue/river"

	"gorbital.dev/actor"
	lifecycle "gorbital.dev/app"
	"gorbital.dev/mail"
	"gorbital.dev/modules/jobs"
	"gorbital.dev/modules/postgres/pgtest"
	"gorbital.dev/modules/settings"
	"gorbital.dev/modules/storage"
)

// testApp migrates a new database and builds an app on it, logging to logs.
func testApp(t *testing.T, vars map[string]string, logs io.Writer, opts ...Option) (*App, error) {
	t.Helper()
	all := map[string]string{"DATABASE_URL": pgtest.NewDatabase(t), "LOG_ARCHIVE_DIR": t.TempDir(), "STORAGE_LOCAL_DIR": t.TempDir(), "APP_ADDR": "127.0.0.1:0"}
	for k, v := range vars {
		all[k] = v
	}
	cfg, err := LoadConfig(env(all))
	if err != nil {
		t.Fatal(err)
	}
	opts = append([]Option{WithLogger(slog.New(slog.NewTextHandler(logs, nil)))}, opts...)
	ctx := context.Background()
	if err := Migrate(ctx, cfg, io.Discard, opts...); err != nil {
		t.Fatal(err)
	}
	a, err := New(ctx, cfg, opts...)
	if err == nil {
		t.Cleanup(func() { _ = a.Close(ctx) })
	}
	return a, err
}

// principalAuth sets a user actor on requests with an Authorization header.
type principalAuth struct{}

func (principalAuth) Middleware(*slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "" {
				r = r.WithContext(actor.With(r.Context(), actor.Actor{Kind: actor.KindUser, ID: "usr_1"}))
			}
			next.ServeHTTP(w, r)
		})
	}
}

func TestMiddlewareOrder(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	record := func(name string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				a, _ := actor.From(r.Context())
				mu.Lock()
				seen = append(seen, name+":"+a.ID)
				mu.Unlock()
				next.ServeHTTP(w, r)
			})
		}
	}
	var gotDeps Deps
	a, err := testApp(t, nil, io.Discard,
		WithAuth(principalAuth{}),
		WithMiddleware(record("first")),
		WithMiddlewareFunc(func(d Deps) func(http.Handler) http.Handler { gotDeps = d; return record("second") }),
		WithStack(func(s Stack) []func(http.Handler) http.Handler {
			return append([]func(http.Handler) http.Handler{record("outermost")}, s.Default()...)
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/livez", nil)
	req.Header.Set("Authorization", "Bearer x")
	a.Handler().ServeHTTP(httptest.NewRecorder(), req)
	if want := []string{"outermost:", "first:usr_1", "second:usr_1"}; !slices.Equal(seen, want) {
		t.Errorf("middleware ran as %q, want %q: the stack first, app middleware after authentication in option order", seen, want)
	}
	if gotDeps.DB == nil || gotDeps.Jobs == nil {
		t.Errorf("WithMiddlewareFunc got %+v, want the app's dependencies", gotDeps)
	}
}

func TestCustomStackWarnings(t *testing.T) {
	var logs bytes.Buffer
	_, err := testApp(t, nil, &logs, WithStack(func(s Stack) []func(http.Handler) http.Handler {
		return []func(http.Handler) http.Handler{s.RequestID, s.AccessLog}
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"no Recover step", "no Auth step"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("logs lack %q:\n%s", want, logs.String())
		}
	}

	logs.Reset()
	if _, err := testApp(t, nil, &logs, WithAuth(principalAuth{})); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.String(), "no Recover step") || strings.Contains(logs.String(), "no Auth step") || strings.Contains(logs.String(), "no authenticator") {
		t.Errorf("the default stack with an authenticator logged a warning:\n%s", logs.String())
	}

	logs.Reset()
	if _, err := testApp(t, nil, &logs, WithModules(Module{Name: "books", Routes: func(r *Router, _ Deps) {
		Get(r, "/v1/books", func(context.Context, *struct{}) (*struct{}, error) { return nil, nil })
	}})); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "no authenticator") || !strings.Contains(logs.String(), "unreachable_routes=1") {
		t.Errorf("without an authenticator, logs = %s; want the unreachable routes counted", logs.String())
	}
}

type recordingSender struct{ sent []mail.Message }

func (s *recordingSender) Send(_ context.Context, m mail.Message) error {
	s.sent = append(s.sent, m)
	return nil
}

func TestNewConfigurationErrors(t *testing.T) {
	s3 := map[string]string{"STORAGE_DRIVER": "s3", "STORAGE_REGION": "eu-west-1", "STORAGE_BUCKET": "b", "STORAGE_ACCESS_KEY": "a", "STORAGE_SECRET_KEY": "s"}
	if _, err := testApp(t, s3, io.Discard); !errors.Is(err, errInvalidConfig) || !strings.Contains(err.Error(), "STORAGE_DRIVER=s3 needs its client") {
		t.Errorf("S3 without WithStorage: error = %v", err)
	}
	var opened Config
	store := storage.Store(nil)
	if _, err := testApp(t, s3, io.Discard, WithStorageFunc(func(cfg Config) (storage.Store, error) { opened = cfg; return store, errors.New("bad endpoint") })); !errors.Is(err, errInvalidConfig) || !strings.Contains(err.Error(), "bad endpoint") || opened.Storage.Bucket != "b" {
		t.Errorf("WithStorageFunc failing: error = %v, cfg = %+v", err, opened.Storage)
	}
	keepBuiltIn := WithStorageFunc(func(Config) (storage.Store, error) { return nil, nil })
	if _, err := testApp(t, s3, io.Discard, keepBuiltIn); !errors.Is(err, errInvalidConfig) || !strings.Contains(err.Error(), "STORAGE_DRIVER=s3 needs its client") {
		t.Errorf("WithStorageFunc returning no store for S3: error = %v", err)
	}
	if _, err := testApp(t, nil, io.Discard, keepBuiltIn); err != nil {
		t.Errorf("WithStorageFunc returning no store for local: error = %v, want the local driver", err)
	}
	provider := map[string]string{"MAIL_DELIVERY": "provider"}
	if _, err := testApp(t, provider, io.Discard); !errors.Is(err, errInvalidConfig) || !strings.Contains(err.Error(), "MAIL_DELIVERY=provider needs an email provider") {
		t.Errorf("provider without WithMailer: error = %v", err)
	}
	if _, err := testApp(t, provider, io.Discard, WithMailer(&recordingSender{})); err != nil {
		t.Errorf("provider with WithMailer: error = %v", err)
	}
	if _, err := testApp(t, nil, io.Discard, WithMailerFunc(func(Config) (mail.Sender, error) { return nil, errors.New("unused") })); err != nil {
		t.Errorf("devmail ignores the provider: error = %v", err)
	}
	if _, err := New(context.Background(), Config{Env: "development"}); !errors.Is(err, errInvalidConfig) || !strings.Contains(err.Error(), "DATABASE_URL is required") {
		t.Errorf("New() without DATABASE_URL = %v", err)
	}
}

type noteArgs struct{}

func (noteArgs) Kind() string { return "notes_digest" }

type noteWorker struct {
	river.WorkerDefaults[noteArgs]
}

func (noteWorker) Work(context.Context, *river.Job[noteArgs]) error { return nil }

func defineNotes(defs *jobs.Definitions, _ Deps) {
	jobs.Define(defs, jobs.Definition[noteArgs]{Name: "notes_digest", Worker: noteWorker{}, NewArgs: func() noteArgs { return noteArgs{} }})
}

type retentionArgs struct{}

func (retentionArgs) Kind() string { return "retention" }

type retentionWorker struct {
	river.WorkerDefaults[retentionArgs]
}

func (retentionWorker) Work(context.Context, *river.Job[retentionArgs]) error { return nil }

func TestNewDuplicateDeclarations(t *testing.T) {
	tests := []struct {
		name    string
		modules []Module
		want    string
	}{
		{"job", []Module{{Name: "notes", Jobs: defineNotes}, {Name: "digests", Jobs: defineNotes}}, `job "notes_digest" is defined by modules "notes" and "digests"`},
		{"built-in job", []Module{{Name: "cleanup", Jobs: func(defs *jobs.Definitions, _ Deps) {
			jobs.Define(defs, jobs.Definition[retentionArgs]{Name: "retention", Worker: retentionWorker{}, NewArgs: func() retentionArgs { return retentionArgs{} }})
		}}}, `job "retention" is defined by modules "gorbital" and "cleanup"`},
		{"permission", []Module{
			{Name: "notes", Permissions: []Permission{{Name: "notes.note.read"}}},
			{Name: "digests", Permissions: []Permission{{Name: "notes.note.read"}}},
		}, `permission "notes.note.read" is declared by modules "notes" and "digests"`},
		{"route", []Module{
			{Name: "notes", Routes: func(r *Router, _ Deps) { Get(r, "/v1/notes", noteHandler) }},
			{Name: "digests", Routes: func(r *Router, _ Deps) { Get(r, "/v1/notes", noteHandler, OperationID("x")) }},
		}, `GET /v1/notes is registered by modules "notes" and "digests"`},
		{"built-in setting", []Module{{Name: "notes", Settings: func(r *settings.Registry) { settings.String(r, "maintenance.message", "") }}}, `module "notes": settings`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := testApp(t, nil, io.Discard, WithModules(tt.modules...))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("New() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func noteHandler(context.Context, *struct{}) (*struct{}, error) { return nil, nil }

// TestShutdownOrder: Run marks the app not ready before the server and
// workers stop, and closes resources after they have, in reverse order.
func TestShutdownOrder(t *testing.T) {
	a, err := testApp(t, nil, io.Discard, WithAuth(principalAuth{}))
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var events []string
	note := func(e string) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	}
	readiness := func() int {
		rec := httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		return rec.Code
	}
	// A probe runner stopping with the others, and a resource registered
	// last, so it closes first.
	probe := lifecycle.RunnerFunc(func(ctx context.Context) error {
		note("running")
		<-ctx.Done()
		note("runners stopping: readyz " + http.StatusText(readiness()))
		return nil
	})
	a.cleanup.Add("probe", func(context.Context) error {
		ping := "database open"
		if a.deps.DB.Ping(context.Background()) != nil {
			ping = "database closed"
		}
		note("closing resources: " + ping)
		return nil
	})

	server := lifecycle.RunnerFunc(func(ctx context.Context) error { <-ctx.Done(); return nil })
	runners := append(a.runners(server), probe)
	if len(runners) < 7 {
		t.Fatalf("runners = %d, want the server and the settings, flags, jobs, job manager, release and collector workers", len(runners))
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- lifecycle.Run(ctx, runners, append(a.runOptions(), lifecycle.WithSignals())...) }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		mu.Lock()
		started := len(events) > 0
		mu.Unlock()
		if started || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if code := readiness(); code != http.StatusOK {
		t.Fatalf("readyz while running = %d", code)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() = %v", err)
	}
	want := []string{"running", "runners stopping: readyz Service Unavailable", "closing resources: database open"}
	if !slices.Equal(events, want) {
		t.Errorf("shutdown = %q, want %q", events, want)
	}
	if a.deps.DB.Ping(context.Background()) == nil {
		t.Error("the database pool is still open after Run returned")
	}
}
