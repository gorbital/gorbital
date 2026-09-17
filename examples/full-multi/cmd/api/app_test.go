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

	for _, path := range []string{"/livez", "/readyz", "/version", "/openapi.json", "/v1/ping"} {
		anonymous.Get(path).AssertStatus(t, http.StatusOK)
	}
	// Every route requires sign-in unless it is public.
	anonymous.Get("/v1/orgs").AssertProblem(t, http.StatusUnauthorized, "unauthenticated")
	anonymous.Get("/ops/settings").AssertProblem(t, http.StatusUnauthorized, "unauthenticated")

	// Every account gets a personal workspace, where its projects go.
	ada, _ := app.SignUp(t, "ada@example.com")
	workspace := personalWorkspace(t, ada)
	projects := "/v1/orgs/" + workspace + "/projects"
	ada.Post(projects, map[string]string{"name": "Website"}).AssertStatus(t, http.StatusCreated)
	var page struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
	}
	ada.Get(projects).JSON(t, &page)
	if len(page.Items) != 1 || page.Items[0].Name != "Website" {
		t.Errorf("GET %s = %+v, want the project Ada created", projects, page)
	}

	// Another account can't tell the organisation exists.
	sam, _ := app.SignUp(t, "sam@example.com")
	sam.Get(projects).AssertProblem(t, http.StatusNotFound, "org_not_found")

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
