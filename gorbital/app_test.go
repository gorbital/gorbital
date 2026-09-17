package gorbital_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/riverqueue/river"

	"gorbital.dev/actor"
	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/gorbitaltest"
	"gorbital.dev/gorbital/guard"
	"gorbital.dev/httpx"
	"gorbital.dev/mail"
	"gorbital.dev/modules/jobs"
	"gorbital.dev/modules/settings"
)

// reminderArgs is a job a test module defines and enqueues.
type reminderArgs struct {
	BookID string `json:"book_id"`
}

func (reminderArgs) Kind() string { return "books_reminder" }

type reminderWorker struct {
	river.WorkerDefaults[reminderArgs]
	deps gorbital.Deps
}

func (w *reminderWorker) Work(context.Context, *river.Job[reminderArgs]) error { return nil }

var errNoShelf = errors.New("books: no shelf")

// catalogModule is a module using every dependency New provides: a public and
// a protected route, a permission, an error mapping, a job, and email.
func catalogModule() gorbital.Module {
	return gorbital.Module{
		Name:        "books",
		Permissions: []gorbital.Permission{{Name: "books.book.write", Description: "Add books", Roles: []string{"user"}}},
		Errors:      []httpx.Mapping{{Err: errNoShelf, Status: http.StatusConflict, Code: "no_shelf", Detail: "create a shelf first"}},
		Jobs: func(defs *jobs.Definitions, d gorbital.Deps) {
			jobs.Define(defs, jobs.Definition[reminderArgs]{
				Name: "books_reminder", Worker: &reminderWorker{deps: d}, NewArgs: func() reminderArgs { return reminderArgs{} },
			})
		},
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			gorbital.Get(r, "/v1/catalog/{id}", getBook, guard.Public())
			gorbital.Post(r, "/v1/books", func(ctx context.Context, in *createInput) (*bookOutput, error) {
				if in.Body.Title == "no shelf" {
					return nil, errNoShelf
				}
				if err := d.Mailer.Send(ctx, mail.Message{To: []mail.Address{{Email: "reader@example.com"}}, Subject: "Added " + in.Body.Title, Text: "Enjoy."}); err != nil {
					return nil, err
				}
				if _, err := d.Jobs.Insert(ctx, reminderArgs{BookID: "bok_1"}, nil); err != nil {
					return nil, err
				}
				return createBook(ctx, in)
			}, guard.Permission("books.book.write"), gorbital.Status(http.StatusCreated))
		},
	}
}

