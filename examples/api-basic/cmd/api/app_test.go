package main

import (
	"net/http"
	"testing"

	"gorbital.dev/gorbital/gorbitaltest"
)

// These tests run the app as main.go builds it, through its real middleware
// stack, on a new database per test (gorbitaltest). The modules' own tests
// are next to them in internal/modules; sign-in, /ops, flags and email
// events are gorbital's, tested in the library.

// newApp builds the app with main.go's options.
func newApp(t *testing.T) *gorbitaltest.App {
	t.Helper()
	return gorbitaltest.New(t, options()...)
}

func TestApp(t *testing.T) {
	app := newApp(t)
	anonymous := app.Client()

	for _, path := range []string{"/livez", "/readyz", "/version", "/openapi.json", "/v1/ping"} {
		anonymous.Get(path).AssertStatus(t, http.StatusOK)
	}
	// Every route requires sign-in unless it is public.
	anonymous.Get("/v1/projects").AssertProblem(t, http.StatusUnauthorized, "unauthenticated")
	anonymous.Get("/ops/settings").AssertProblem(t, http.StatusUnauthorized, "unauthenticated")

	ada, _ := app.SignUp(t, "ada@example.com")
	ada.Post("/v1/projects", map[string]string{"name": "Website"}).AssertStatus(t, http.StatusCreated)
	var projects struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
	}
	ada.Get("/v1/projects").JSON(t, &projects)
	if len(projects.Items) != 1 || projects.Items[0].Name != "Website" {
		t.Errorf("GET /v1/projects = %+v, want the project Ada created", projects)
	}

	// Users hold no platform role: /ops is for operators.
	ada.Get("/ops/settings").AssertProblem(t, http.StatusForbidden, "forbidden")

	// Client feature flags, the example's among them.
	var flags struct {
		Flags map[string]bool `json:"flags"`
	}
	ada.Get("/v1/flags").JSON(t, &flags)
	if on, ok := flags.Flags["example.ping_time"]; !ok || on {
		t.Errorf("GET /v1/flags = %v, want example.ping_time off", flags)
	}
}
