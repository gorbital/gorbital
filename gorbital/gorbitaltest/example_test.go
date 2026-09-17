package gorbitaltest_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/gorbitaltest"
	"gorbital.dev/gorbital/guard"
	"gorbital.dev/mail"
)

// The examples test this module: a public catalog, and a route that needs
// books.book.write and sends an email.
type bookBody struct {
	Body struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
}

type addBookInput struct {
	Body struct {
		Title string `json:"title" minLength:"1"`
	}
}

func booksModule() gorbital.Module {
	return gorbital.Module{
		Name: "books",
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			gorbital.Get(r, "/v1/catalog/{id}", func(_ context.Context, in *struct {
				ID string `path:"id"`
			}) (*bookBody, error) {
				out := &bookBody{}
				out.Body.ID, out.Body.Title = in.ID, "Dune"
				return out, nil
			}, guard.Public())
			gorbital.Post(r, "/v1/books", func(ctx context.Context, in *addBookInput) (*bookBody, error) {
				if err := d.Mailer.Send(ctx, mail.Message{To: []mail.Address{{Email: "reader@example.com"}}, Subject: "Added " + in.Body.Title, Text: "Happy reading."}); err != nil {
					return nil, err
				}
				out := &bookBody{}
				out.Body.ID, out.Body.Title = "bok_1", in.Body.Title
				return out, nil
			}, guard.Permission("books.book.write"), gorbital.Status(http.StatusCreated))
		},
	}
}

// test holds body for TestExamples, which runs it: examples can't receive
// a *testing.T.
func test(body func(t *testing.T)) { bodies = append(bodies, body) }

var bodies []func(t *testing.T)

func ExampleNew() {
	test(func(t *testing.T) {
		app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
		app.Client().Get("/v1/catalog/bok_1").AssertStatus(t, http.StatusOK)
	})
}

func ExampleNewWithEnv() {
	test(func(t *testing.T) {
		// The app's configuration, as .env would set it.
		app := gorbitaltest.NewWithEnv(t, map[string]string{"APP_MAX_BODY_BYTES": "64"}, gorbital.WithModules(booksModule()))
		long := strings.Repeat("a", 100)
		app.As(gorbitaltest.User("usr_1", "books.book.write")).Post("/v1/books", map[string]string{"title": long}).AssertProblem(t, http.StatusRequestEntityTooLarge, "request_too_large")
		if app.Config().MaxBodyBytes != 64 {
			t.Errorf("MaxBodyBytes = %d", app.Config().MaxBodyBytes)
		}
	})
}

func ExampleApp_Config() {
	test(func(t *testing.T) {
		app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
		// Commands such as authhttp's grant-role open the test's database
		// from the configuration.
		if app.Config().DatabaseURL.IsZero() {
			t.Error("no database URL")
		}
	})
}

func ExampleApp() {
	test(func(t *testing.T) {
		app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
		app.As(gorbitaltest.User("usr_1", "books.book.write")).Post("/v1/books", map[string]string{"title": "Dune"}).AssertStatus(t, http.StatusCreated)
	})
}

func ExampleApp_App() {
	test(func(t *testing.T) {
		app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
		// Prepare rows through the app's own pool.
		if _, err := app.App().Deps().DB.Exec(context.Background(), `SELECT 1`); err != nil {
			t.Fatal(err)
		}
	})
}

func ExampleApp_Client() {
	test(func(t *testing.T) {
		app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
		// Deny by default: a route without guard.Public refuses anonymous callers.
		app.Client().Post("/v1/books", map[string]string{"title": "Dune"}).AssertProblem(t, http.StatusUnauthorized, "unauthenticated")
	})
}

func ExampleApp_As() {
	test(func(t *testing.T) {
		app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
		app.As(gorbitaltest.User("usr_1")).Post("/v1/books", map[string]string{"title": "Dune"}).AssertProblem(t, http.StatusForbidden, "forbidden")
	})
}

func ExampleUser() {
	p := gorbitaltest.User("usr_1", "books.book.write")
	fmt.Println(p.UserID, p.Permissions, p.SessionID != "", p.APIKeyID == "")
	// Output: usr_1 [books.book.write] true true
}

func ExampleAPIKey() {
	p := gorbitaltest.APIKey("usr_1", "books.book.read")
	fmt.Println(p.UserID, p.Scopes, p.SessionID == "", p.APIKeyID != "")
	// Output: usr_1 [books.book.read] true true
}

func ExampleClient() {
	test(func(t *testing.T) {
		app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
		writer := app.As(gorbitaltest.User("usr_1", "books.book.write"))
		writer.Post("/v1/books", map[string]string{"title": "Dune"}).AssertStatus(t, http.StatusCreated)
	})
}

