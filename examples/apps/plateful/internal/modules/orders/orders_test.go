package orders_test

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
	"example.com/plateful/internal/modules/orders"
	"example.com/plateful/internal/modules/orders/usecase"
)

// These tests drive the orders routes through the app's real middleware
// stack, on a new database per test (gorbitaltest), with real accounts:
// organisation members are sign-in's users.

// newApp builds the app with sign-in, organisations and the orders
// module for the test.
func newApp(t *testing.T) *gorbitaltest.App {
	t.Helper()
	auth := authhttp.New()
	return gorbitaltest.New(t,
		gorbital.WithAuth(auth),
		gorbital.WithModules(orgshttp.Module(auth), orders.Module()),
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

// collection is the path of the organisation orgID's orders.
func collection(orgID string) string { return "/v1/orgs/" + orgID + "/orders" }

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

// apiOrder is an order as the API returns it.
type apiOrder struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Address   string `json:"address"`
	Note      string `json:"note"`
	CreatedBy string `json:"created_by"`
	Version   int64  `json:"version"`
}

// apiOrderPage is a page of orders.
type apiOrderPage struct {
	Items      []apiOrder `json:"items"`
	NextCursor string     `json:"next_cursor"`
}

func TestCreateAndGetOrder(t *testing.T) {
	app := newApp(t)
	ada, adaID, adaOrg := signUp(t, app, "ada@example.com")

	res := ada.Post(collection(adaOrg), map[string]any{"address": " Website ", "note": "Example note"})
	res.AssertStatus(t, http.StatusCreated)
	var created apiOrder
	res.JSON(t, &created)
	if !strings.HasPrefix(created.ID, "ord_") || created.Address != "Website" || created.Status != "placed" || created.CreatedBy != adaID || created.Version != 1 {
		t.Errorf("created = %+v, want a ord_ ID, the address trimmed, the default status, created by Ada and version 1", created)
	}

	var got apiOrder
	ada.Get(collection(adaOrg)+"/"+created.ID).JSON(t, &got)
	if got != created {
		t.Errorf("GET = %+v, want %+v", got, created)
	}
}

func TestOrderRules(t *testing.T) {
	app := newApp(t)
	ada, _, adaOrg := signUp(t, app, "ada@example.com")
	ada.Post(collection(adaOrg), map[string]any{"address": "Website", "note": "Example note"}).AssertStatus(t, http.StatusCreated)

	for _, tt := range []struct {
		name   string
		body   map[string]any
		status int
		code   string
	}{
		{"blank address", map[string]any{"address": "   ", "note": "Example note"}, http.StatusUnprocessableEntity, "validation_failed"},
		{"missing address", map[string]any{}, http.StatusUnprocessableEntity, "validation_failed"},
		{"unknown status", map[string]any{"address": "Docs", "note": "Example note", "status": "?"}, http.StatusUnprocessableEntity, "validation_failed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ada.Post(collection(adaOrg), tt.body).AssertProblem(t, tt.status, tt.code)
		})
	}
}

// TestOrdersAreProtected checks deny by default, membership, organisation
// isolation and API key scopes: an organisation someone isn't a member of
// doesn't exist for them, and another organisation's order isn't in theirs.
func TestOrdersAreProtected(t *testing.T) {
	app := newApp(t)
	ada, _, adaOrg := signUp(t, app, "ada@example.com")
	bob, _, bobOrg := signUp(t, app, "bob@example.com")
	var website apiOrder
	ada.Post(collection(adaOrg), map[string]any{"address": "Website", "note": "Example note"}).JSON(t, &website)
	item := collection(adaOrg) + "/" + website.ID

	app.Client().Get(collection(adaOrg)).AssertProblem(t, http.StatusUnauthorized, "unauthenticated")

	for name, res := range map[string]*gorbitaltest.Response{
		"list":                      bob.Get(collection(adaOrg)),
		"create":                    bob.Post(collection(adaOrg), map[string]any{"address": "Mine now", "note": "Example note"}),
		"get":                       bob.Get(item),
		"update":                    bob.Patch(item, map[string]any{"version": 1, "address": "Mine now"}),
		"delete":                    bob.Delete(item),
		"list in an unknown org":    bob.Get(collection("org_mfrggzdfmztwq2lkmfrggzdfmy")),
		"list with a malformed org": bob.Get(collection("not-an-org")),
	} {
		t.Run("non-member "+name, func(t *testing.T) {
			res.AssertProblem(t, http.StatusNotFound, "org_not_found")
		})
	}

	bobItem := collection(bobOrg) + "/" + website.ID
	bob.Get(bobItem).AssertProblem(t, http.StatusNotFound, "order_not_found")
	bob.Patch(bobItem, map[string]any{"version": 1, "address": "Mine now"}).AssertProblem(t, http.StatusNotFound, "order_not_found")
	bob.Delete(bobItem).AssertProblem(t, http.StatusNotFound, "order_not_found")
	var bobs apiOrderPage
	bob.Get(collection(bobOrg)).JSON(t, &bobs)
	if len(bobs.Items) != 0 {
		t.Errorf("another organisation's list = %+v, want none", bobs.Items)
	}

	readOnly := app.Client().WithHeader("Authorization", "Bearer "+apiKey(t, ada, usecase.PermRead))
	readOnly.Get(item).AssertStatus(t, http.StatusOK)
	readOnly.Post(collection(adaOrg), map[string]any{"address": "By a key", "note": "Example note"}).AssertProblem(t, http.StatusForbidden, "forbidden")
	readOnly.Patch(item, map[string]any{"version": 1, "address": "By a key"}).AssertProblem(t, http.StatusForbidden, "forbidden")
	readOnly.Delete(item).AssertProblem(t, http.StatusForbidden, "forbidden")
}

