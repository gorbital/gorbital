package app_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"apistock.dev/spikes/openapi/internal/app"
)

type result struct {
	status      int
	contentType string
	body        map[string]any
}

func call(t *testing.T, h http.Handler, method, path, token, body string) result {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	res := result{status: rec.Code, contentType: rec.Header().Get("Content-Type")}
	raw, _ := io.ReadAll(rec.Body)
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &res.body); err != nil {
			t.Fatalf("%s %s: invalid JSON %q: %v", method, path, raw, err)
		}
	}
	return res
}

func TestProjectsHTTP(t *testing.T) {
	h, _ := app.New(slog.New(slog.DiscardHandler))

	created := call(t, h, "POST", "/v1/orgs/org_acme/projects", "alice", `{"name":"Website"}`)
	if created.status != http.StatusCreated {
		t.Fatalf("create = %d %v, want 201", created.status, created.body)
	}
	id, _ := created.body["id"].(string)

	tests := []struct {
		name       string
		method     string
		path       string
		token      string
		body       string
		wantStatus int
		wantCode   string
	}{
		{"duplicate name", "POST", "/v1/orgs/org_acme/projects", "alice", `{"name":"Website"}`, 409, "project_name_taken"},
		{"schema validation", "POST", "/v1/orgs/org_acme/projects", "alice", `{"name":""}`, 422, "validation_failed"},
		{"domain validation", "POST", "/v1/orgs/org_acme/projects", "alice", `{"name":"   "}`, 422, "project_name_required"},
		{"no token", "GET", "/v1/orgs/org_acme/projects", "", "", 401, "unauthorized"},
		{"not a member", "GET", "/v1/orgs/org_acme/projects", "mallory", "", 403, "forbidden"},
		{"read-only member cannot create", "POST", "/v1/orgs/org_acme/projects", "bob", `{"name":"Other"}`, 403, "forbidden"},
		{"cross-org id lookup", "GET", "/v1/orgs/org_globex/projects/" + id, "bob", "", 404, "project_not_found"},
		{"archive", "POST", "/v1/orgs/org_acme/projects/" + id + "/archive", "alice", "", 200, ""},
		{"archive twice", "POST", "/v1/orgs/org_acme/projects/" + id + "/archive", "alice", "", 409, "project_already_archived"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := call(t, h, tt.method, tt.path, tt.token, tt.body)
			if got.status != tt.wantStatus {
				t.Fatalf("%s %s = %d %v, want %d", tt.method, tt.path, got.status, got.body, tt.wantStatus)
			}
			if tt.wantCode == "" {
				return
			}
			if code := got.body["code"]; code != tt.wantCode {
				t.Errorf("%s %s code = %v, want %s (body %v)", tt.method, tt.path, code, tt.wantCode, got.body)
			}
			if got.contentType != "application/problem+json" {
				t.Errorf("%s %s content type = %q, want application/problem+json", tt.method, tt.path, got.contentType)
			}
			if rid, _ := got.body["request_id"].(string); !strings.HasPrefix(rid, "req_") {
				t.Errorf("%s %s request_id = %q, want req_ prefix", tt.method, tt.path, rid)
			}
		})
	}

	list := call(t, h, "GET", "/v1/orgs/org_acme/projects", "bob", "")
	if items, _ := list.body["items"].([]any); list.status != 200 || len(items) != 1 {
		t.Errorf("list as read-only member = %d %v, want 200 with 1 item", list.status, list.body)
	}
	if _, ok := list.body["$schema"]; ok {
		t.Errorf("list body contains $schema link, want clean response: %v", list.body)
	}
}

func TestOpenAPIDocument(t *testing.T) {
	h, _ := app.New(slog.New(slog.DiscardHandler))
	spec := call(t, h, "GET", "/openapi.json", "", "")
	if spec.status != 200 {
		t.Fatalf("GET /openapi.json = %d, want 200", spec.status)
	}
	raw, _ := json.Marshal(spec.body)
	doc := string(raw)
	for _, want := range []string{
		`"/v1/orgs/{orgId}/projects/{id}/archive"`,
		`"bearer"`,
		`"project_name_taken"`, // Problem example documented
		`"request_id"`,
		`"archive-project"`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("openapi.json missing %s", want)
		}
	}

	docs := httptest.NewRecorder()
	h.ServeHTTP(docs, httptest.NewRequest("GET", "/docs", nil))
	if docs.Code != 200 || !strings.Contains(docs.Body.String(), "api-reference") {
		t.Errorf("GET /docs = %d, want Scalar page", docs.Code)
	}
}
