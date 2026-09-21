package shelves_test

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/gorbitaltest"

	"example.com/shelfie/db/migrations"
	"example.com/shelfie/internal/modules/shelves"
	"example.com/shelfie/internal/modules/shelves/usecase"
)

// These tests drive the shelves routes through the app's real middleware
// stack, on a new database per test (gorbitaltest).

// newApp builds the app with the shelves module for the test.
func newApp(t *testing.T) *gorbitaltest.App {
	t.Helper()
	return gorbitaltest.New(t,
		gorbital.WithModules(shelves.Module()),
		gorbital.WithMigrations(migrations.FS),
	)
}

// apiShelf is a shelf as the API returns it.
type apiShelf struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Visibility  string `json:"visibility"`
	Version     int64  `json:"version"`
}

// apiShelfPage is a page of shelves.
type apiShelfPage struct {
	Items      []apiShelf `json:"items"`
	NextCursor string     `json:"next_cursor"`
}

const collection = "/v1/shelves"

func TestCreateAndGetShelf(t *testing.T) {
	app := newApp(t)
	ada := app.As(gorbitaltest.User("usr_ada", usecase.PermRead, usecase.PermWrite))

	res := ada.Post(collection, map[string]any{"name": " Website ", "description": "Example description"})
	res.AssertStatus(t, http.StatusCreated)
	var created apiShelf
	res.JSON(t, &created)
	if !strings.HasPrefix(created.ID, "shl_") || created.Name != "Website" || created.Visibility != "private" || created.Version != 1 {
		t.Errorf("created = %+v, want a shl_ ID, the name trimmed, the default visibility and version 1", created)
	}

	var got apiShelf
	ada.Get(collection+"/"+created.ID).JSON(t, &got)
	if got != created {
		t.Errorf("GET = %+v, want %+v", got, created)
	}
}

func TestShelfRules(t *testing.T) {
	app := newApp(t)
	ada := app.As(gorbitaltest.User("usr_ada", usecase.PermRead, usecase.PermWrite))
	ada.Post(collection, map[string]any{"name": "Website", "description": "Example description"}).AssertStatus(t, http.StatusCreated)

	for _, tt := range []struct {
		name   string
		body   map[string]any
		status int
		code   string
	}{
		{"blank name", map[string]any{"name": "   ", "description": "Example description"}, http.StatusUnprocessableEntity, "validation_failed"},
		{"missing name", map[string]any{}, http.StatusUnprocessableEntity, "validation_failed"},
		{"unknown visibility", map[string]any{"name": "Docs", "description": "Example description", "visibility": "?"}, http.StatusUnprocessableEntity, "validation_failed"},
		{"name already taken, ignoring case", map[string]any{"name": "WEBSITE", "description": "Example description"}, http.StatusConflict, "shelf_name_taken"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ada.Post(collection, tt.body).AssertProblem(t, tt.status, tt.code)
		})
	}
	// Another user can use the same name.
	app.As(gorbitaltest.User("usr_bob", usecase.PermWrite)).Post(collection, map[string]any{"name": "Website", "description": "Example description"}).AssertStatus(t, http.StatusCreated)
}

// TestShelvesAreProtected checks deny by default, the permission guards and
// owner isolation: someone else's shelf doesn't exist for them.
func TestShelvesAreProtected(t *testing.T) {
	app := newApp(t)
	ada := app.As(gorbitaltest.User("usr_ada", usecase.PermRead, usecase.PermWrite))
	var website apiShelf
	ada.Post(collection, map[string]any{"name": "Website", "description": "Example description"}).JSON(t, &website)
	item := collection + "/" + website.ID

	app.Client().Get(collection).AssertProblem(t, http.StatusUnauthorized, "unauthenticated")

	bob := app.As(gorbitaltest.User("usr_bob", usecase.PermRead, usecase.PermWrite))
	bob.Get(item).AssertProblem(t, http.StatusNotFound, "shelf_not_found")
	bob.Patch(item, map[string]any{"version": 1, "name": "Mine now"}).AssertProblem(t, http.StatusNotFound, "shelf_not_found")
	bob.Delete(item).AssertProblem(t, http.StatusNotFound, "shelf_not_found")
	var bobs apiShelfPage
	bob.Get(collection).JSON(t, &bobs)
	if len(bobs.Items) != 0 {
		t.Errorf("another user's list = %+v, want none", bobs.Items)
	}

	readOnly := app.As(gorbitaltest.APIKey("usr_ada", usecase.PermRead))
	readOnly.Get(item).AssertStatus(t, http.StatusOK)
	readOnly.Post(collection, map[string]any{"name": "By a key", "description": "Example description"}).AssertProblem(t, http.StatusForbidden, "forbidden")
	readOnly.Patch(item, map[string]any{"version": 1, "name": "By a key"}).AssertProblem(t, http.StatusForbidden, "forbidden")
	readOnly.Delete(item).AssertProblem(t, http.StatusForbidden, "forbidden")
}

