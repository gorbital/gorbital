package restaurants_test

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/authhttp"
	"gorbital.dev/gorbital/gorbitaltest"
	"gorbital.dev/gorbital/orgshttp"
	"gorbital.dev/modules/postgres"

	"example.com/plateful/db/migrations"
	"example.com/plateful/internal/modules/restaurants"
	"example.com/plateful/internal/modules/restaurants/usecase"
)

// These tests drive the restaurants routes through the app's real middleware
// stack, on a new database per test (gorbitaltest), with real accounts:
// organisation members are sign-in's users.

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

// signUp creates an account for email and returns its client, its user ID
// and its personal workspace, the organisation every account gets.
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

// collection is the path of the organisation orgID's restaurants.
func collection(orgID string) string { return "/v1/orgs/" + orgID + "/restaurants" }

// apiKey creates an API key for the client's account, limited to scopes.
func apiKey(t *testing.T, client *gorbitaltest.Client, scopes ...string) string {
	t.Helper()
	res := client.Post("/v1/auth/api-keys", map[string]any{
		"name": "Test", "scopes": scopes, "password": gorbitaltest.SignUpPassword,
		"expires_at": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
	})
	res.AssertStatus(t, http.StatusCreated)
	var created struct {
		Key string `json:"key"`
	}
	res.JSON(t, &created)
	return created.Key
}

// apiRestaurant is a restaurant as the API returns it.
type apiRestaurant struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Address   string `json:"address"`
	Cuisine   string `json:"cuisine"`
	Status    string `json:"status"`
	CreatedBy string `json:"created_by"`
	Version   int64  `json:"version"`
}

// apiRestaurantPage is a page of restaurants.
type apiRestaurantPage struct {
	Items      []apiRestaurant `json:"items"`
	NextCursor string          `json:"next_cursor"`
}

func TestCreateAndGetRestaurant(t *testing.T) {
	app := newApp(t)
	ada, adaID, adaOrg := signUp(t, app, "ada@example.com")

	res := ada.Post(collection(adaOrg), map[string]any{"name": " Website ", "address": "Example address", "cuisine": "Example cuisine"})
	res.AssertStatus(t, http.StatusCreated)
	var created apiRestaurant
	res.JSON(t, &created)
	if !strings.HasPrefix(created.ID, "rst_") || created.Name != "Website" || created.Status != "onboarding" || created.CreatedBy != adaID || created.Version != 1 {
		t.Errorf("created = %+v, want a rst_ ID, the name trimmed, the default status, created by Ada and version 1", created)
	}

	var got apiRestaurant
	ada.Get(collection(adaOrg)+"/"+created.ID).JSON(t, &got)
	if got != created {
		t.Errorf("GET = %+v, want %+v", got, created)
	}
}

func TestRestaurantRules(t *testing.T) {
	app := newApp(t)
	ada, _, adaOrg := signUp(t, app, "ada@example.com")
	ada.Post(collection(adaOrg), map[string]any{"name": "Website", "address": "Example address", "cuisine": "Example cuisine"}).AssertStatus(t, http.StatusCreated)

	for _, tt := range []struct {
		name   string
		body   map[string]any
		status int
		code   string
	}{
		{"blank name", map[string]any{"name": "   ", "address": "Example address", "cuisine": "Example cuisine"}, http.StatusUnprocessableEntity, "validation_failed"},
		{"missing name", map[string]any{}, http.StatusUnprocessableEntity, "validation_failed"},
		{"unknown status", map[string]any{"name": "Docs", "address": "Example address", "cuisine": "Example cuisine", "status": "?"}, http.StatusUnprocessableEntity, "validation_failed"},
		{"name already taken, ignoring case", map[string]any{"name": "WEBSITE", "address": "Example address", "cuisine": "Example cuisine"}, http.StatusConflict, "restaurant_name_taken"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ada.Post(collection(adaOrg), tt.body).AssertProblem(t, tt.status, tt.code)
		})
	}
	// Another organisation can use the same name.
	bob, _, bobOrg := signUp(t, app, "bob@example.com")
	bob.Post(collection(bobOrg), map[string]any{"name": "Website", "address": "Example address", "cuisine": "Example cuisine"}).AssertStatus(t, http.StatusCreated)
}