func ExampleClient_WithHeader() {
	test(func(t *testing.T) {
		app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
		writer := app.As(gorbitaltest.User("usr_1", "books.book.write")).WithHeader("Idempotency-Key", "7f1e9c4a")
		writer.Post("/v1/books", map[string]string{"title": "Dune"}).AssertStatus(t, http.StatusCreated)
		retry := writer.Post("/v1/books", map[string]string{"title": "Dune"})
		if retry.Header.Get("Idempotent-Replayed") != "true" {
			t.Error("the retry wasn't replayed")
		}
	})
}

func ExampleClient_Get() {
	test(func(t *testing.T) {
		app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
		app.Client().Get("/v1/catalog/bok_1?fields=title").AssertStatus(t, http.StatusOK)
	})
}

func ExampleClient_Post() {
	test(func(t *testing.T) {
		app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
		res := app.As(gorbitaltest.User("usr_1", "books.book.write")).Post("/v1/books", map[string]string{"title": ""})
		res.AssertProblem(t, http.StatusUnprocessableEntity, "validation_failed")
	})
}

func ExampleClient_Put() {
	test(func(t *testing.T) {
		app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
		app.As(gorbitaltest.User("usr_1")).Put("/v1/books/bok_1", map[string]string{"title": "Dune"}).AssertProblem(t, http.StatusNotFound, "not_found")
	})
}

func ExampleClient_Patch() {
	test(func(t *testing.T) {
		app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
		app.As(gorbitaltest.User("usr_1")).Patch("/v1/books/bok_1", map[string]string{"title": "Dune"}).AssertProblem(t, http.StatusNotFound, "not_found")
	})
}

func ExampleClient_Delete() {
	test(func(t *testing.T) {
		app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
		app.As(gorbitaltest.User("usr_1")).Delete("/v1/books/bok_1").AssertProblem(t, http.StatusNotFound, "not_found")
	})
}

func ExampleClient_Do() {
	test(func(t *testing.T) {
		app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
		req := httptest.NewRequest(http.MethodPost, "/v1/books", strings.NewReader(`{"title": "Dune"`))
		req.Header.Set("Content-Type", "application/json")
		app.As(gorbitaltest.User("usr_1", "books.book.write")).Do(req).AssertProblem(t, http.StatusBadRequest, "bad_request")
	})
}

func ExampleResponse() {
	test(func(t *testing.T) {
		app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
		res := app.Client().Get("/v1/catalog/bok_1")
		t.Log(res.Status, res.Header.Get("Content-Type"), string(res.Body))
	})
}

func ExampleResponse_JSON() {
	test(func(t *testing.T) {
		app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
		var book struct{ Title string }
		app.Client().Get("/v1/catalog/bok_1").JSON(t, &book)
		if book.Title != "Dune" {
			t.Errorf("title = %q", book.Title)
		}
	})
}

func ExampleResponse_AssertStatus() {
	test(func(t *testing.T) {
		app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
		app.Client().Get("/readyz").AssertStatus(t, http.StatusOK)
	})
}

func ExampleResponse_AssertProblem() {
	test(func(t *testing.T) {
		app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
		app.Client().Get("/v1/shelves").AssertProblem(t, http.StatusNotFound, "not_found")
	})
}

func ExampleJob() {
	test(func(t *testing.T) {
		app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
		for _, j := range app.Jobs(t, "") {
			var args map[string]any
			if err := json.Unmarshal(j.Args, &args); err != nil {
				t.Fatal(err)
			}
			t.Log(j.Kind, args)
		}
	})
}

func ExampleApp_Jobs() {
	test(func(t *testing.T) {
		app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
		app.As(gorbitaltest.User("usr_1", "books.book.write")).Post("/v1/books", map[string]string{"title": "Dune"}).AssertStatus(t, http.StatusCreated)
		if n := len(app.Jobs(t, "gorbital.mail.send")); n != 1 {
			t.Errorf("queued emails = %d, want 1", n)
		}
	})
}

func ExampleApp_Mail() {
	test(func(t *testing.T) {
		app := gorbitaltest.New(t, gorbital.WithModules(booksModule()))
		app.As(gorbitaltest.User("usr_1", "books.book.write")).Post("/v1/books", map[string]string{"title": "Dune"}).AssertStatus(t, http.StatusCreated)
		sent := app.Mail(t)
		if len(sent) != 1 || sent[0].Subject != "Added Dune" || sent[0].To[0].Email != "reader@example.com" {
			t.Errorf("mail = %+v", sent)
		}
	})
}
