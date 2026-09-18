package main

import (
	"net/http"
	"testing"

	"gorbital.dev/gorbital/gorbitaltest"
)

// These tests run the app as main.go builds it, through its real middleware
// stack, on a new database per test (gorbitaltest). The modules' own tests
// are next to them in internal/modules; sign-in, organisations, /ops, flags
// and email events are gorbital's, tested in the library.

// newApp builds the app with main.go's options.
func newApp(t *testing.T) *gorbitaltest.App {
	t.Helper()
	return gorbitaltest.New(t, options()...)
}

func TestApp(t *testing.T) {
	app := newApp(t)
	anonymous := app.Client()

	for _, path := range []string{"/livez", "/readyz", "/version", "/openapi.json"} {
		anonymous.Get(path).AssertStatus(t, http.StatusOK)
	}
	// Every route requires sign-in unless it is public.
	anonymous.Get("/v1/orgs").AssertProblem(t, http.StatusUnauthorized, "unauthenticated")
	anonymous.Get("/ops/settings").AssertProblem(t, http.StatusUnauthorized, "unauthenticated")

	// Signing in works, and /ops is for operators only.
	ada, _ := app.SignUp(t, "ada@example.com")
	ada.Get("/ops/settings").AssertProblem(t, http.StatusForbidden, "forbidden")
}

// personalWorkspace returns the ID of the client's personal workspace.
func personalWorkspace(t *testing.T, client *gorbitaltest.Client) string {
	t.Helper()
	var orgs struct {
		Items []struct {
			ID       string `json:"id"`
			Personal bool   `json:"personal"`
		} `json:"items"`
	}
	client.Get("/v1/orgs").JSON(t, &orgs)
	for _, org := range orgs.Items {
		if org.Personal {
			return org.ID
		}
	}
	t.Fatal("no personal workspace")
	return ""
}
