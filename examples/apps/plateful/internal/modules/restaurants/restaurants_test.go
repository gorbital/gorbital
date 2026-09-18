package restaurants_test

import (
	"net/http"
	"testing"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/gorbitaltest"

	"example.com/plateful/db/migrations"
	authhttp "example.com/plateful/internal/modules/auth"
	orgshttp "example.com/plateful/internal/modules/orgs"
	"example.com/plateful/internal/modules/restaurants"
	"example.com/plateful/internal/modules/restaurants/usecase"
)

// These tests drive the restaurants routes through the app's real
// middleware stack, on a new database per test (gorbitaltest), with real
// accounts: a restaurant's staff are members of its organisation, and a
// customer is an account that belongs to none.

// newApp builds the app with sign-in, organisations and the restaurants
// module for the test.
func newApp(t *testing.T) *gorbitaltest.App {
	t.Helper()
	auth := authhttp.New()
	return gorbitaltest.New(t,
		gorbital.WithAuth(auth),
		gorbital.WithModules(orgshttp.Module(auth), restaurants.Module()),
		gorbital.WithMigrations(migrations.FS),
	)
}

// restaurateur signs an account up and gives it an organisation of its own,
// which is what a restaurant is on Plateful.
func restaurateur(t *testing.T, app *gorbitaltest.App, email, name string) (*gorbitaltest.Client, string) {
	t.Helper()
	client, _ := app.SignUp(t, email)
	res := client.Post("/v1/orgs", map[string]string{"name": name})
	res.AssertStatus(t, http.StatusCreated)
	var org struct {
		ID string `json:"id"`
	}
	res.JSON(t, &org)
	return client, org.ID
}

// profile is a restaurant's profile as the API returns it.
type profile struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Status          string `json:"status"`
	DeliveryRadiusM int    `json:"delivery_radius_m"`
	SuspendedReason string `json:"suspended_reason"`
	Version         int64  `json:"version"`
}

// body is a valid profile for name.
func body(version int64, name, status string) map[string]any {
	return map[string]any{
		"version": version, "name": name, "address": "12 Market Street, Leeds",
		"cuisine": "Neapolitan", "opens_minute": 660, "closes_minute": 1320,
		"delivery_radius_m": 3000, "status": status,
	}
}

// publish creates an open restaurant for the organisation and returns it.
func publish(t *testing.T, client *gorbitaltest.Client, orgID, name string) profile {
	t.Helper()
	path := "/v1/orgs/" + orgID + "/restaurant"
	var created profile
	if got := client.Get(path); got.Status == http.StatusOK {
		got.JSON(t, &created)
	} else {
		client.Put(path, body(0, name, "onboarding")).AssertStatus(t, http.StatusOK)
		client.Get(path).JSON(t, &created)
	}
	res := client.Put(path, body(created.Version, name, "open"))
	res.AssertStatus(t, http.StatusOK)
	var open profile
	res.JSON(t, &open)
	return open
}

func TestSaveAndPublish(t *testing.T) {
	app := newApp(t)
	bruno, org := restaurateur(t, app, "bruno@example.com", "Trattoria Bruno")
	path := "/v1/orgs/" + org + "/restaurant"

	// There is no profile until the restaurateur writes one: gorbital has no
	// "an organisation was created" hook, so nothing can create it for them.
	bruno.Get(path).AssertProblem(t, http.StatusNotFound, "restaurant_not_found")

	res := bruno.Put(path, body(0, "Trattoria Bruno", "onboarding"))
	res.AssertStatus(t, http.StatusOK)
	var created profile
	res.JSON(t, &created)
	if created.Status != "onboarding" || created.Version != 1 {
		t.Fatalf("created = %+v, want onboarding version 1", created)
	}

	// Creating it twice is a conflict, not a second restaurant.
	bruno.Put(path, body(0, "Trattoria Bruno", "onboarding")).
		AssertProblem(t, http.StatusConflict, "restaurant_version_conflict")

	opened := publish(t, bruno, org, "Trattoria Bruno")
	if opened.Status != "open" {
		t.Errorf("published = %+v, want open", opened)
	}
}

func TestProfileRules(t *testing.T) {
	app := newApp(t)
	bruno, org := restaurateur(t, app, "bruno@example.com", "Trattoria Bruno")
	path := "/v1/orgs/" + org + "/restaurant"
	created := publish(t, bruno, org, "Trattoria Bruno")

	// docs:start test-radius-setting
	// The delivery radius is capped by a runtime setting, so the limit moves
	// in /ops/settings rather than in a deploy.
	over := body(created.Version, "Trattoria Bruno", "open")
	over["delivery_radius_m"] = 40_000
	bruno.Put(path, over).AssertProblem(t, http.StatusUnprocessableEntity, "validation_failed")
	// docs:end test-radius-setting

	// A restaurant's own staff can't suspend themselves — or unsuspend.
	suspend := body(created.Version, "Trattoria Bruno", "open")
	suspend["status"] = "suspended"
	bruno.Put(path, suspend).AssertProblem(t, http.StatusUnprocessableEntity, "validation_failed")

	// A name another restaurant already uses.
	sara, sarasOrg := restaurateur(t, app, "sara@example.com", "Sara's")
	sara.Put("/v1/orgs/"+sarasOrg+"/restaurant", body(0, "Trattoria Bruno", "onboarding")).
		AssertProblem(t, http.StatusConflict, "restaurant_name_taken")
}

