package main

import (
	"net/http"
	"regexp"
	"testing"

	"gorbital.dev/gorbital/gorbitaltest"
)

// sixDigits finds a verification code in an email.
var sixDigits = regexp.MustCompile(`\b(\d{6})\b`)

// These tests run the app as main.go builds it, through its real middleware
// stack, on a new database per test (gorbitaltest). The modules' own tests
// are next to them in internal/modules; sign-in, organisations, /ops, flags
// and email events are gorbital's, tested in the library.

// newApp builds the app with main.go's options.
func newApp(t *testing.T) *gorbitaltest.App {
	t.Helper()
	return gorbitaltest.New(t, options()...)
}

// docs:start test-app

// TestApp: the platform answers, everything but the reviews list needs a
// signed-in caller, and the three kinds of caller reach three different
// parts of it.
func TestApp(t *testing.T) {
	app := newApp(t)
	anonymous := app.Client()

	for _, path := range []string{"/livez", "/readyz", "/version", "/openapi.json"} {
		anonymous.Get(path).AssertStatus(t, http.StatusOK)
	}
	// A diner choosing where to eat hasn't signed in yet, so a restaurant's
	// reviews are the one thing the platform answers to nobody in
	// particular. Everything else is deny by default.
	anonymous.Get("/v1/restaurants/rst_unknown/reviews").AssertStatus(t, http.StatusOK)
	for _, path := range []string{"/v1/restaurants", "/v1/orders", "/v1/orgs", "/ops/settings"} {
		anonymous.Get(path).AssertProblem(t, http.StatusUnauthorized, "unauthenticated")
	}

	// Registration takes Plateful's own fields, and saves them with the
	// account (cmd/api/signin.go).
	anonymous.Post("/v1/auth/register", map[string]string{
		"email": "diner@example.com", "password": gorbitaltest.SignUpPassword,
	}).AssertProblem(t, http.StatusUnprocessableEntity, "validation_failed")

	// A customer: signed in, a member of no organisation, and able to browse.
	customer, _ := signUp(t, app, "diner@example.com")
	customer.Get("/v1/restaurants").AssertStatus(t, http.StatusOK)
	customer.Get("/v1/orders").AssertStatus(t, http.StatusOK)
	// And no more than that: /ops is for the platform's operators.
	customer.Get("/ops/settings").AssertProblem(t, http.StatusForbidden, "forbidden")
	customer.Get("/v1/platform/restaurants").AssertProblem(t, http.StatusForbidden, "forbidden")

	// A restaurateur: an organisation of their own, and a restaurant in it.
	restaurateur, _ := signUp(t, app, "bruno@example.com")
	res := restaurateur.Post("/v1/orgs", map[string]string{"name": "Trattoria Bruno"})
	res.AssertStatus(t, http.StatusCreated)
	var org struct {
		ID string `json:"id"`
	}
	res.JSON(t, &org)
	restaurateur.Put("/v1/orgs/"+org.ID+"/restaurant", map[string]any{
		"version": 0, "name": "Trattoria Bruno", "address": "12 Market Street, Leeds",
		"opens_minute": 0, "closes_minute": 0, "delivery_radius_m": 3000, "status": "open",
	}).AssertStatus(t, http.StatusOK)
	// The customer can't see into it, and the restaurateur's own
	// organisation is the only one they can.
	customer.Get("/v1/orgs/"+org.ID+"/orders").AssertProblem(t, http.StatusNotFound, "org_not_found")

	// The customer app's feature flags: the client ones, and only those.
	var flags struct {
		Flags map[string]bool `json:"flags"`
	}
	customer.Get("/v1/flags").JSON(t, &flags)
	if _, ok := flags.Flags["orders.scheduled_ordering"]; !ok {
		t.Errorf("GET /v1/flags = %v, want the client flag orders.scheduled_ordering", flags.Flags)
	}
	if _, ok := flags.Flags["orders.courier_auto_assign"]; ok {
		t.Error("a server-side flag is in GET /v1/flags")
	}
}

// docs:end test-app

// signUp registers an account with Plateful's registration fields and
// returns a client that carries its token.
func signUp(t *testing.T, app *gorbitaltest.App, email string) (*gorbitaltest.Client, string) {
	t.Helper()
	anon := app.Client()
	anon.Post("/v1/auth/register", map[string]string{
		"email": email, "password": gorbitaltest.SignUpPassword,
		"display_name": "Ada", "address": "3 Kirkgate, Leeds",
	}).AssertStatus(t, http.StatusAccepted)
	code := verificationCode(t, app, email)
	anon.Post("/v1/auth/verify-email", map[string]string{"email": email, "code": code}).
		AssertStatus(t, http.StatusNoContent)
	res := anon.Post("/v1/auth/login", map[string]string{
		"email": email, "password": gorbitaltest.SignUpPassword, "transport": "bearer",
	})
	res.AssertStatus(t, http.StatusOK)
	var login struct {
		Token string `json:"token"`
	}
	res.JSON(t, &login)
	client := app.Client().WithHeader("Authorization", "Bearer "+login.Token)
	var me struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	client.Get("/v1/auth/me").JSON(t, &me)
	return client, me.User.ID
}

// verificationCode reads the six digits out of the email the app queued.
// Workers don't run in tests, so nothing was delivered: the message is a
// queued job, which App.Mail reads back.
func verificationCode(t *testing.T, app *gorbitaltest.App, email string) string {
	t.Helper()
	sent := app.Mail(t)
	for i := len(sent) - 1; i >= 0; i-- {
		if len(sent[i].To) == 0 || sent[i].To[0].Email != email {
			continue
		}
		if m := sixDigits.FindStringSubmatch(sent[i].Text); m != nil {
			return m[1]
		}
	}
	t.Fatalf("no verification email for %s", email)
	return ""
}
