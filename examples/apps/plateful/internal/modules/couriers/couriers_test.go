package couriers_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/gorbitaltest"

	"example.com/plateful/db/migrations"
	authhttp "example.com/plateful/internal/modules/auth"
	"example.com/plateful/internal/modules/couriers"
	orgshttp "example.com/plateful/internal/modules/orgs"
)

// These tests drive the couriers routes through the app's real middleware
// stack, on a new database per test (gorbitaltest), with real accounts.
//
// The organisations module is here for one route only: the restaurant's
// dispatch list. Everything a courier does is reached without it, which is
// the module's whole point.

// newApp builds the app with sign-in, organisations and the couriers module
// for the test.
func newApp(t *testing.T) *gorbitaltest.App {
	t.Helper()
	auth := authhttp.New()
	return gorbitaltest.New(t,
		gorbital.WithAuth(auth),
		gorbital.WithModules(orgshttp.Module(auth), couriers.Module()),
		gorbital.WithMigrations(migrations.FS),
	)
}

// signUp creates an account for email and returns its client, its user ID
// and its personal workspace, the organisation every account gets from
// orgshttp.
//
// A courier's "no organisation" is therefore not "no row in org_members" in
// this app: it is that no restaurant's organisation has them, which is what
// these tests check. A courier who is staff of no restaurant still reaches
// every one of their own routes.
func signUp(t *testing.T, app *gorbitaltest.App, email string) (*gorbitaltest.Client, string, string) {
	t.Helper()
	client, userID := app.SignUp(t, email)
	var orgs struct {
		Items []struct {
			ID       string `json:"id"`
			Personal bool   `json:"personal"`
		} `json:"items"`
	}
	client.Get("/v1/orgs").JSON(t, &orgs)
	for _, org := range orgs.Items {
		if org.Personal {
			return client, userID, org.ID
		}
	}
	t.Fatalf("%s has no personal workspace", email)
	return nil, "", ""
}

// apiCourier is a courier's own profile as the API returns it.
type apiCourier struct {
	ID            string `json:"id"`
	DisplayName   string `json:"display_name"`
	Vehicle       string `json:"vehicle"`
	Available     bool   `json:"available"`
	ActiveOrderID string `json:"active_order_id"`
	Version       int64  `json:"version"`
}

// register signs up a courier and returns their client, their account and
// their profile.
func register(t *testing.T, app *gorbitaltest.App, email, name, vehicle string) (*gorbitaltest.Client, string, apiCourier) {
	t.Helper()
	client, userID, _ := signUp(t, app, email)
	res := client.Post("/v1/couriers", map[string]any{"display_name": name, "vehicle": vehicle})
	res.AssertStatus(t, http.StatusCreated)
	var c apiCourier
	res.JSON(t, &c)
	return client, userID, c
}

// dispatchList is the path a restaurant's staff read available couriers on.
func dispatchList(orgID string) string { return "/v1/orgs/" + orgID + "/couriers" }

func TestRegisterCourier(t *testing.T) {
	app := newApp(t)
	sam, _, sams := register(t, app, "sam@example.com", "  Sam  ", "bicycle")

	if !strings.HasPrefix(sams.ID, "cur_") || sams.DisplayName != "Sam" || sams.Vehicle != "bicycle" || sams.Available || sams.Version != 1 {
		t.Errorf("registered = %+v, want a cur_ ID, the name trimmed, off duty and version 1", sams)
	}

	var got apiCourier
	sam.Get("/v1/couriers/me").JSON(t, &got)
	if got != sams {
		t.Errorf("GET /v1/couriers/me = %+v, want %+v", got, sams)
	}

	// One profile per account: the second attempt is a conflict, not a
	// second identity. The unique index on user_id is what decides it.
	sam.Post("/v1/couriers", map[string]any{"display_name": "Sam again", "vehicle": "car"}).
		AssertProblem(t, http.StatusConflict, "courier_already_registered")

	sam.Post("/v1/couriers", map[string]any{"display_name": "", "vehicle": "bicycle"}).
		AssertProblem(t, http.StatusUnprocessableEntity, "validation_failed")
	sam.Post("/v1/couriers", map[string]any{"display_name": "Sam", "vehicle": "hovercraft"}).
		AssertProblem(t, http.StatusUnprocessableEntity, "validation_failed")

	app.Client().Get("/v1/couriers/me").AssertProblem(t, http.StatusUnauthorized, "unauthenticated")
}

