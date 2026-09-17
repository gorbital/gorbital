package books_test

import (
	"net/http"
	"regexp"
	"testing"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/authhttp"
	"gorbital.dev/gorbital/gorbitaltest"

	"example.com/shelfie/db/migrations"
	"example.com/shelfie/internal/modules/books"
)

// docs:start sign-in

// TestSignUpAndAddABook goes through real sign-in, as the web app does:
// register, verify the address with the emailed code, sign in, then add a
// book. The app is built with authhttp, as main.go builds it, instead of
// gorbitaltest's principals.
func TestSignUpAndAddABook(t *testing.T) {
	app := gorbitaltest.New(t,
		gorbital.WithAuth(authhttp.New()),
		gorbital.WithModules(books.Module()),
		gorbital.WithMigrations(migrations.FS),
	)
	reader := app.Client()
	account := map[string]string{"email": "ada@example.com", "password": "a long enough password"}

	reader.Post("/v1/auth/register", account).AssertStatus(t, http.StatusAccepted)
	reader.Post("/v1/auth/login", account).AssertProblem(t, http.StatusForbidden, "email_not_verified")

	// The code is in the verification email, queued for delivery.
	code := emailedCode(t, app, "ada@example.com")
	reader.Post("/v1/auth/verify-email", map[string]string{"email": "ada@example.com", "code": code}).AssertStatus(t, http.StatusNoContent)

	// A mobile app asks for the session token in the response.
	login := reader.Post("/v1/auth/login", map[string]string{"email": "ada@example.com", "password": "a long enough password", "transport": "bearer"})
	login.AssertStatus(t, http.StatusOK)
	var session struct {
		Token string `json:"token"`
	}
	login.JSON(t, &session)
	ada := app.Client().WithHeader("Authorization", "Bearer "+session.Token)

	// Every account holds the user role, which the books permissions name.
	ada.Post("/v1/books", map[string]string{"title": "Dune"}).AssertStatus(t, http.StatusCreated)
	var shelf struct {
		Items []book `json:"items"`
	}
	ada.Get("/v1/books").JSON(t, &shelf)
	if len(shelf.Items) != 1 || shelf.Items[0].Title != "Dune" {
		t.Errorf("GET /v1/books = %+v, want Dune", shelf.Items)
	}
	app.Client().Get("/v1/books").AssertProblem(t, http.StatusUnauthorized, "unauthenticated")
}

var sixDigits = regexp.MustCompile(`\b(\d{6})\b`)

// emailedCode returns the 6-digit code in the newest email queued to to.
func emailedCode(t *testing.T, app *gorbitaltest.App, to string) string {
	t.Helper()
	sent := app.Mail(t)
	for i := len(sent) - 1; i >= 0; i-- {
		if len(sent[i].To) > 0 && sent[i].To[0].Email == to {
			if m := sixDigits.FindStringSubmatch(sent[i].Text); m != nil {
				return m[1]
			}
		}
	}
	t.Fatalf("no email with a code to %s in %d queued", to, len(sent))
	return ""
}

// docs:end sign-in
