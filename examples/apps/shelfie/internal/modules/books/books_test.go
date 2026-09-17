package books_test

import (
	"context"
	"net/http"
	"testing"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/gorbitaltest"

	"example.com/shelfie/db/migrations"
	"example.com/shelfie/internal/modules/books"
	"example.com/shelfie/internal/modules/books/usecase"
)

// docs:start new-app

// newApp builds Shelfie with the books module on a new database for the
// test: gorbital.Migrate with db/migrations, then gorbital.New.
func newApp(t *testing.T) *gorbitaltest.App {
	t.Helper()
	return gorbitaltest.New(t,
		gorbital.WithModules(books.Module()),
		gorbital.WithMigrations(migrations.FS),
	)
}

// docs:end new-app

type book struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Author string `json:"author"`
	ISBN   string `json:"isbn"`
	Status string `json:"status"`
}

// docs:start create-and-read

func TestAddAndReadBooks(t *testing.T) {
	app := newApp(t)
	ada := app.As(gorbitaltest.User("usr_ada", usecase.PermRead, usecase.PermWrite))

	res := ada.Post("/v1/books", map[string]string{"title": " Dune ", "author": "Frank Herbert", "isbn": "978-0-441-17271-9"})
	res.AssertStatus(t, http.StatusCreated)
	var created book
	res.JSON(t, &created)
	if created.Title != "Dune" || created.ISBN != "9780441172719" || created.Status != "want_to_read" {
		t.Errorf("created = %+v, want the title trimmed, the ISBN without hyphens and the default status", created)
	}

	var got book
	ada.Get("/v1/books/"+created.ID).JSON(t, &got)
	if got != created {
		t.Errorf("GET = %+v, want %+v", got, created)
	}
}

// docs:end create-and-read

func TestListUpdateAndDeleteBooks(t *testing.T) {
	app := newApp(t)
	ada := app.As(gorbitaltest.User("usr_ada", usecase.PermRead, usecase.PermWrite))
	for _, title := range []string{"Dune", "Emma", "Kindred"} {
		ada.Post("/v1/books", map[string]string{"title": title}).AssertStatus(t, http.StatusCreated)
	}

	var list struct{ Items []book }
	ada.Get("/v1/books?limit=2").JSON(t, &list)
	if len(list.Items) != 2 || list.Items[0].Title != "Kindred" || list.Items[1].Title != "Emma" {
		t.Fatalf("GET /v1/books?limit=2 = %+v, want the two newest", list.Items)
	}

	res := ada.Patch("/v1/books/"+list.Items[0].ID, map[string]string{"status": "reading", "author": "Octavia E. Butler"})
	res.AssertStatus(t, http.StatusOK)
	var updated book
	res.JSON(t, &updated)
	if updated.Status != "reading" || updated.Author != "Octavia E. Butler" || updated.Title != "Kindred" {
		t.Errorf("PATCH = %+v", updated)
	}
	list.Items = nil
	ada.Get("/v1/books?status=reading").JSON(t, &list)
	if len(list.Items) != 1 || list.Items[0].ID != updated.ID {
		t.Errorf("GET /v1/books?status=reading = %+v", list.Items)
	}

	ada.Delete("/v1/books/"+updated.ID).AssertStatus(t, http.StatusNoContent)
	ada.Get("/v1/books/"+updated.ID).AssertProblem(t, http.StatusNotFound, "book_not_found")
	ada.Delete("/v1/books/"+updated.ID).AssertProblem(t, http.StatusNotFound, "book_not_found")

	var events int
	if err := app.App().Deps().DB.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_events WHERE action LIKE 'books.book.%' AND actor_id = 'usr_ada'`).Scan(&events); err != nil || events != 5 {
		t.Errorf("audit events = %d, %v; want 3 created, 1 updated and 1 deleted", events, err)
	}
}

// docs:start protection

func TestBooksAreProtected(t *testing.T) {
	app := newApp(t)
	ada := app.As(gorbitaltest.User("usr_ada", usecase.PermRead, usecase.PermWrite))
	res := ada.Post("/v1/books", map[string]string{"title": "Dune"})
	var dune book
	res.JSON(t, &dune)

	// Deny by default: no route of the module is public.
	app.Client().Get("/v1/books").AssertProblem(t, http.StatusUnauthorized, "unauthenticated")
	// Another reader can't see Ada's book, nor tell whether it exists.
	bob := app.As(gorbitaltest.User("usr_bob", usecase.PermRead, usecase.PermWrite))
	bob.Get("/v1/books/"+dune.ID).AssertProblem(t, http.StatusNotFound, "book_not_found")
	// An API key scoped to reading can't change anything.
	readOnly := app.As(gorbitaltest.APIKey("usr_ada", usecase.PermRead))
	readOnly.Get("/v1/books/"+dune.ID).AssertStatus(t, http.StatusOK)
	readOnly.Delete("/v1/books/"+dune.ID).AssertProblem(t, http.StatusForbidden, "forbidden")
}

// docs:end protection

func TestBookRules(t *testing.T) {
	app := newApp(t)
	ada := app.As(gorbitaltest.User("usr_ada", usecase.PermRead, usecase.PermWrite))
	ada.Post("/v1/books", map[string]string{"title": "Dune", "isbn": "0441172717"}).AssertStatus(t, http.StatusCreated)

	for _, tt := range []struct {
		name   string
		body   map[string]string
		status int
		code   string
	}{
		{"blank title", map[string]string{"title": "   "}, http.StatusUnprocessableEntity, "title_required"},
		{"missing title", map[string]string{"author": "Frank Herbert"}, http.StatusUnprocessableEntity, "validation_failed"},
		{"invalid ISBN", map[string]string{"title": "Dune", "isbn": "12345"}, http.StatusUnprocessableEntity, "invalid_isbn"},
		{"unknown status", map[string]string{"title": "Dune", "status": "abandoned"}, http.StatusUnprocessableEntity, "validation_failed"},
		{"ISBN already on the shelf", map[string]string{"title": "Dune (again)", "isbn": "0-441-17271-7"}, http.StatusConflict, "isbn_taken"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ada.Post("/v1/books", tt.body).AssertProblem(t, tt.status, tt.code)
		})
	}
	// The same ISBN on another reader's shelf is fine.
	app.As(gorbitaltest.User("usr_bob", usecase.PermWrite)).Post("/v1/books", map[string]string{"title": "Dune", "isbn": "0441172717"}).AssertStatus(t, http.StatusCreated)
}

// docs:start mail-and-jobs

// TestNothingIsQueued shows how a test reads what the app queued: email a
// module sends through Deps.Mailer and jobs it enqueues through Deps.Jobs.
// The books module sends neither yet.
func TestNothingIsQueued(t *testing.T) {
	app := newApp(t)
	app.As(gorbitaltest.User("usr_ada", usecase.PermWrite)).Post("/v1/books", map[string]string{"title": "Dune"}).AssertStatus(t, http.StatusCreated)
	if sent := app.Mail(t); len(sent) != 0 {
		t.Errorf("queued email = %+v, want none", sent)
	}
	if queued := app.Jobs(t, ""); len(queued) != 0 {
		t.Errorf("enqueued jobs = %+v, want none", queued)
	}
}

// docs:end mail-and-jobs
