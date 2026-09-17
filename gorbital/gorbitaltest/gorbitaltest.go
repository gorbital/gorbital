// Package gorbitaltest tests a gorbital app through its real middleware
// stack, on its own PostgreSQL database per test (ADR-0028):
//
//	func TestCreateBook(t *testing.T) {
//		app := gorbitaltest.New(t, gorbital.WithModules(books.Module()), gorbital.WithMigrations(migrations.FS))
//
//		res := app.As(gorbitaltest.User("usr_1", usecase.PermWrite)).Post("/v1/books", map[string]any{"title": "Dune"})
//		res.AssertStatus(t, http.StatusCreated)
//
//		app.Client().Get("/v1/books").AssertProblem(t, http.StatusUnauthorized, "unauthenticated")
//	}
//
// [New] creates a database on the server GORBITAL_TEST_DATABASE_URL names,
// migrates it with gorbital.Migrate and builds the app with gorbital.New.
// Tests are skipped when the variable is unset, and fail instead with
// GORBITAL_REQUIRE_DB=1, as in CI.
//
// Requests carry a principal set with [App.As], as the authenticator would
// set it, so tests don't sign in. Email modules send and jobs they enqueue
// are stored as jobs, which [App.Mail] and [App.Jobs] read back: workers
// don't run in tests, so nothing is delivered.
//
// Don't build apps in parallel tests of one package (t.Parallel): Huma keeps
// its error constructor in a package-level variable, which each app sets to
// its own error mappings (openapi.InstallErrors), so two apps built at once
// race.
//
// Stability: experimental until v0.2.0 (ADR-0015, ADR-0081).
package gorbitaltest

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gorbital.dev/config"
	"gorbital.dev/gorbital"
	"gorbital.dev/mail"
	"gorbital.dev/modules/auth"
	"gorbital.dev/modules/jobs"
	"gorbital.dev/modules/postgres/pgtest"
)

// An App is a gorbital app built for one test.
type App struct {
	t   testing.TB
	app *gorbital.App
}