func TestListShelvesPages(t *testing.T) {
	app := newApp(t)
	ada := app.As(gorbitaltest.User("usr_ada", usecase.PermRead, usecase.PermWrite))
	ada.Post(collection, map[string]any{"name": "Website", "description": "Example description"}).AssertStatus(t, http.StatusCreated)
	ada.Post(collection, map[string]any{"name": "Docs", "description": "Example description", "visibility": "shared"}).AssertStatus(t, http.StatusCreated)

	var titles []string
	cursor := ""
	for range 3 {
		var p apiShelfPage
		ada.Get(collection+"?limit=1&sort=name&cursor="+url.QueryEscape(cursor)).JSON(t, &p)
		for _, item := range p.Items {
			titles = append(titles, item.Name)
		}
		if cursor = p.NextCursor; cursor == "" {
			break
		}
	}
	if want := []string{"Docs", "Website"}; !slices.Equal(titles, want) {
		t.Errorf("pages sorted by name = %v, want %v", titles, want)
	}

	var first apiShelfPage
	ada.Get(collection+"?limit=1").JSON(t, &first)
	ada.Get(collection+"?sort=name&cursor="+url.QueryEscape(first.NextCursor)).AssertProblem(t, http.StatusBadRequest, "invalid_cursor")
	ada.Get(collection+"?sort=owner_id").AssertProblem(t, http.StatusBadRequest, "invalid_sort")

	var filtered apiShelfPage
	ada.Get(collection+"?visibility=shared").JSON(t, &filtered)
	if len(filtered.Items) != 1 || filtered.Items[0].Name != "Docs" {
		t.Errorf("list with visibility=shared = %+v, want Docs only", filtered.Items)
	}
}

func TestUpdateAndDeleteShelf(t *testing.T) {
	app := newApp(t)
	ada := app.As(gorbitaltest.User("usr_ada", usecase.PermRead, usecase.PermWrite))
	var website apiShelf
	ada.Post(collection, map[string]any{"name": "Website", "description": "Example description"}).JSON(t, &website)
	item := collection + "/" + website.ID

	ada.Patch(item, map[string]any{"name": "No version"}).AssertProblem(t, http.StatusUnprocessableEntity, "validation_failed")
	ada.Patch(item, map[string]any{"version": 2, "name": "Too new"}).AssertProblem(t, http.StatusConflict, "shelf_version_conflict")

	res := ada.Patch(item, map[string]any{"version": 1, "name": "Renamed", "visibility": "shared"})
	res.AssertStatus(t, http.StatusOK)
	var updated apiShelf
	res.JSON(t, &updated)
	if updated.Name != "Renamed" || updated.Visibility != "shared" || updated.Version != 2 {
		t.Errorf("updated = %+v, want the new values and version 2", updated)
	}
	// Changing a shelf with the version it already had is a conflict.
	ada.Patch(item, map[string]any{"version": 1, "name": "Again"}).AssertProblem(t, http.StatusConflict, "shelf_version_conflict")

	ada.Delete(item).AssertStatus(t, http.StatusNoContent)
	ada.Get(item).AssertProblem(t, http.StatusNotFound, "shelf_not_found")
	ada.Delete(item).AssertProblem(t, http.StatusNotFound, "shelf_not_found")

	rows, err := app.App().Deps().DB.Query(context.Background(),
		`SELECT action FROM audit_events WHERE resource_type = 'shelf' AND resource_id = $1 ORDER BY id`, website.ID)
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
