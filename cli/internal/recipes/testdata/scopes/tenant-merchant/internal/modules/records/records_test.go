package records_test

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"gorbital.dev/actor"
	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/gorbitaltest"

	"example.com/app/db/migrations"
	"example.com/app/internal/modules/records"
	"example.com/app/internal/modules/records/usecase"
)

// These tests drive the records routes through the app's real middleware
// stack, on a new database per test (gorbitaltest), under the app's own
// tenancy: a merchant in the path, and membership answered by a
// gorbital.ScopeAuthorizer (ADR-0088).
//
// The authorizer here is the test's own, so the module can be checked
// without the merchants module's tables. Point newApp at the
// app's real one once you would rather test them together; what these
// tests assert about isolation holds either way.

// The merchants the tests act in.
const (
	adaMerchantID = "mrc_ada"
	bobMerchantID = "mrc_bob"
)

// grants is what each merchant role may do with records. It mirrors
// ScopeRoles in module.go; the app's own authorizer reads it from
// the app's tables instead.
var grants = map[string][]string{
	"owner":   {usecase.PermRead, usecase.PermWrite},
	"manager": {usecase.PermRead, usecase.PermWrite},
	"courier": {usecase.PermRead, usecase.PermWrite},
}

// members is a scope authorizer over a membership table, keyed
// "merchant/actor".
type members map[string]string

func (m members) AuthorizeScope(ctx context.Context, scopeID, permission string) (context.Context, error) {
	a, ok := actor.From(ctx)
	if !ok || a.ID == "" {
		return ctx, actor.ErrUnauthenticated
	}
	role, member := m[scopeID+"/"+a.ID]
	if !member {
		// Unknown, deleted and "not yours" are one answer, so merchant
		// IDs can't be probed.
		return ctx, gorbital.ErrScopeNotFound
	}
	if !slices.Contains(grants[role], permission) {
		return ctx, actor.ErrForbidden
	}
	a.OrgID, a.Permissions = scopeID, grants[role]
	return actor.With(ctx, a), nil
}