// New builds an app with opts on a new, migrated database for the test, and
// closes it when the test ends. The configuration is development's
// defaults, with logs at warning level and above written to the test's
// output, and the log archive and local file storage in temporary
// directories. opts are applied after gorbitaltest's own, so a
// gorbital.WithLogger or gorbital.WithAuth of the test's wins.
func New(t testing.TB, opts ...gorbital.Option) *App {
	t.Helper()
	url := pgtest.NewDatabase(t)
	env := map[string]string{
		"APP_ENV":           "development",
		"DATABASE_URL":      url,
		"APP_DB_MAX_CONNS":  "4",
		"APP_JOB_WORKERS":   "1",
		"LOG_ARCHIVE_DIR":   t.TempDir(),
		"STORAGE_LOCAL_DIR": t.TempDir(),
	}
	cfg, err := gorbital.LoadConfig(config.Source{Getenv: func(key string) string { return env[key] }})
	if err != nil {
		t.Fatalf("gorbitaltest: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(t.Output(), &slog.HandlerOptions{Level: slog.LevelWarn}))
	opts = append([]gorbital.Option{gorbital.WithLogger(logger), gorbital.WithAuth(principals{})}, opts...)

	ctx := context.Background()
	if err := gorbital.Migrate(ctx, cfg, io.Discard, opts...); err != nil {
		t.Fatalf("gorbitaltest: migrate: %v", err)
	}
	a, err := gorbital.New(ctx, cfg, opts...)
	if err != nil {
		t.Fatalf("gorbitaltest: new: %v", err)
	}
	// Cleanups run in reverse order: the app closes before its database is
	// dropped.
	t.Cleanup(func() {
		if err := a.Close(context.WithoutCancel(ctx)); err != nil {
			t.Errorf("gorbitaltest: close: %v", err)
		}
	})
	return &App{t: t, app: a}
}

// principals is the test app's authenticator: requests carry the principal
// the Client put in their context. An authenticator the test passes with
// gorbital.WithAuth replaces it.
type principals struct{}

func (principals) Middleware(*slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler { return next }
}

// App returns the built app, for its handler and its dependencies, such as
// Deps().DB to prepare rows.
func (a *App) App() *gorbital.App { return a.app }

// Client returns a client whose requests carry no principal, as a caller
// that isn't signed in.
func (a *App) Client() *Client { return &Client{app: a, header: http.Header{}} }

// As returns a client whose requests carry p, as the authenticator sets it
// for a signed-in caller: see [User] and [APIKey].
func (a *App) As(p auth.Principal) *Client {
	return &Client{app: a, principal: &p, header: http.Header{}}
}

// User returns the principal of a signed-in user with a session holding
// permissions. The session signed in and verified a second factor just now,
// so guard.RecentReauth allows it.
func User(id string, permissions ...string) auth.Principal {
	now := time.Now()
	return auth.Principal{
		UserID: id, SessionID: "ses_test_" + id, Permissions: permissions,
		SignedInAt: now, MFAVerified: true, MFAVerifiedAt: now,
	}
}

// APIKey returns the principal of a request authenticated with an API key
// of the user userID, scoped to scopes: the actor holds exactly those
// permissions, and guards that need a session, such as guard.RecentReauth,
// refuse it.
func APIKey(userID string, scopes ...string) auth.Principal {
	return auth.Principal{UserID: userID, APIKeyID: "key_test_" + userID, Permissions: scopes, Scopes: scopes}
}

// A Client sends requests to an app's handler, through its whole middleware
// stack.
type Client struct {
	app       *App
	principal *auth.Principal
	header    http.Header
}

// WithHeader returns a copy of c that sends the header on every request,
// such as Idempotency-Key.
func (c *Client) WithHeader(name, value string) *Client {
	h := c.header.Clone()
	h.Set(name, value)
	return &Client{app: c.app, principal: c.principal, header: h}
}

// Get sends a GET request for path, which may have a query string.
func (c *Client) Get(path string) *Response { return c.send(http.MethodGet, path, nil) }

// Post sends a POST request with body encoded as JSON; a nil body sends
// none.
func (c *Client) Post(path string, body any) *Response { return c.send(http.MethodPost, path, body) }

// Put sends a PUT request with body encoded as JSON.
func (c *Client) Put(path string, body any) *Response { return c.send(http.MethodPut, path, body) }

// Patch sends a PATCH request with body encoded as JSON.
func (c *Client) Patch(path string, body any) *Response { return c.send(http.MethodPatch, path, body) }

// Delete sends a DELETE request for path.
func (c *Client) Delete(path string) *Response { return c.send(http.MethodDelete, path, nil) }

// Do sends req as it is, with the client's principal and headers added.
func (c *Client) Do(req *http.Request) *Response {
	c.app.t.Helper()
	for name, values := range c.header {
		req.Header[name] = values
	}
	if c.principal != nil {
		req = req.WithContext(auth.WithPrincipal(req.Context(), *c.principal))
	}
	rec := httptest.NewRecorder()
	c.app.app.Handler().ServeHTTP(rec, req)
	return &Response{Status: rec.Code, Header: rec.Header(), Body: rec.Body.Bytes(), method: req.Method, path: req.URL.Path}
}

func (c *Client) send(method, path string, body any) *Response {
	c.app.t.Helper()
	var r io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			c.app.t.Fatalf("gorbitaltest: %s %s: encode body: %v", method, path, err)
		}
		r = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.Do(req)
}

// A Response is what the app answered.
type Response struct {
	Status int
	Header http.Header
	Body   []byte

	method, path string
}

// JSON decodes the body into v, failing the test when it isn't JSON of
// that shape.
func (r *Response) JSON(t testing.TB, v any) {
	t.Helper()
	if err := json.Unmarshal(r.Body, v); err != nil {
		t.Fatalf("%s %s: decode body %s: %v", r.method, r.path, r.Body, err)
	}
}

// AssertStatus fails the test when the response's status isn't want,
// showing the body.
func (r *Response) AssertStatus(t testing.TB, want int) {
	t.Helper()
	if r.Status != want {
		t.Fatalf("%s %s = %d %s, want %d", r.method, r.path, r.Status, r.Body, want)
	}
}

// AssertProblem fails the test unless the response is a problem+json error
// with status and code, such as 404 and "book_not_found".
func (r *Response) AssertProblem(t testing.TB, status int, code string) {
	t.Helper()
	var p struct {
		Code string `json:"code"`
	}
	ct := r.Header.Get("Content-Type")
	if r.Status != status || !strings.HasPrefix(ct, "application/problem+json") || json.Unmarshal(r.Body, &p) != nil || p.Code != code {
		t.Fatalf("%s %s = %d %s (%s), want a %d %s problem", r.method, r.path, r.Status, r.Body, ct, status, code)
	}
}

// A Job is a job the app enqueued.
type Job struct {
	// Kind is the job's name, such as "gorbital.mail.send".
	Kind string
	// Args are the job's arguments as stored, in JSON.
	Args json.RawMessage
}

// Jobs returns the jobs the app enqueued of kind, oldest first, or every
// job when kind is empty.
func (a *App) Jobs(t testing.TB, kind string) []Job {
	t.Helper()
	rows, err := a.app.Deps().DB.Query(context.Background(),
		`SELECT kind, args FROM river_job WHERE $1 = '' OR kind = $1 ORDER BY id`, kind)
	if err != nil {
		t.Fatalf("gorbitaltest: read jobs: %v", err)
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		var j Job
		if err := rows.Scan(&j.Kind, &j.Args); err != nil {
			t.Fatalf("gorbitaltest: read jobs: %v", err)
		}
		out = append(out, j)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("gorbitaltest: read jobs: %v", err)
	}
	return out
}

// queuedMail is the stored form of a queued email (jobs.MailKind), whose
// JSON field names are fixed so queued jobs survive library upgrades.
type queuedMail struct {
	Message struct {
		From    queuedAddress     `json:"from"`
		To      []queuedAddress   `json:"to"`
		ReplyTo []queuedAddress   `json:"reply_to"`
		Subject string            `json:"subject"`
		Text    string            `json:"text"`
		HTML    string            `json:"html"`
		Tags    map[string]string `json:"tags"`
	} `json:"message"`
}

type queuedAddress struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

func (q queuedAddress) address() mail.Address { return mail.Address{Name: q.Name, Email: q.Email} }

// Mail returns the email the app queued, oldest first, with the sender the
// mail.* runtime settings filled in.
func (a *App) Mail(t testing.TB) []mail.Message {
	t.Helper()
	var out []mail.Message
	for _, j := range a.Jobs(t, jobs.MailKind) {
		var q queuedMail
		if err := json.Unmarshal(j.Args, &q); err != nil {
			t.Fatalf("gorbitaltest: read queued mail: %v", err)
		}
		m := mail.Message{From: q.Message.From.address(), Subject: q.Message.Subject, Text: q.Message.Text, HTML: q.Message.HTML, Tags: q.Message.Tags}
		for _, to := range q.Message.To {
			m.To = append(m.To, to.address())
		}
		for _, r := range q.Message.ReplyTo {
			m.ReplyTo = append(m.ReplyTo, r.address())
		}
		out = append(out, m)
	}
	return out
}
