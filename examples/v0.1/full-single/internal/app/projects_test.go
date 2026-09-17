package app_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestProjectsEndToEnd drives the projects API over HTTP: create,
// validation, owner isolation, pagination, versioned updates, delete and the
// audit trail.
func TestProjectsEndToEnd(t *testing.T) {
	a, dbURL := newAppWithURL(t, nil)
	h := a.Handler()
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	ada, adaID := signIn(t, a, "ada@example.com", "")
	bob, _ := signIn(t, a, "bob@example.com", "")
	collection := "/v1/projects"

	if r := do(t, h, "POST", collection, `{"name":"Website","description":"Example description"}`); r.code != http.StatusUnauthorized || r.json["code"] != "unauthenticated" {
		t.Errorf("create without signing in = %d %s", r.code, r.body)
	}

	created := do(t, h, "POST", collection, `{"name":" Website ","description":"Example description"}`, ada...)
	if created.code != http.StatusCreated || created.json["name"] != "Website" || created.json["status"] != "active" ||
		created.json["version"] != float64(1) {
		t.Fatalf("create = %d %s", created.code, created.body)
	}
	id, _ := created.json["id"].(string)
	if !strings.HasPrefix(id, "prj_") {
		t.Errorf("id = %q, want prefix prj_", id)
	}
	item := collection + "/" + id

	if r := do(t, h, "POST", collection, `{"name":"WEBSITE","description":"Example description"}`, ada...); r.code != http.StatusConflict || r.json["code"] != "project_name_taken" {
		t.Errorf("create with a taken name = %d %s", r.code, r.body)
	}
	invalid := do(t, h, "POST", collection, `{"name":"   ","description":"Example description"}`, ada...)
	fieldErrors, _ := invalid.json["errors"].([]any)
	if invalid.code != http.StatusUnprocessableEntity || invalid.json["code"] != "validation_failed" || len(fieldErrors) != 1 ||
		fieldErrors[0].(map[string]any)["location"] != "body.name" {
		t.Errorf("create with a blank name = %d %s", invalid.code, invalid.body)
	}

	// An API key reaches the projects only within its scopes (ADR-0058).
	expires := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	readKey, _ := createKey(t, h, "/v1/auth/api-keys", fmt.Sprintf(`{"name":"reader","expires_at":%q,"password":%q,"scopes":[%q]}`, expires, testPassword, "projects.project.read"), ada)
	reader := []string{"Authorization", "Bearer " + readKey}
	for _, path := range []string{collection, item} {
		if r := do(t, h, "GET", path, "", reader...); r.code != http.StatusOK {
			t.Errorf("GET %s with a read-only key = %d %s, want 200", path, r.code, r.body)
		}
	}
	for _, r := range []response{
		do(t, h, "POST", collection, `{"name":"By a key","description":"Example description"}`, reader...),
		do(t, h, "PATCH", item, `{"version":1,"name":"By a key"}`, reader...),
		do(t, h, "DELETE", item, "", reader...),
	} {
		if r.code != http.StatusForbidden || r.json["code"] != "forbidden" {
			t.Errorf("change with a read-only key = %d %s, want 403 forbidden", r.code, r.body)
		}
	}

	// Someone else's project doesn't exist for them.
	for _, r := range []response{
		do(t, h, "GET", item, "", bob...),
		do(t, h, "PATCH", item, `{"version":1,"name":"Mine now"}`, bob...),
		do(t, h, "DELETE", item, "", bob...),
	} {
		if r.code != http.StatusNotFound || r.json["code"] != "project_not_found" {
			t.Errorf("another user's request = %d %s, want 404 project_not_found", r.code, r.body)
		}
	}
	if r := do(t, h, "GET", collection, "", bob...); r.code != http.StatusOK || len(r.json["items"].([]any)) != 0 {
		t.Errorf("another user's list = %d %s, want no items", r.code, r.body)
	}

	if r := do(t, h, "POST", collection, `{"name":"Docs","description":"Example description","status":"archived"}`, ada...); r.code != http.StatusCreated {
		t.Fatalf("create Docs = %d %s", r.code, r.body)
	}
	var titles []string
	cursor := ""
	for range 3 {
		r := do(t, h, "GET", collection+"?limit=1&sort=name&cursor="+url.QueryEscape(cursor), "", ada...)
		if r.code != http.StatusOK {
			t.Fatalf("list = %d %s", r.code, r.body)
		}
		for _, entry := range r.json["items"].([]any) {
			titles = append(titles, entry.(map[string]any)["name"].(string))
		}
		if cursor, _ = r.json["next_cursor"].(string); cursor == "" {
			break
		}
	}
	if want := []string{"Docs", "Website"}; !slices.Equal(titles, want) {
		t.Errorf("pages sorted by name = %v, want %v", titles, want)
	}
	first := do(t, h, "GET", collection+"?limit=1", "", ada...)
	for query, want := range map[string]string{
		"sort=name&cursor=" + url.QueryEscape(first.json["next_cursor"].(string)): "invalid_cursor",
		"sort=owner_id": "invalid_sort",
	} {
		if r := do(t, h, "GET", collection+"?"+query, "", ada...); r.code != http.StatusBadRequest || r.json["code"] != want {
			t.Errorf("list ?%s = %d %s, want 400 %s", query, r.code, r.body, want)
		}
	}
	if r := do(t, h, "GET", collection+"?status=archived", "", ada...); r.code != http.StatusOK || len(r.json["items"].([]any)) != 1 {
		t.Errorf("list with status=archived = %d %s, want Docs only", r.code, r.body)
	}

	if r := do(t, h, "PATCH", item, `{"name":"No version"}`, ada...); r.code != http.StatusUnprocessableEntity {
		t.Errorf("update without a version = %d %s, want 422", r.code, r.body)
	}
	if r := do(t, h, "PATCH", item, `{"version":2,"name":"Too new"}`, ada...); r.code != http.StatusConflict || r.json["code"] != "project_version_conflict" {
		t.Errorf("update with a stale version = %d %s", r.code, r.body)
	}
	updated := do(t, h, "PATCH", item, `{"version":1,"status":"archived"}`, ada...)
	if updated.code != http.StatusOK || updated.json["status"] != "archived" || updated.json["name"] != "Website" ||
		updated.json["version"] != float64(2) {
		t.Errorf("update = %d %s", updated.code, updated.body)
	}

	if r := do(t, h, "DELETE", item, "", ada...); r.code != http.StatusNoContent {
		t.Fatalf("delete = %d %s", r.code, r.body)
	}
	if r := do(t, h, "GET", item, "", ada...); r.code != http.StatusNotFound {
		t.Errorf("get after delete = %d %s", r.code, r.body)
	}

	rows, err := pool.Query(context.Background(), `SELECT action, actor_id FROM audit_events WHERE resource_type = 'project' AND resource_id = $1 ORDER BY id`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var trail []string
	for rows.Next() {
		var action, actorID string
		if err := rows.Scan(&action, &actorID); err != nil {
			t.Fatal(err)
		}
		trail = append(trail, action+" by "+strings.ReplaceAll(actorID, adaID, "ada"))
	}
	want := []string{"projects.project.created by ada", "projects.project.updated by ada", "projects.project.deleted by ada"}
	if !slices.Equal(trail, want) {
		t.Errorf("audit trail = %v, want %v", trail, want)
	}
}