// docs:start test-three-callers

// TestThreeKindsOfCaller: one table, three ways in. A restaurant's staff
// reach their own profile through their organisation; a customer who
// belongs to no organisation browses what is open; platform staff see
// everything and suspend.
func TestThreeKindsOfCaller(t *testing.T) {
	app := newApp(t)
	bruno, brunosOrg := restaurateur(t, app, "bruno@example.com", "Trattoria Bruno")
	open := publish(t, bruno, brunosOrg, "Trattoria Bruno")
	sara, sarasOrg := restaurateur(t, app, "sara@example.com", "Sara's")
	sara.Put("/v1/orgs/"+sarasOrg+"/restaurant", body(0, "Sara's Kitchen", "onboarding")).AssertStatus(t, http.StatusOK)

	// Staff: their own organisation only. Another restaurant's is 404
	// org_not_found, as if it didn't exist.
	sara.Get("/v1/orgs/"+brunosOrg+"/restaurant").AssertProblem(t, http.StatusNotFound, "org_not_found")

	// A customer: no organisation at all, and no way to use one. They see
	// the restaurants that are open, and not the one still being set up.
	diner, _ := app.SignUp(t, "diner@example.com")
	var page struct {
		Items []profile `json:"items"`
	}
	diner.Get("/v1/restaurants").JSON(t, &page)
	if len(page.Items) != 1 || page.Items[0].Name != "Trattoria Bruno" {
		t.Fatalf("a customer's list = %+v, want the open restaurant only", page.Items)
	}
	diner.Get("/v1/restaurants/"+open.ID).AssertStatus(t, http.StatusOK)
	diner.Get("/v1/platform/restaurants").AssertProblem(t, http.StatusForbidden, "forbidden")
	app.Client().Get("/v1/restaurants").AssertProblem(t, http.StatusUnauthorized, "unauthenticated")

	// Platform staff: every restaurant, whatever its status.
	staff := app.As(gorbitaltest.User("usr_staff", usecase.PermOversee, usecase.PermSuspend))
	staff.Get("/v1/platform/restaurants").JSON(t, &page)
	if len(page.Items) != 2 {
		t.Errorf("the platform's list = %+v, want both restaurants", page.Items)
	}
}

// docs:end test-three-callers

// docs:start test-suspension

// TestSuspension: suspending is the platform's, and it stops the restaurant
// at once — a customer stops seeing it, and its own staff can't edit their
// way out.
func TestSuspension(t *testing.T) {
	app := newApp(t)
	bruno, org := restaurateur(t, app, "bruno@example.com", "Trattoria Bruno")
	open := publish(t, bruno, org, "Trattoria Bruno")
	diner, _ := app.SignUp(t, "diner@example.com")
	staff := app.As(gorbitaltest.User("usr_staff", usecase.PermOversee, usecase.PermSuspend))

	// A restaurateur can't reach the platform's routes at all.
	bruno.Post("/v1/platform/restaurants/"+open.ID+"/suspend", map[string]string{"reason": "no"}).
		AssertProblem(t, http.StatusForbidden, "forbidden")

	res := staff.Post("/v1/platform/restaurants/"+open.ID+"/suspend", map[string]string{"reason": "selling food it doesn't have"})
	res.AssertStatus(t, http.StatusOK)
	var suspended profile
	res.JSON(t, &suspended)
	if suspended.Status != "suspended" || suspended.SuspendedReason == "" {
		t.Fatalf("suspended = %+v, want the status and the reason", suspended)
	}

	diner.Get("/v1/restaurants/"+open.ID).AssertProblem(t, http.StatusNotFound, "restaurant_not_found")
	bruno.Put("/v1/orgs/"+org+"/restaurant", body(suspended.Version, "Trattoria Bruno", "open")).
		AssertProblem(t, http.StatusConflict, "restaurant_suspended")

	// Its own staff still see why, which a customer never does.
	var own profile
	bruno.Get("/v1/orgs/"+org+"/restaurant").JSON(t, &own)
	if own.SuspendedReason == "" {
		t.Error("the restaurant's own staff can't see why it was suspended")
	}

	// Lifting brings it back paused, not open.
	res = staff.Post("/v1/platform/restaurants/"+open.ID+"/unsuspend", nil)
	res.AssertStatus(t, http.StatusOK)
	var lifted profile
	res.JSON(t, &lifted)
	if lifted.Status != "paused" {
		t.Errorf("lifted = %+v, want paused", lifted)
	}
	staff.Post("/v1/platform/restaurants/"+open.ID+"/unsuspend", nil).
		AssertProblem(t, http.StatusConflict, "restaurant_not_suspended")
}

// docs:end test-suspension