// newApp builds the app with the records module and a tenancy whose
// members are ada in a merchant of her own and bob in one of his.
func newApp(t *testing.T) *gorbitaltest.App {
	t.Helper()
	return gorbitaltest.New(t,
		gorbital.WithScope(gorbital.Scope{
			Name:         "merchant",
			PathParam:    "merchantId",
			NotFoundCode: "merchant_not_found",
			ValidID:      func(id string) bool { return strings.HasPrefix(id, "mrc_") },
			Roles: []gorbital.ScopeRole{
				{Name: "owner", Description: "Granted by the app's modules"},
				{Name: "manager", Description: "Granted by the app's modules"},
				{Name: "courier", Description: "Granted by the app's modules"},
			},
		}, members{
			adaMerchantID + "/usr_ada": "owner",
			bobMerchantID + "/usr_bob": "owner",
		}),
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

// apiRecordPage is a page of records.
type apiRecordPage struct {
	Items      []apiRecord `json:"items"`
	NextCursor string      `json:"next_cursor"`
}

// collection is the path of the merchant merchantID's records.
func collection(merchantID string) string {
	return "/v1/merchants/" + merchantID + "/records"
}

func TestCreateAndGetRecord(t *testing.T) {
	app := newApp(t)
	ada := app.As(gorbitaltest.User("usr_ada"))

	res := ada.Post(collection(adaMerchantID), map[string]any{"title": " Website ", "note": "Example note"})
	res.AssertStatus(t, http.StatusCreated)
	var created apiRecord
	res.JSON(t, &created)
	if !strings.HasPrefix(created.ID, "rcr_") || created.Title != "Website" || created.State != "open" || created.Version != 1 {
		t.Errorf("created = %+v, want a rcr_ ID, the title trimmed, the default state and version 1", created)
	}

	var got apiRecord
	ada.Get(collection(adaMerchantID)+"/"+created.ID).JSON(t, &got)
	if got != created {
		t.Errorf("GET = %+v, want %+v", got, created)
	}
}

func TestRecordRules(t *testing.T) {
	app := newApp(t)
	ada := app.As(gorbitaltest.User("usr_ada"))
	ada.Post(collection(adaMerchantID), map[string]any{"title": "Website", "note": "Example note"}).AssertStatus(t, http.StatusCreated)

	for _, tt := range []struct {
		name   string
		body   map[string]any
		status int
		code   string
	}{
		{"blank title", map[string]any{"title": "   ", "note": "Example note"}, http.StatusUnprocessableEntity, "validation_failed"},
		{"missing title", map[string]any{}, http.StatusUnprocessableEntity, "validation_failed"},
		{"unknown state", map[string]any{"title": "Docs", "note": "Example note", "state": "?"}, http.StatusUnprocessableEntity, "validation_failed"},
		{"title already taken, ignoring case", map[string]any{"title": "WEBSITE", "note": "Example note"}, http.StatusConflict, "record_title_taken"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ada.Post(collection(adaMerchantID), tt.body).AssertProblem(t, tt.status, tt.code)
		})
	}
	// Another merchant can use the same title.
	bob := app.As(gorbitaltest.User("usr_bob"))
	bob.Post(collection(bobMerchantID), map[string]any{"title": "Website", "note": "Example note"}).AssertStatus(t, http.StatusCreated)
}

// TestRecordsAreProtected checks deny by default, membership and
// merchant isolation across list, create, get, update and delete: a
// merchant somebody isn't a member of doesn't exist for them, an unknown
// or malformed merchant ID is refused the same way so IDs can't be
// probed, and another merchant's record isn't in theirs.
func TestRecordsAreProtected(t *testing.T) {
	app := newApp(t)
	ada := app.As(gorbitaltest.User("usr_ada"))
	bob := app.As(gorbitaltest.User("usr_bob"))
	var website apiRecord
	ada.Post(collection(adaMerchantID), map[string]any{"title": "Website", "note": "Example note"}).JSON(t, &website)
	item := collection(adaMerchantID) + "/" + website.ID

	app.Client().Get(collection(adaMerchantID)).AssertProblem(t, http.StatusUnauthorized, "unauthenticated")

	for name, res := range map[string]*gorbitaltest.Response{
		"list":                           bob.Get(collection(adaMerchantID)),
		"create":                         bob.Post(collection(adaMerchantID), map[string]any{"title": "Mine now", "note": "Example note"}),
		"get":                            bob.Get(item),
		"update":                         bob.Patch(item, map[string]any{"version": 1, "title": "Mine now"}),
		"delete":                         bob.Delete(item),
		"list in an unknown merchant":    bob.Get(collection("mrc_nobody")),
		"list with a malformed merchant": bob.Get(collection("not-a-merchant")),
	} {
		t.Run("non-member "+name, func(t *testing.T) {
			res.AssertProblem(t, http.StatusNotFound, "merchant_not_found")
		})
	}

	// In his own merchant, bob is a member — and ada's record still
	// isn't his.
	bobItem := collection(bobMerchantID) + "/" + website.ID
	bob.Get(bobItem).AssertProblem(t, http.StatusNotFound, "record_not_found")
	bob.Patch(bobItem, map[string]any{"version": 1, "title": "Mine now"}).AssertProblem(t, http.StatusNotFound, "record_not_found")
	bob.Delete(bobItem).AssertProblem(t, http.StatusNotFound, "record_not_found")
	var bobs apiRecordPage
	bob.Get(collection(bobMerchantID)).JSON(t, &bobs)
	if len(bobs.Items) != 0 {
		t.Errorf("another merchant's list = %+v, want none", bobs.Items)
	}
}

func TestListRecordsPages(t *testing.T) {
	app := newApp(t)
	ada := app.As(gorbitaltest.User("usr_ada"))
	ada.Post(collection(adaMerchantID), map[string]any{"title": "Website", "note": "Example note"}).AssertStatus(t, http.StatusCreated)
	ada.Post(collection(adaMerchantID), map[string]any{"title": "Docs", "note": "Example note", "state": "done"}).AssertStatus(t, http.StatusCreated)

	var titles []string
	cursor := ""
	for range 3 {
		var p apiRecordPage
		ada.Get(collection(adaMerchantID)+"?limit=1&sort=title&cursor="+url.QueryEscape(cursor)).JSON(t, &p)
		for _, item := range p.Items {
			titles = append(titles, item.Title)
		}
		if cursor = p.NextCursor; cursor == "" {
			break
		}
	}
	if want := []string{"Docs", "Website"}; !slices.Equal(titles, want) {
		t.Errorf("pages sorted by title = %v, want %v", titles, want)
	}

	var first apiRecordPage
	ada.Get(collection(adaMerchantID)+"?limit=1").JSON(t, &first)
	ada.Get(collection(adaMerchantID)+"?sort=title&cursor="+url.QueryEscape(first.NextCursor)).AssertProblem(t, http.StatusBadRequest, "invalid_cursor")
	// A sort can't reach the merchant column, which isolation depends on.
	ada.Get(collection(adaMerchantID)+"?sort=merchant_id").AssertProblem(t, http.StatusBadRequest, "invalid_sort")

	var filtered apiRecordPage
	ada.Get(collection(adaMerchantID)+"?state=done").JSON(t, &filtered)
	if len(filtered.Items) != 1 || filtered.Items[0].Title != "Docs" {
		t.Errorf("list with state=done = %+v, want Docs only", filtered.Items)
	}
}

func TestUpdateAndDeleteRecord(t *testing.T) {
	app := newApp(t)
	ada := app.As(gorbitaltest.User("usr_ada"))
	var website apiRecord
	ada.Post(collection(adaMerchantID), map[string]any{"title": "Website", "note": "Example note"}).JSON(t, &website)
	item := collection(adaMerchantID) + "/" + website.ID

	ada.Patch(item, map[string]any{"title": "No version"}).AssertProblem(t, http.StatusUnprocessableEntity, "validation_failed")
	ada.Patch(item, map[string]any{"version": 2, "title": "Too new"}).AssertProblem(t, http.StatusConflict, "record_version_conflict")

	res := ada.Patch(item, map[string]any{"version": 1, "title": "Renamed", "state": "done"})
	res.AssertStatus(t, http.StatusOK)
	var updated apiRecord
	res.JSON(t, &updated)
	if updated.Title != "Renamed" || updated.State != "done" || updated.Version != 2 {
		t.Errorf("updated = %+v, want the new values and version 2", updated)
	}
	// Changing a record with the version it already had is a conflict.
	ada.Patch(item, map[string]any{"version": 1, "title": "Again"}).AssertProblem(t, http.StatusConflict, "record_version_conflict")

	ada.Delete(item).AssertStatus(t, http.StatusNoContent)
	ada.Get(item).AssertProblem(t, http.StatusNotFound, "record_not_found")
	ada.Delete(item).AssertProblem(t, http.StatusNotFound, "record_not_found")

	// Every event names the member and the merchant.
	rows, err := app.App().Deps().DB.Query(context.Background(),
		`SELECT action, actor_id, coalesce(org_id, '') FROM audit_events WHERE resource_type = 'record' AND resource_id = $1 ORDER BY id`, website.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var trail []string
	for rows.Next() {
		var action, actorID, scopeID string
		if err := rows.Scan(&action, &actorID, &scopeID); err != nil {
			t.Fatal(err)
		}
		trail = append(trail, action+" by "+actorID+" in "+scopeID)
	}
	var want []string
	for _, action := range []string{usecase.ActionCreated, usecase.ActionUpdated, usecase.ActionDeleted} {
		want = append(want, action+" by usr_ada in "+adaMerchantID)
	}
	if !slices.Equal(trail, want) {
		t.Errorf("audit trail = %v, want %v", trail, want)
	}
}
