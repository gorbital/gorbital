package main

import (
	"context"
	"net/http"
	"regexp"
	"testing"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/authhttp"
	"gorbital.dev/gorbital/gorbitaltest"

	"example.com/shelfie/db/migrations"
	"example.com/shelfie/internal/modules"
)

// These tests are in package main to build sign-in with signInOptions, as
// main.go does.

// docs:start new-accounts-app

// newAccountsApp builds Shelfie's sign-in with its options and every module
// of modules.All.
func newAccountsApp(t *testing.T) *gorbitaltest.App {
	t.Helper()
	return gorbitaltest.New(t,
		gorbital.WithName("shelfie"),
		gorbital.WithAuth(authhttp.New(signInOptions()...)),
		gorbital.WithModules(modules.All()...),
		gorbital.WithMigrations(migrations.FS),
	)
}

// docs:end new-accounts-app

const readerPassword = "a long enough password"

var sixDigits = regexp.MustCompile(`\b(\d{6})\b`)

// verifyAndSignIn verifies email's address with the emailed code and signs
// in with a bearer token.
func verifyAndSignIn(t *testing.T, app *gorbitaltest.App, email string) *gorbitaltest.Client {
	t.Helper()
	var code string
	for _, m := range app.Mail(t) {
		if len(m.To) > 0 && m.To[0].Email == email {
			if found := sixDigits.FindStringSubmatch(m.Text); found != nil {
				code = found[1]
			}
		}
	}
	app.Client().Post("/v1/auth/verify-email", map[string]string{"email": email, "code": code}).AssertStatus(t, http.StatusNoContent)
	res := app.Client().Post("/v1/auth/login", map[string]string{"email": email, "password": readerPassword, "transport": "bearer"})
	res.AssertStatus(t, http.StatusOK)
	var session struct {
		Token string `json:"token"`
		User  struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	res.JSON(t, &session)
	return app.Client().WithHeader("Authorization", "Bearer "+session.Token)
}

// docs:start register-with-profile

func TestRegisterWithAProfile(t *testing.T) {
	app := newAccountsApp(t)
	visitor := app.Client()

	// The fields are checked before any account exists.
	visitor.Post("/v1/auth/register", map[string]string{"email": "ada@example.com", "password": readerPassword}).
		AssertProblem(t, http.StatusUnprocessableEntity, "validation_failed") // display_name is required
	visitor.Post("/v1/auth/register", map[string]string{"email": "ada@example.com", "password": readerPassword, "display_name": "   "}).
		AssertProblem(t, http.StatusUnprocessableEntity, "validation_failed") // RegistrationFields.Resolve
	visitor.Post("/v1/auth/register", map[string]string{"email": "ada@example.com", "password": "13 characters", "display_name": "Ada"}).
		AssertProblem(t, http.StatusUnprocessableEntity, "weak_password") // MinPasswordLength(14)

	visitor.Post("/v1/auth/register", map[string]string{
		"email": "ada@example.com", "password": readerPassword, "display_name": "Ada", "country": "GB",
	}).AssertStatus(t, http.StatusAccepted)
	ada := verifyAndSignIn(t, app, "ada@example.com")

	var profile struct {
		DisplayName string `json:"display_name"`
		Country     string `json:"country"`
	}
	ada.Get("/v1/profile").JSON(t, &profile)
	if profile.DisplayName != "Ada" || profile.Country != "GB" {
		t.Errorf("GET /v1/profile = %+v", profile)
	}
	// OnRegister created a private "Reading" shelf, which the shelves
	// module serves like any other.
	var shelves struct {
		Items []struct {
			Name       string `json:"name"`
			Visibility string `json:"visibility"`
			Version    int64  `json:"version"`
		} `json:"items"`
	}
	ada.Get("/v1/shelves").JSON(t, &shelves)
	if len(shelves.Items) != 1 || shelves.Items[0].Name != "Reading" || shelves.Items[0].Visibility != "private" || shelves.Items[0].Version != 1 {
		t.Errorf("GET /v1/shelves = %+v, want the default shelf", shelves)
	}
	ada.Post("/v1/shelves", map[string]string{"name": "reading"}).AssertProblem(t, http.StatusConflict, "shelf_name_taken")
}

// docs:end register-with-profile

// docs:start complete-profile

// TestCompleteYourProfile: a reader without a profile, as after a first
// Google, Apple or GitHub sign-in, which doesn't send registration fields,
// creates it with PUT /v1/profile.
func TestCompleteYourProfile(t *testing.T) {
	app := newAccountsApp(t)
	app.Client().Post("/v1/auth/register", map[string]string{"email": "bob@example.com", "password": readerPassword, "display_name": "Bob"}).
		AssertStatus(t, http.StatusAccepted)
	bob := verifyAndSignIn(t, app, "bob@example.com")
	if _, err := app.App().Deps().DB.Exec(context.Background(), `DELETE FROM profiles`); err != nil {
		t.Fatal(err)
	}

	bob.Get("/v1/profile").AssertProblem(t, http.StatusNotFound, "profile_incomplete")
	bob.Put("/v1/profile", map[string]string{"display_name": "Bob", "country": "FR"}).AssertStatus(t, http.StatusOK)
	bob.Get("/v1/profile").AssertStatus(t, http.StatusOK)
}

// docs:end complete-profile

// docs:start suspended

func TestSuspendedReadersCantSignIn(t *testing.T) {
	app := newAccountsApp(t)
	app.Client().Post("/v1/auth/register", map[string]string{"email": "eve@example.com", "password": readerPassword, "display_name": "Eve"}).
		AssertStatus(t, http.StatusAccepted)
	verifyAndSignIn(t, app, "eve@example.com")

	// A moderator suspends Eve.
	if _, err := app.App().Deps().DB.Exec(context.Background(), `UPDATE profiles SET suspended_at = now()`); err != nil {
		t.Fatal(err)
	}
	app.Client().Post("/v1/auth/login", map[string]string{"email": "eve@example.com", "password": readerPassword}).
		AssertProblem(t, http.StatusForbidden, "reader_suspended")

	// A wrong password still gets the usual answer: the hook only runs once
	// the password is right, so it never tells anyone else she's suspended.
	app.Client().Post("/v1/auth/login", map[string]string{"email": "eve@example.com", "password": "not her password at all"}).
		AssertProblem(t, http.StatusUnauthorized, "invalid_credentials")
}

// docs:end suspended
