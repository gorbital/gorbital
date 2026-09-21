package records_test

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/gorbitaltest"

	"example.com/app/db/migrations"
	"example.com/app/internal/modules/records"
	"example.com/app/internal/modules/records/usecase"
)

// These tests drive the records routes through the app's real middleware
// stack, on a new database per test (gorbitaltest).

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

// apiRecordPage is a page of records.
type apiRecordPage struct {
	Items      []apiRecord `json:"items"`
	NextCursor string      `json:"next_cursor"`
}

const collection = "/v1/records"

func TestCreateAndGetRecord(t *testing.T) {
	app := newApp(t)
	ada := app.As(gorbitaltest.User("usr_ada", usecase.PermRead, usecase.PermWrite))

	res := ada.Post(collection, map[string]any{"title": " Website ", "note": "Example note"})
	res.AssertStatus(t, http.StatusCreated)
	var created apiRecord
	res.JSON(t, &created)
	if !strings.HasPrefix(created.ID, "rcr_") || created.Title != "Website" || created.State != "open" || created.Version != 1 {
		t.Errorf("created = %+v, want a rcr_ ID, the title trimmed, the default state and version 1", created)
	}

	var got apiRecord
	ada.Get(collection+"/"+created.ID).JSON(t, &got)
	if got != created {
		t.Errorf("GET = %+v, want %+v", got, created)
	}
}

func TestRecordRules(t *testing.T) {
	app := newApp(t)
	ada := app.As(gorbitaltest.User("usr_ada", usecase.PermRead, usecase.PermWrite))
	ada.Post(collection, map[string]any{"title": "Website", "note": "Example note"}).AssertStatus(t, http.StatusCreated)

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
			ada.Post(collection, tt.body).AssertProblem(t, tt.status, tt.code)
		})
	}
	// Another user can use the same title.
	app.As(gorbitaltest.User("usr_bob", usecase.PermWrite)).Post(collection, map[string]any{"title": "Website", "note": "Example note"}).AssertStatus(t, http.StatusCreated)
}

// TestRecordsAreProtected checks deny by default, the permission guards and
// owner isolation: someone else's record doesn't exist for them.
func TestRecordsAreProtected(t *testing.T) {
	app := newApp(t)
	ada := app.As(gorbitaltest.User("usr_ada", usecase.PermRead, usecase.PermWrite))
	var website apiRecord
	ada.Post(collection, map[string]any{"title": "Website", "note": "Example note"}).JSON(t, &website)
	item := collection + "/" + website.ID

	app.Client().Get(collection).AssertProblem(t, http.StatusUnauthorized, "unauthenticated")

	bob := app.As(gorbitaltest.User("usr_bob", usecase.PermRead, usecase.PermWrite))
	bob.Get(item).AssertProblem(t, http.StatusNotFound, "record_not_found")
	bob.Patch(item, map[string]any{"version": 1, "title": "Mine now"}).AssertProblem(t, http.StatusNotFound, "record_not_found")
	bob.Delete(item).AssertProblem(t, http.StatusNotFound, "record_not_found")
	var bobs apiRecordPage
	bob.Get(collection).JSON(t, &bobs)
	if len(bobs.Items) != 0 {
		t.Errorf("another user's list = %+v, want none", bobs.Items)
	}

	readOnly := app.As(gorbitaltest.APIKey("usr_ada", usecase.PermRead))
	readOnly.Get(item).AssertStatus(t, http.StatusOK)
	readOnly.Post(collection, map[string]any{"title": "By a key", "note": "Example note"}).AssertProblem(t, http.StatusForbidden, "forbidden")
	readOnly.Patch(item, map[string]any{"version": 1, "title": "By a key"}).AssertProblem(t, http.StatusForbidden, "forbidden")
	readOnly.Delete(item).AssertProblem(t, http.StatusForbidden, "forbidden")
}

func TestListRecordsPages(t *testing.T) {
	app := newApp(t)
	ada := app.As(gorbitaltest.User("usr_ada", usecase.PermRead, usecase.PermWrite))
	ada.Post(collection, map[string]any{"title": "Website", "note": "Example note"}).AssertStatus(t, http.StatusCreated)
	ada.Post(collection, map[string]any{"title": "Docs", "note": "Example note", "state": "done"}).AssertStatus(t, http.StatusCreated)

	var titles []string
	cursor := ""
	for range 3 {
		var p apiRecordPage
		ada.Get(collection+"?limit=1&sort=title&cursor="+url.QueryEscape(cursor)).JSON(t, &p)
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
	ada.Get(collection+"?limit=1").JSON(t, &first)
	ada.Get(collection+"?sort=title&cursor="+url.QueryEscape(first.NextCursor)).AssertProblem(t, http.StatusBadRequest, "invalid_cursor")
	ada.Get(collection+"?sort=owner_id").AssertProblem(t, http.StatusBadRequest, "invalid_sort")

	var filtered apiRecordPage
	ada.Get(collection+"?state=done").JSON(t, &filtered)
	if len(filtered.Items) != 1 || filtered.Items[0].Title != "Docs" {
		t.Errorf("list with state=done = %+v, want Docs only", filtered.Items)
	}
}

func TestUpdateAndDeleteRecord(t *testing.T) {
	app := newApp(t)
	ada := app.As(gorbitaltest.User("usr_ada", usecase.PermRead, usecase.PermWrite))
	var website apiRecord
	ada.Post(collection, map[string]any{"title": "Website", "note": "Example note"}).JSON(t, &website)
	item := collection + "/" + website.ID

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

	rows, err := app.App().Deps().DB.Query(context.Background(),
		`SELECT action FROM audit_events WHERE resource_type = 'record' AND resource_id = $1 ORDER BY id`, website.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var trail []string
	for rows.Next() {
		var action string
		if err := rows.Scan(&action); err != nil {
			t.Fatal(err)
		}
		trail = append(trail, action)
	}
	if want := []string{usecase.ActionCreated, usecase.ActionUpdated, usecase.ActionDeleted}; !slices.Equal(trail, want) {
		t.Errorf("audit trail = %v, want %v", trail, want)
	}
}
