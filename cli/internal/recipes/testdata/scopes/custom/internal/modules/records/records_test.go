package records_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/gorbitaltest"

	"example.com/app/db/migrations"
	"example.com/app/internal/modules/records"
	"example.com/app/internal/modules/records/domain"
	"example.com/app/internal/modules/records/usecase"
)

// These tests drive the records routes through the app's real middleware
// stack, on a new database per test (gorbitaltest).
//
// What they can check is the floor under the module's own rule: that
// nothing reaches a handler without signing in and holding the
// permission, and that an unwritten policy refuses rather than serves.
// Who may see and change which record is policy.go's decision, so the
// tests for it are yours to write — see policy_test.go.

// newApp builds the app with the records module for the test.
func newApp(t *testing.T) *gorbitaltest.App {
	t.Helper()
	return gorbitaltest.New(t,
		gorbital.WithModules(records.Module()),
		gorbital.WithMigrations(migrations.FS),
	)
}

// apiRecord is a record as the API returns it.
type apiRecord struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Note    string `json:"note"`
	State   string `json:"state"`
	Version int64  `json:"version"`
}

const collection = "/v1/records"

// policyWritten reports whether policy.go still returns
// gorbital.ErrNotImplemented. While it does, the module answers 501 to
// everything and the tests below that need a working rule skip: the
// failure that matters then is policy_test.go's, and one red test is
// enough to stop a build.
func policyWritten() bool {
	var none domain.Record
	p := records.Policy{}
	return !errors.Is(p.CanRead(context.Background(), none), gorbital.ErrNotImplemented) &&
		!errors.Is(p.CanWrite(context.Background(), none), gorbital.ErrNotImplemented)
}

// TestRecordsNeedSigningIn checks what holds whatever the policy says: no
// route serves a caller who isn't signed in or doesn't hold the
// permission. The policy is asked after these, never instead of them.
func TestRecordsNeedSigningIn(t *testing.T) {
	app := newApp(t)
	item := collection + "/rcr_mfrggzdfmztwq2lkmfrggzdfmy"
	stranger := app.Client()

	for name, res := range map[string]*gorbitaltest.Response{
		"list":   stranger.Get(collection),
		"get":    stranger.Get(item),
		"create": stranger.Post(collection, map[string]any{"title": "Website", "note": "Example note"}),
		"update": stranger.Patch(item, map[string]any{"version": 1, "title": "Mine now"}),
		"delete": stranger.Delete(item),
	} {
		t.Run("signed out "+name, func(t *testing.T) {
			res.AssertProblem(t, http.StatusUnauthorized, "unauthenticated")
		})
	}

	reader := app.As(gorbitaltest.User("usr_bob", usecase.PermRead))
	reader.Post(collection, map[string]any{"title": "By a reader", "note": "Example note"}).AssertProblem(t, http.StatusForbidden, "forbidden")
	reader.Patch(item, map[string]any{"version": 1, "title": "By a reader"}).AssertProblem(t, http.StatusForbidden, "forbidden")
	reader.Delete(item).AssertProblem(t, http.StatusForbidden, "forbidden")
}

// TestUnwrittenPolicyRefusesEveryRequest checks that a module whose rule
// nobody has written yet serves nothing: 501 not_implemented, not a row.
// Once policy.go is written this test skips, and the tests of your own
// rule take over.
func TestUnwrittenPolicyRefusesEveryRequest(t *testing.T) {
	if policyWritten() {
		t.Skip("policy.go is written: replace this test with the cases your rule names")
	}
	app := newApp(t)
	ada := app.As(gorbitaltest.User("usr_ada", usecase.PermRead, usecase.PermWrite))
	item := collection + "/rcr_mfrggzdfmztwq2lkmfrggzdfmy"

	for name, res := range map[string]*gorbitaltest.Response{
		"list":   ada.Get(collection),
		"get":    ada.Get(item),
		"create": ada.Post(collection, map[string]any{"title": "Website", "note": "Example note"}),
		"update": ada.Patch(item, map[string]any{"version": 1, "title": "Mine now"}),
		"delete": ada.Delete(item),
	} {
		t.Run(name, func(t *testing.T) {
			res.AssertProblem(t, http.StatusNotImplemented, "not_implemented")
		})
	}
}

// TestCreateAndGetRecord is the round trip, once the policy lets the
// caller through. Adjust the actor to one your rule allows.
func TestCreateAndGetRecord(t *testing.T) {
	if !policyWritten() {
		t.Skip("policy.go isn't written yet (policy_test.go says so)")
	}
	app := newApp(t)
	ada := app.As(gorbitaltest.User("usr_ada", usecase.PermRead, usecase.PermWrite))

	res := ada.Post(collection, map[string]any{"title": "Website", "note": "Example note"})
	res.AssertStatus(t, http.StatusCreated)
	var created apiRecord
	res.JSON(t, &created)

	var got apiRecord
	ada.Get(collection+"/"+created.ID).JSON(t, &got)
	if got != created {
		t.Errorf("GET = %+v, want %+v", got, created)
	}

	rows, err := app.App().Deps().DB.Query(context.Background(),
		`SELECT action FROM audit_events WHERE resource_type = 'record' AND resource_id = $1`, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Errorf("creating a record recorded no audit event")
	}
}