// TestRestaurantsAreProtected checks deny by default, membership, organisation
// isolation and API key scopes: an organisation someone isn't a member of
// doesn't exist for them, and another organisation's restaurant isn't in theirs.
func TestRestaurantsAreProtected(t *testing.T) {
	app := newApp(t)
	ada, _, adaOrg := signUp(t, app, "ada@example.com")
	bob, _, bobOrg := signUp(t, app, "bob@example.com")
	var website apiRestaurant
	ada.Post(collection(adaOrg), map[string]any{"name": "Website", "address": "Example address", "cuisine": "Example cuisine"}).JSON(t, &website)
	item := collection(adaOrg) + "/" + website.ID

	app.Client().Get(collection(adaOrg)).AssertProblem(t, http.StatusUnauthorized, "unauthenticated")

	for name, res := range map[string]*gorbitaltest.Response{
		"list":                      bob.Get(collection(adaOrg)),
		"create":                    bob.Post(collection(adaOrg), map[string]any{"name": "Mine now", "address": "Example address", "cuisine": "Example cuisine"}),
		"get":                       bob.Get(item),
		"update":                    bob.Patch(item, map[string]any{"version": 1, "name": "Mine now"}),
		"delete":                    bob.Delete(item),
		"list in an unknown org":    bob.Get(collection("org_mfrggzdfmztwq2lkmfrggzdfmy")),
		"list with a malformed org": bob.Get(collection("not-an-org")),
	} {
		t.Run("non-member "+name, func(t *testing.T) {
			res.AssertProblem(t, http.StatusNotFound, "org_not_found")
		})
	}

	bobItem := collection(bobOrg) + "/" + website.ID
	bob.Get(bobItem).AssertProblem(t, http.StatusNotFound, "restaurant_not_found")
	bob.Patch(bobItem, map[string]any{"version": 1, "name": "Mine now"}).AssertProblem(t, http.StatusNotFound, "restaurant_not_found")
	bob.Delete(bobItem).AssertProblem(t, http.StatusNotFound, "restaurant_not_found")
	var bobs apiRestaurantPage
	bob.Get(collection(bobOrg)).JSON(t, &bobs)
	if len(bobs.Items) != 0 {
		t.Errorf("another organisation's list = %+v, want none", bobs.Items)
	}

	readOnly := app.Client().WithHeader("Authorization", "Bearer "+apiKey(t, ada, usecase.PermRead))
	readOnly.Get(item).AssertStatus(t, http.StatusOK)
	readOnly.Post(collection(adaOrg), map[string]any{"name": "By a key", "address": "Example address", "cuisine": "Example cuisine"}).AssertProblem(t, http.StatusForbidden, "forbidden")
	readOnly.Patch(item, map[string]any{"version": 1, "name": "By a key"}).AssertProblem(t, http.StatusForbidden, "forbidden")
	readOnly.Delete(item).AssertProblem(t, http.StatusForbidden, "forbidden")
}

