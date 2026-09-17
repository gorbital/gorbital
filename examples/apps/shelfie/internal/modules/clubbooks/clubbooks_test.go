package clubbooks_test

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

	"example.com/shelfie/db/migrations"
	"example.com/shelfie/internal/modules/clubbooks"
	"example.com/shelfie/internal/modules/clubbooks/usecase"
)

// These tests drive the club books routes through the app's real middleware
// stack, on a new database per test (gorbitaltest), with real accounts:
// organisation members are sign-in's users.

// newApp builds the app with sign-in, organisations and the club books
// module for the test.
func newApp(t *testing.T) *gorbitaltest.App {
	t.Helper()
	auth := authhttp.New()
	return gorbitaltest.New(t,
		gorbital.WithAuth(auth),
		gorbital.WithModules(orgshttp.Module(auth), clubbooks.Module()),
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

// collection is the path of the organisation orgID's club books.
func collection(orgID string) string { return "/v1/orgs/" + orgID + "/club-books" }

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

// apiClubBook is a club book as the API returns it.
type apiClubBook struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Author    string `json:"author"`
	Status    string `json:"status"`
	Note      string `json:"note"`
	CreatedBy string `json:"created_by"`
	Version   int64  `json:"version"`
}

// apiClubBookPage is a page of club books.
type apiClubBookPage struct {
	Items      []apiClubBook `json:"items"`
	NextCursor string        `json:"next_cursor"`
}

func TestCreateAndGetClubBook(t *testing.T) {
	app := newApp(t)
	ada, adaID, adaOrg := signUp(t, app, "ada@example.com")

	res := ada.Post(collection(adaOrg), map[string]any{"title": " Website ", "author": "Example author", "note": "Example note"})
	res.AssertStatus(t, http.StatusCreated)
	var created apiClubBook
	res.JSON(t, &created)
	if !strings.HasPrefix(created.ID, "clb_") || created.Title != "Website" || created.Status != "proposed" || created.CreatedBy != adaID || created.Version != 1 {
		t.Errorf("created = %+v, want a clb_ ID, the title trimmed, the default status, created by Ada and version 1", created)
	}

	var got apiClubBook
	ada.Get(collection(adaOrg)+"/"+created.ID).JSON(t, &got)
	if got != created {
		t.Errorf("GET = %+v, want %+v", got, created)
	}
}

func TestClubBookRules(t *testing.T) {
	app := newApp(t)
	ada, _, adaOrg := signUp(t, app, "ada@example.com")
	ada.Post(collection(adaOrg), map[string]any{"title": "Website", "author": "Example author", "note": "Example note"}).AssertStatus(t, http.StatusCreated)

	for _, tt := range []struct {
		name   string
		body   map[string]any
		status int
		code   string
	}{
		{"blank title", map[string]any{"title": "   ", "author": "Example author", "note": "Example note"}, http.StatusUnprocessableEntity, "validation_failed"},
		{"missing title", map[string]any{}, http.StatusUnprocessableEntity, "validation_failed"},
		{"unknown status", map[string]any{"title": "Docs", "author": "Example author", "note": "Example note", "status": "?"}, http.StatusUnprocessableEntity, "validation_failed"},
		{"title already taken, ignoring case", map[string]any{"title": "WEBSITE", "author": "Example author", "note": "Example note"}, http.StatusConflict, "club_book_title_taken"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ada.Post(collection(adaOrg), tt.body).AssertProblem(t, tt.status, tt.code)
		})
	}
	// Another organisation can use the same title.
	bob, _, bobOrg := signUp(t, app, "bob@example.com")
	bob.Post(collection(bobOrg), map[string]any{"title": "Website", "author": "Example author", "note": "Example note"}).AssertStatus(t, http.StatusCreated)
}

// TestClubBooksAreProtected checks deny by default, membership, organisation
// isolation and API key scopes: an organisation someone isn't a member of
// doesn't exist for them, and another organisation's club book isn't in theirs.
func TestClubBooksAreProtected(t *testing.T) {
	app := newApp(t)
	ada, _, adaOrg := signUp(t, app, "ada@example.com")
	bob, _, bobOrg := signUp(t, app, "bob@example.com")
	var website apiClubBook
	ada.Post(collection(adaOrg), map[string]any{"title": "Website", "author": "Example author", "note": "Example note"}).JSON(t, &website)
	item := collection(adaOrg) + "/" + website.ID

	app.Client().Get(collection(adaOrg)).AssertProblem(t, http.StatusUnauthorized, "unauthenticated")

	for name, res := range map[string]*gorbitaltest.Response{
		"list":                      bob.Get(collection(adaOrg)),
		"create":                    bob.Post(collection(adaOrg), map[string]any{"title": "Mine now", "author": "Example author", "note": "Example note"}),
		"get":                       bob.Get(item),
		"update":                    bob.Patch(item, map[string]any{"version": 1, "title": "Mine now"}),
		"delete":                    bob.Delete(item),
		"list in an unknown org":    bob.Get(collection("org_mfrggzdfmztwq2lkmfrggzdfmy")),
		"list with a malformed org": bob.Get(collection("not-an-org")),
	} {
		t.Run("non-member "+name, func(t *testing.T) {
			res.AssertProblem(t, http.StatusNotFound, "org_not_found")
		})
	}

	bobItem := collection(bobOrg) + "/" + website.ID
	bob.Get(bobItem).AssertProblem(t, http.StatusNotFound, "club_book_not_found")
	bob.Patch(bobItem, map[string]any{"version": 1, "title": "Mine now"}).AssertProblem(t, http.StatusNotFound, "club_book_not_found")
	bob.Delete(bobItem).AssertProblem(t, http.StatusNotFound, "club_book_not_found")
	var bobs apiClubBookPage
	bob.Get(collection(bobOrg)).JSON(t, &bobs)
	if len(bobs.Items) != 0 {
		t.Errorf("another organisation's list = %+v, want none", bobs.Items)
	}

	readOnly := app.Client().WithHeader("Authorization", "Bearer "+apiKey(t, ada, usecase.PermRead))
	readOnly.Get(item).AssertStatus(t, http.StatusOK)
	readOnly.Post(collection(adaOrg), map[string]any{"title": "By a key", "author": "Example author", "note": "Example note"}).AssertProblem(t, http.StatusForbidden, "forbidden")
	readOnly.Patch(item, map[string]any{"version": 1, "title": "By a key"}).AssertProblem(t, http.StatusForbidden, "forbidden")
	readOnly.Delete(item).AssertProblem(t, http.StatusForbidden, "forbidden")
}