// TestACourierInNoRestaurantReachesTheirOwnRoutes is the point of this
// module. Sam is staff of no restaurant, so guard.OrgMember would refuse
// them everywhere and there is no organisation to put in a path; their own
// routes are guarded by a platform permission and the use case's ownership
// check instead, and they work.
func TestACourierInNoRestaurantReachesTheirOwnRoutes(t *testing.T) {
	app := newApp(t)
	sam, _, sams := register(t, app, "sam@example.com", "Sam", "scooter")

	// Ada runs a restaurant. Sam is not a member of it, and never will be.
	_, _, adaOrg := signUp(t, app, "ada@example.com")
	sam.Get(dispatchList(adaOrg)).AssertProblem(t, http.StatusNotFound, "org_not_found")

	// The same account, on its own routes, is fine.
	sam.Get("/v1/couriers/me").AssertStatus(t, http.StatusOK)
	res := sam.Patch("/v1/couriers/me", map[string]any{"version": sams.Version, "available": true})
	res.AssertStatus(t, http.StatusOK)
	var updated apiCourier
	res.JSON(t, &updated)
	if !updated.Available || updated.Version != 2 {
		t.Errorf("after going on duty = %+v, want available and version 2", updated)
	}
}

// TestACourierCannotReadAnotherProfile checks the ownership check that
// stands in for the guard. /v1/couriers/me takes no ID, so the only way to
// ask for somebody else's profile is to be somebody else; an account without
// one gets the same 404 whether or not anybody else has registered.
func TestACourierCannotReadAnotherProfile(t *testing.T) {
	app := newApp(t)
	_, _, sams := register(t, app, "sam@example.com", "Sam", "bicycle")
	kit, _, _ := signUp(t, app, "kit@example.com")

	kit.Get("/v1/couriers/me").AssertProblem(t, http.StatusNotFound, "courier_not_found")
	// Kit knows Sam's ID and version and still can't touch the row.
	kit.Patch("/v1/couriers/me", map[string]any{"version": sams.Version, "display_name": "Kit"}).
		AssertProblem(t, http.StatusNotFound, "courier_not_found")
}

func TestUpdateCourierChecksTheVersion(t *testing.T) {
	app := newApp(t)
	sam, _, sams := register(t, app, "sam@example.com", "Sam", "bicycle")

	sam.Patch("/v1/couriers/me", map[string]any{"version": sams.Version + 1, "vehicle": "car"}).
		AssertProblem(t, http.StatusConflict, "courier_version_conflict")
	sam.Patch("/v1/couriers/me", map[string]any{"version": sams.Version, "vehicle": "car"}).
		AssertStatus(t, http.StatusOK)
	// The version it already had is now stale.
	sam.Patch("/v1/couriers/me", map[string]any{"version": sams.Version, "vehicle": "scooter"}).
		AssertProblem(t, http.StatusConflict, "courier_version_conflict")
	sam.Patch("/v1/couriers/me", map[string]any{"version": 2, "vehicle": "tractor"}).
		AssertProblem(t, http.StatusUnprocessableEntity, "validation_failed")
}

// TestACourierCannotGoOffDutyMidDelivery checks the one rule the orders
// module and this one share.
func TestACourierCannotGoOffDutyMidDelivery(t *testing.T) {
	app := newApp(t)
	sam, _, sams := register(t, app, "sam@example.com", "Sam", "scooter")
	sam.Patch("/v1/couriers/me", map[string]any{"version": sams.Version, "available": true}).AssertStatus(t, http.StatusOK)

	// Put an order on Sam. In the real flow the orders module writes
	// active_order_id inside its own transaction, with this row locked FOR
	// UPDATE alongside the order it is assigning; this test only needs the
	// state that leaves behind, and the orders module isn't built into this
	// app.
	db := app.App().Deps().DB
	if _, err := db.Exec(context.Background(),
		`UPDATE couriers SET active_order_id = $2 WHERE id = $1`, sams.ID, "ord_carried"); err != nil {
		t.Fatal(err)
	}

	var carrying apiCourier
	sam.Get("/v1/couriers/me").JSON(t, &carrying)
	if carrying.ActiveOrderID != "ord_carried" {
		t.Fatalf("active_order_id = %q, want the order the courier is carrying", carrying.ActiveOrderID)
	}

	sam.Patch("/v1/couriers/me", map[string]any{"version": carrying.Version, "available": false}).
		AssertProblem(t, http.StatusConflict, "courier_on_delivery")
	// Everything else about the profile still changes mid-delivery.
	sam.Patch("/v1/couriers/me", map[string]any{"version": carrying.Version, "display_name": "Sam on a scooter"}).
		AssertStatus(t, http.StatusOK)

	// Released by the orders module at the end of the delivery, the courier
	// can go off duty again.
	if _, err := db.Exec(context.Background(), `UPDATE couriers SET active_order_id = '' WHERE id = $1`, sams.ID); err != nil {
		t.Fatal(err)
	}
	var free apiCourier
	sam.Get("/v1/couriers/me").JSON(t, &free)
	sam.Patch("/v1/couriers/me", map[string]any{"version": free.Version, "available": false}).AssertStatus(t, http.StatusOK)
}