func TestListRestaurantsPages(t *testing.T) {
	app := newApp(t)
	ada, _, adaOrg := signUp(t, app, "ada@example.com")
	ada.Post(collection(adaOrg), map[string]any{"name": "Website", "address": "Example address", "cuisine": "Example cuisine"}).AssertStatus(t, http.StatusCreated)
	ada.Post(collection(adaOrg), map[string]any{"name": "Docs", "address": "Example address", "cuisine": "Example cuisine", "status": "suspended"}).AssertStatus(t, http.StatusCreated)

	var titles []string
	cursor := ""
	for range 3 {
		var p apiRestaurantPage
		ada.Get(collection(adaOrg)+"?limit=1&sort=name&cursor="+url.QueryEscape(cursor)).JSON(t, &p)
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

	var first apiRestaurantPage
	ada.Get(collection(adaOrg)+"?limit=1").JSON(t, &first)
	ada.Get(collection(adaOrg)+"?sort=name&cursor="+url.QueryEscape(first.NextCursor)).AssertProblem(t, http.StatusBadRequest, "invalid_cursor")
	ada.Get(collection(adaOrg)+"?sort=org_id").AssertProblem(t, http.StatusBadRequest, "invalid_sort")

	var filtered apiRestaurantPage
	ada.Get(collection(adaOrg)+"?status=suspended").JSON(t, &filtered)
	if len(filtered.Items) != 1 || filtered.Items[0].Name != "Docs" {
		t.Errorf("list with status=suspended = %+v, want Docs only", filtered.Items)
	}
}

func TestUpdateAndDeleteRestaurant(t *testing.T) {
	app := newApp(t)
	ada, adaID, adaOrg := signUp(t, app, "ada@example.com")
	var website apiRestaurant
	ada.Post(collection(adaOrg), map[string]any{"name": "Website", "address": "Example address", "cuisine": "Example cuisine"}).JSON(t, &website)
	item := collection(adaOrg) + "/" + website.ID

	ada.Patch(item, map[string]any{"name": "No version"}).AssertProblem(t, http.StatusUnprocessableEntity, "validation_failed")
	ada.Patch(item, map[string]any{"version": 2, "name": "Too new"}).AssertProblem(t, http.StatusConflict, "restaurant_version_conflict")

	res := ada.Patch(item, map[string]any{"version": 1, "name": "Renamed", "status": "suspended"})
	res.AssertStatus(t, http.StatusOK)
	var updated apiRestaurant
	res.JSON(t, &updated)
	if updated.Name != "Renamed" || updated.Status != "suspended" || updated.Version != 2 {
		t.Errorf("updated = %+v, want the new values and version 2", updated)
	}
	// Changing a restaurant with the version it already had is a conflict.
	ada.Patch(item, map[string]any{"version": 1, "name": "Again"}).AssertProblem(t, http.StatusConflict, "restaurant_version_conflict")

	ada.Delete(item).AssertStatus(t, http.StatusNoContent)
	ada.Get(item).AssertProblem(t, http.StatusNotFound, "restaurant_not_found")
	ada.Delete(item).AssertProblem(t, http.StatusNotFound, "restaurant_not_found")

	// Every event names the member and the organisation.
	rows, err := app.App().Deps().DB.Query(context.Background(),
		`SELECT action, actor_id, coalesce(org_id, '') FROM audit_events WHERE resource_type = 'restaurant' AND resource_id = $1 ORDER BY id`, website.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var trail []string
	for rows.Next() {
		var action, actorID, orgID string
		if err := rows.Scan(&action, &actorID, &orgID); err != nil {
			t.Fatal(err)
		}
		trail = append(trail, action+" by "+actorID+" in "+orgID)
	}
	var want []string
	for _, action := range []string{usecase.ActionCreated, usecase.ActionUpdated, usecase.ActionDeleted} {
		want = append(want, action+" by "+adaID+" in "+adaOrg)
	}
	if !slices.Equal(trail, want) {
		t.Errorf("audit trail = %v, want %v", trail, want)
	}
}

// TestPurgingAnOrganisationDeletesItsRestaurants checks the migration's foreign key:
// the orgs_purge job deletes an organisation once its restore period ends,
// and its restaurants go with it.
func TestPurgingAnOrganisationDeletesItsRestaurants(t *testing.T) {
	app := newApp(t)
	ada, _, adaOrg := signUp(t, app, "ada@example.com")
	ada.Post(collection(adaOrg), map[string]any{"name": "Website", "address": "Example address", "cuisine": "Example cuisine"}).AssertStatus(t, http.StatusCreated)

	// Every organisation's rows, whether or not the app has row-level security.
	ctx := postgres.WithoutRowLevelSecurity(context.Background(), "test: purge")
	db := app.App().Deps().DB
	if _, err := db.Exec(ctx, `DELETE FROM orgs WHERE id = $1`, adaOrg); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM restaurants WHERE org_id = $1`, adaOrg).Scan(&left); err != nil || left != 0 {
		t.Errorf("restaurants left after purging the organisation = %d, %v; want 0", left, err)
	}
}