func TestListClubBooksPages(t *testing.T) {
	app := newApp(t)
	ada, _, adaOrg := signUp(t, app, "ada@example.com")
	ada.Post(collection(adaOrg), map[string]any{"title": "Website", "author": "Example author", "note": "Example note"}).AssertStatus(t, http.StatusCreated)
	ada.Post(collection(adaOrg), map[string]any{"title": "Docs", "author": "Example author", "note": "Example note", "status": "finished"}).AssertStatus(t, http.StatusCreated)

	var titles []string
	cursor := ""
	for range 3 {
		var p apiClubBookPage
		ada.Get(collection(adaOrg)+"?limit=1&sort=title&cursor="+url.QueryEscape(cursor)).JSON(t, &p)
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

	var first apiClubBookPage
	ada.Get(collection(adaOrg)+"?limit=1").JSON(t, &first)
	ada.Get(collection(adaOrg)+"?sort=title&cursor="+url.QueryEscape(first.NextCursor)).AssertProblem(t, http.StatusBadRequest, "invalid_cursor")
	ada.Get(collection(adaOrg)+"?sort=org_id").AssertProblem(t, http.StatusBadRequest, "invalid_sort")

	var filtered apiClubBookPage
	ada.Get(collection(adaOrg)+"?status=finished").JSON(t, &filtered)
	if len(filtered.Items) != 1 || filtered.Items[0].Title != "Docs" {
		t.Errorf("list with status=finished = %+v, want Docs only", filtered.Items)
	}
}

func TestUpdateAndDeleteClubBook(t *testing.T) {
	app := newApp(t)
	ada, adaID, adaOrg := signUp(t, app, "ada@example.com")
	var website apiClubBook
	ada.Post(collection(adaOrg), map[string]any{"title": "Website", "author": "Example author", "note": "Example note"}).JSON(t, &website)
	item := collection(adaOrg) + "/" + website.ID

	ada.Patch(item, map[string]any{"title": "No version"}).AssertProblem(t, http.StatusUnprocessableEntity, "validation_failed")
	ada.Patch(item, map[string]any{"version": 2, "title": "Too new"}).AssertProblem(t, http.StatusConflict, "club_book_version_conflict")

	res := ada.Patch(item, map[string]any{"version": 1, "title": "Renamed", "status": "finished"})
	res.AssertStatus(t, http.StatusOK)
	var updated apiClubBook
	res.JSON(t, &updated)
	if updated.Title != "Renamed" || updated.Status != "finished" || updated.Version != 2 {
		t.Errorf("updated = %+v, want the new values and version 2", updated)
	}
	// Changing a club book with the version it already had is a conflict.
	ada.Patch(item, map[string]any{"version": 1, "title": "Again"}).AssertProblem(t, http.StatusConflict, "club_book_version_conflict")

	ada.Delete(item).AssertStatus(t, http.StatusNoContent)
	ada.Get(item).AssertProblem(t, http.StatusNotFound, "club_book_not_found")
	ada.Delete(item).AssertProblem(t, http.StatusNotFound, "club_book_not_found")

	// Every event names the member and the organisation.
	rows, err := app.App().Deps().DB.Query(context.Background(),
		`SELECT action, actor_id, coalesce(org_id, '') FROM audit_events WHERE resource_type = 'club_book' AND resource_id = $1 ORDER BY id`, website.ID)
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

// TestPurgingAnOrganisationDeletesItsClubBooks checks the migration's foreign key:
// the orgs_purge job deletes an organisation once its restore period ends,
// and its club books go with it.
func TestPurgingAnOrganisationDeletesItsClubBooks(t *testing.T) {
	app := newApp(t)
	ada, _, adaOrg := signUp(t, app, "ada@example.com")
	ada.Post(collection(adaOrg), map[string]any{"title": "Website", "author": "Example author", "note": "Example note"}).AssertStatus(t, http.StatusCreated)

	// Every organisation's rows, whether or not the app has row-level security.
	ctx := postgres.WithoutRowLevelSecurity(context.Background(), "test: purge")
	db := app.App().Deps().DB
	if _, err := db.Exec(ctx, `DELETE FROM orgs WHERE id = $1`, adaOrg); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM club_books WHERE org_id = $1`, adaOrg).Scan(&left); err != nil || left != 0 {
		t.Errorf("club books left after purging the organisation = %d, %v; want 0", left, err)
	}
}