func TestListOrdersPages(t *testing.T) {
	app := newApp(t)
	ada, _, adaOrg := signUp(t, app, "ada@example.com")
	ada.Post(collection(adaOrg), map[string]any{"address": "Website", "note": "Example note"}).AssertStatus(t, http.StatusCreated)
	ada.Post(collection(adaOrg), map[string]any{"address": "Docs", "note": "Example note", "status": "cancelled"}).AssertStatus(t, http.StatusCreated)

	var titles []string
	cursor := ""
	for range 3 {
		var p apiOrderPage
		ada.Get(collection(adaOrg)+"?limit=1&sort=address&cursor="+url.QueryEscape(cursor)).JSON(t, &p)
		for _, item := range p.Items {
			titles = append(titles, item.Address)
		}
		if cursor = p.NextCursor; cursor == "" {
			break
		}
	}
	if want := []string{"Docs", "Website"}; !slices.Equal(titles, want) {
		t.Errorf("pages sorted by address = %v, want %v", titles, want)
	}

	var first apiOrderPage
	ada.Get(collection(adaOrg)+"?limit=1").JSON(t, &first)
	ada.Get(collection(adaOrg)+"?sort=address&cursor="+url.QueryEscape(first.NextCursor)).AssertProblem(t, http.StatusBadRequest, "invalid_cursor")
	ada.Get(collection(adaOrg)+"?sort=org_id").AssertProblem(t, http.StatusBadRequest, "invalid_sort")

	var filtered apiOrderPage
	ada.Get(collection(adaOrg)+"?status=cancelled").JSON(t, &filtered)
	if len(filtered.Items) != 1 || filtered.Items[0].Address != "Docs" {
		t.Errorf("list with status=cancelled = %+v, want Docs only", filtered.Items)
	}
}

func TestUpdateAndDeleteOrder(t *testing.T) {
	app := newApp(t)
	ada, adaID, adaOrg := signUp(t, app, "ada@example.com")
	var website apiOrder
	ada.Post(collection(adaOrg), map[string]any{"address": "Website", "note": "Example note"}).JSON(t, &website)
	item := collection(adaOrg) + "/" + website.ID

	ada.Patch(item, map[string]any{"address": "No version"}).AssertProblem(t, http.StatusUnprocessableEntity, "validation_failed")
	ada.Patch(item, map[string]any{"version": 2, "address": "Too new"}).AssertProblem(t, http.StatusConflict, "order_version_conflict")

	res := ada.Patch(item, map[string]any{"version": 1, "address": "Renamed", "status": "cancelled"})
	res.AssertStatus(t, http.StatusOK)
	var updated apiOrder
	res.JSON(t, &updated)
	if updated.Address != "Renamed" || updated.Status != "cancelled" || updated.Version != 2 {
		t.Errorf("updated = %+v, want the new values and version 2", updated)
	}
	// Changing an order with the version it already had is a conflict.
	ada.Patch(item, map[string]any{"version": 1, "address": "Again"}).AssertProblem(t, http.StatusConflict, "order_version_conflict")

	ada.Delete(item).AssertStatus(t, http.StatusNoContent)
	ada.Get(item).AssertProblem(t, http.StatusNotFound, "order_not_found")
	ada.Delete(item).AssertProblem(t, http.StatusNotFound, "order_not_found")

	// Every event names the member and the organisation.
	rows, err := app.App().Deps().DB.Query(context.Background(),
		`SELECT action, actor_id, coalesce(org_id, '') FROM audit_events WHERE resource_type = 'order' AND resource_id = $1 ORDER BY id`, website.ID)
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

// TestPurgingAnOrganisationDeletesItsOrders checks the migration's foreign key:
// the orgs_purge job deletes an organisation once its restore period ends,
// and its orders go with it.
func TestPurgingAnOrganisationDeletesItsOrders(t *testing.T) {
	app := newApp(t)
	ada, _, adaOrg := signUp(t, app, "ada@example.com")
	ada.Post(collection(adaOrg), map[string]any{"address": "Website", "note": "Example note"}).AssertStatus(t, http.StatusCreated)

	// Every organisation's rows, whether or not the app has row-level security.
	ctx := postgres.WithoutRowLevelSecurity(context.Background(), "test: purge")
	db := app.App().Deps().DB
	if _, err := db.Exec(ctx, `DELETE FROM orgs WHERE id = $1`, adaOrg); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM orders WHERE org_id = $1`, adaOrg).Scan(&left); err != nil || left != 0 {
		t.Errorf("orders left after purging the organisation = %d, %v; want 0", left, err)
	}
}