func TestNewServesModulesThroughTheStack(t *testing.T) {
	app := gorbitaltest.New(t, gorbital.WithName("shelf"), gorbital.WithModules(catalogModule()))
	anon := app.Client()

	for _, path := range []string{"/livez", "/readyz", "/version", "/docs", "/openapi.json"} {
		anon.Get(path).AssertStatus(t, http.StatusOK)
	}
	anon.Get("/v1/nope").AssertProblem(t, http.StatusNotFound, "not_found")
	anon.Get("/v1/catalog/bok_1").AssertStatus(t, http.StatusOK)
	anon.Post("/v1/books", map[string]string{"title": "Dune"}).AssertProblem(t, http.StatusUnauthorized, "unauthenticated")
	app.As(gorbitaltest.User("usr_1")).Post("/v1/books", map[string]string{"title": "Dune"}).AssertProblem(t, http.StatusForbidden, "forbidden")
	app.As(gorbitaltest.APIKey("usr_1", "books.book.read")).Post("/v1/books", map[string]string{"title": "Dune"}).AssertProblem(t, http.StatusForbidden, "forbidden")

	writer := app.As(gorbitaltest.User("usr_1", "books.book.write"))
	writer.Post("/v1/books", map[string]string{"title": "no shelf"}).AssertProblem(t, http.StatusConflict, "no_shelf")
	res := writer.WithHeader("Idempotency-Key", "k1").Post("/v1/books", map[string]string{"title": "Dune"})
	res.AssertStatus(t, http.StatusCreated)
	var book struct{ Title string }
	res.JSON(t, &book)
	if book.Title != "Dune" {
		t.Errorf("created book = %+v", book)
	}
	// The retry is answered from the stored response (idempotency keys).
	replay := writer.WithHeader("Idempotency-Key", "k1").Post("/v1/books", map[string]string{"title": "Dune"})
	replay.AssertStatus(t, http.StatusCreated)
	if replay.Header.Get("Idempotent-Replayed") != "true" {
		t.Errorf("retry with the same Idempotency-Key: Idempotent-Replayed = %q, want true", replay.Header.Get("Idempotent-Replayed"))
	}

	sent := app.Mail(t)
	if len(sent) != 1 || sent[0].Subject != "Added Dune" || sent[0].From.Email != "no-reply@example.com" || sent[0].From.Name != "shelf" {
		t.Errorf("queued mail = %+v, want one from the mail.* settings' defaults", sent)
	}
	reminders := app.Jobs(t, "books_reminder")
	var args reminderArgs
	if len(reminders) != 1 || json.Unmarshal(reminders[0].Args, &args) != nil || args.BookID != "bok_1" {
		t.Errorf("enqueued reminders = %+v", reminders)
	}

	deps := app.App().Deps()
	if deps.DB == nil || deps.Audit == nil || deps.Jobs == nil || deps.Settings == nil || deps.Flags == nil || deps.Storage == nil || deps.RateLimits == nil || deps.Mailer == nil {
		t.Errorf("Deps() = %+v, want every dependency set", deps)
	}
}

func TestMaintenanceModeThroughTheStack(t *testing.T) {
	app := gorbitaltest.New(t, gorbital.WithModules(catalogModule()))
	ctx := actor.With(context.Background(), actor.System("test"))
	store := app.App().Deps().Settings
	set := func(key string, value any) {
		t.Helper()
		raw, _ := json.Marshal(value)
		current, err := store.Get(key)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.Set(ctx, key, raw, settings.Change{Version: current.Version, Reason: "test"}); err != nil {
			t.Fatalf("set %s: %v", key, err)
		}
	}
	set("maintenance.message", "Upgrading the database")
	set("maintenance.enabled", true)

	res := app.Client().Get("/v1/catalog/bok_1")
	res.AssertProblem(t, http.StatusServiceUnavailable, "maintenance")
	if res.Header.Get("Retry-After") != "300" {
		t.Errorf("Retry-After = %q, want 300", res.Header.Get("Retry-After"))
	}
	for _, path := range []string{"/livez", "/readyz", "/version", "/openapi.json", "/docs"} {
		app.Client().Get(path).AssertStatus(t, http.StatusOK)
	}
	if res := app.Client().Post("/v1/auth/login", map[string]string{}); res.Status == http.StatusServiceUnavailable {
		t.Errorf("POST /v1/auth/login in maintenance = 503, want sign-in to stay open")
	}
}

type slowInput struct{}

type slowOutput struct {
	Body struct {
		Done bool `json:"done"`
	}
}

// TestRouteTimeout: gorbital.Timeout shortens a route's deadline inside the
// stack, answering 503 request_timeout and cancelling the handler's context.
func TestRouteTimeout(t *testing.T) {
	cancelled := make(chan struct{})
	module := gorbital.Module{
		Name: "reports",
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			gorbital.Get(r, "/v1/reports/slow", func(ctx context.Context, _ *slowInput) (*slowOutput, error) {
				select {
				case <-ctx.Done():
					close(cancelled)
					return nil, ctx.Err()
				case <-time.After(5 * time.Second):
					return &slowOutput{}, nil
				}
			}, guard.Public(), gorbital.Timeout(50*time.Millisecond))
		},
	}
	app := gorbitaltest.New(t, gorbital.WithModules(module))
	app.Client().Get("/v1/reports/slow").AssertProblem(t, http.StatusServiceUnavailable, "request_timeout")
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("the handler's context wasn't cancelled")
	}
}