// TestRestaurantStaffListAvailableCouriers checks the cross-tenant read: the
// couriers it returns belong to no organisation, so two unrelated
// restaurants see the same people, and the response carries nothing about
// the courier's account.
func TestRestaurantStaffListAvailableCouriers(t *testing.T) {
	app := newApp(t)
	ada, _, adaOrg := signUp(t, app, "ada@example.com")
	bob, _, bobOrg := signUp(t, app, "bob@example.com")

	sam, samID, sams := register(t, app, "sam@example.com", "Sam", "bicycle")
	sam.Patch("/v1/couriers/me", map[string]any{"version": sams.Version, "available": true}).AssertStatus(t, http.StatusOK)
	// Kit registers but stays off duty, so no restaurant may send them out.
	register(t, app, "kit@example.com", "Kit", "car")

	var list struct {
		Items []map[string]any `json:"items"`
	}
	ada.Get(dispatchList(adaOrg)).JSON(t, &list)
	if len(list.Items) != 1 || list.Items[0]["display_name"] != "Sam" || list.Items[0]["vehicle"] != "bicycle" {
		t.Fatalf("available couriers = %+v, want Sam on a bicycle only", list.Items)
	}
	// Never the courier's sign-in account: that is another tenant's
	// business, and not a restaurant's at all.
	for _, field := range []string{"user_id", "active_order_id", "available", "version"} {
		if _, ok := list.Items[0][field]; ok {
			t.Errorf("the dispatch list returns %q; a restaurant sees the ID, the name and the vehicle only", field)
		}
	}
	if id, _ := list.Items[0]["id"].(string); id != sams.ID || id == samID {
		t.Errorf("dispatch list id = %q, want the courier ID %q and never the account", id, sams.ID)
	}

	// Bob's unrelated restaurant sees the same courier: a courier is not a
	// tenant's row, and there is no org_id to filter by.
	var bobsList struct {
		Items []map[string]any `json:"items"`
	}
	bob.Get(dispatchList(bobOrg)).JSON(t, &bobsList)
	if len(bobsList.Items) != 1 || bobsList.Items[0]["id"] != sams.ID {
		t.Errorf("another restaurant's list = %+v, want the same courier", bobsList.Items)
	}

	// A courier carrying an order is out of the list: they are not free.
	db := app.App().Deps().DB
	if _, err := db.Exec(context.Background(),
		`UPDATE couriers SET active_order_id = $2 WHERE id = $1`, sams.ID, "ord_carried"); err != nil {
		t.Fatal(err)
	}
	var busy struct {
		Items []map[string]any `json:"items"`
	}
	ada.Get(dispatchList(adaOrg)).JSON(t, &busy)
	if len(busy.Items) != 0 {
		t.Errorf("available couriers while Sam is carrying an order = %+v, want none", busy.Items)
	}
}

// TestDeletingAnAccountDeletesItsCourierProfile checks the migration's
// foreign key. A courier has no organisation to be purged with, so the only
// thing a courier row hangs off is the sign-in account behind it: delete the
// account and the profile goes too.
func TestDeletingAnAccountDeletesItsCourierProfile(t *testing.T) {
	app := newApp(t)
	_, samID, sams := register(t, app, "sam@example.com", "Sam", "bicycle")

	db := app.App().Deps().DB
	ctx := context.Background()
	if _, err := db.Exec(ctx, `DELETE FROM auth_users WHERE id = $1`, samID); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM couriers WHERE id = $1`, sams.ID).Scan(&left); err != nil || left != 0 {
		t.Errorf("courier profiles left after deleting the account = %d, %v; want 0", left, err)
	}
}

// TestDispatchNeedsMembershipOfTheOrganisationInThePath checks what
// guard.OrgMember does still buy on the one route that has an organisation
// in its path: standing. A signed-in account that is staff of no restaurant
// — a courier, or a diner — gets 404 org_not_found, so the list of who is
// out working tonight isn't readable by everyone with an account.
func TestDispatchNeedsMembershipOfTheOrganisationInThePath(t *testing.T) {
	app := newApp(t)
	_, _, adaOrg := signUp(t, app, "ada@example.com")
	sam, _, _ := register(t, app, "sam@example.com", "Sam", "bicycle")
	kit, _, _ := signUp(t, app, "kit@example.com")

	for name, client := range map[string]*gorbitaltest.Client{"courier": sam, "diner": kit} {
		t.Run("non-member "+name, func(t *testing.T) {
			client.Get(dispatchList(adaOrg)).AssertProblem(t, http.StatusNotFound, "org_not_found")
		})
	}
	app.Client().Get(dispatchList(adaOrg)).AssertProblem(t, http.StatusUnauthorized, "unauthenticated")
}
