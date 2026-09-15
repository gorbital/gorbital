package openapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/httpx"
	"gorbital.dev/modules/openapi"
	"gorbital.dev/modules/openapi/reference"
)

var errNameTaken = errors.New("project name is already taken")

type createInput struct {
	Body struct {
		_        struct{} `json:"-" additionalProperties:"true"`
		Name     string   `json:"name" minLength:"3"`
		Password string   `json:"password" minLength:"12"`
	}
}

type createOutput struct {
	Body struct {
		Name string `json:"name"`
	}
}

func newApp(t *testing.T) (http.Handler, huma.API, *bytes.Buffer) {
	t.Helper()
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	mapper, err := httpx.NewMapper(logger, httpx.Mapping{Err: errNameTaken, Status: http.StatusConflict, Code: "project_name_taken"})
	if err != nil {
		t.Fatal(err)
	}
	openapi.InstallErrors(mapper)

	mux := http.NewServeMux()
	api := openapi.New(mux, "Test API", "0.1.0", openapi.WithBearerAuth("Session token"), openapi.WithDescription("Test"))
	// Docs are mounted before the operation is registered: the reference
	// reads the OpenAPI document when it is first requested.
	openapi.MountDocs(mux, openapi.DocsOptions{Title: "Test API"})
	huma.Register(api, huma.Operation{
		OperationID:   "create-project",
		Method:        http.MethodPost,
		Path:          "/v1/projects",
		Summary:       "Create a project",
		Security:      openapi.Bearer,
		DefaultStatus: http.StatusCreated,
		Errors:        []int{http.StatusConflict},
	}, func(_ context.Context, in *createInput) (*createOutput, error) {
		switch in.Body.Name {
		case "taken":
			return nil, errNameTaken
		case "boom":
			return nil, errors.New("pool exhausted at 10.0.0.9")
		}
		out := &createOutput{}
		out.Body.Name = in.Body.Name
		return out, nil
	})
	return httpx.Chain(mux, httpx.RequestID()), api, logs
}

func post(t *testing.T, h http.Handler, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest("POST", "/v1/projects", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var m map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	return rec, m
}

func TestErrorsAndValidation(t *testing.T) {
	h, _, logs := newApp(t)
	const pw = "correct-horse-battery"

	rec, body := post(t, h, `{"name":"web","password":"`+pw+`","client_version":"2.1"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("valid request with unknown field = %d %v, want 201 (tolerant)", rec.Code, body)
	}

	rec, body = post(t, h, `{"name":"taken","password":"`+pw+`"}`)
	if rec.Code != http.StatusConflict || body["code"] != "project_name_taken" || body["request_id"] == nil {
		t.Errorf("mapped domain error = %d %v, want 409 project_name_taken with request_id", rec.Code, body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != httpx.ProblemContentType {
		t.Errorf("mapped error Content-Type = %q, want %s", ct, httpx.ProblemContentType)
	}

	rec, body = post(t, h, `{"name":"x","password":"short-secret"}`)
	fieldErrs, _ := body["errors"].([]any)
	if rec.Code != http.StatusUnprocessableEntity || body["code"] != "validation_failed" || len(fieldErrs) != 1 {
		t.Errorf("validation error = %d %v, want 422 validation_failed with 1 field error", rec.Code, body)
	}
	rec, _ = post(t, h, `{"name":"web","password":"tiny-pass"}`)
	if strings.Contains(rec.Body.String(), "tiny-pass") {
		t.Errorf("validation response echoes the submitted password: %s", rec.Body.String())
	}

	rec, body = post(t, h, `{"name":"boom","password":"`+pw+`"}`)
	if rec.Code != http.StatusInternalServerError || body["code"] != "internal_error" || strings.Contains(rec.Body.String(), "10.0.0.9") {
		t.Errorf("unmapped error = %d %v, want generic 500", rec.Code, body)
	}
	if !strings.Contains(logs.String(), "10.0.0.9") {
		t.Errorf("unmapped error was not logged: %s", logs.String())
	}
}

func TestSpec(t *testing.T) {
	h, api, _ := newApp(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/openapi.json", nil))
	spec := rec.Body.String()
	for _, want := range []string{`"/v1/projects"`, `"bearer"`, `"request_id"`, `"code"`} {
		if !strings.Contains(spec, want) {
			t.Errorf("GET /openapi.json missing %s", want)
		}
	}
	for _, path := range []string{"/docs/openapi.json", "/schemas/Problem.json"} {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", path, nil))
		if r.Code == http.StatusOK {
			t.Errorf("GET %s = 200, want disabled", path)
		}
	}

	var buf bytes.Buffer
	if err := openapi.WriteSpec(&buf, api); err != nil {
		t.Fatalf("WriteSpec() error = %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil || doc["openapi"] == nil {
		t.Errorf("WriteSpec() output is not an OpenAPI document: %v", err)
	}
}

func TestDocs(t *testing.T) {
	h, _, _ := newApp(t)
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		return rec
	}

	rec := get("/docs")
	page := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(page, "Test API") || !strings.Contains(page, `href="/docs/other/create-project"`) {
		t.Fatalf("GET /docs = %d, want the overview listing the operation registered after MountDocs:\n%s", rec.Code, page)
	}
	if csp := rec.Header().Get("Content-Security-Policy"); csp != reference.ContentSecurityPolicy || strings.Contains(page, "https://") {
		t.Errorf("docs CSP %q; the page must load nothing from other origins", csp)
	}

	rec = get("/docs/other/create-project")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Create a project") || !strings.Contains(rec.Body.String(), "/v1/projects") {
		t.Errorf("GET /docs/other/create-project = %d", rec.Code)
	}

	css := regexp.MustCompile(`href="(/docs/assets/reference\.[0-9a-f]+\.css)"`).FindStringSubmatch(page)
	if css == nil {
		t.Fatal("the docs page doesn't link its stylesheet")
	}
	if rec = get(css[1]); rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/css") {
		t.Errorf("GET %s = %d", css[1], rec.Code)
	}
	if rec = get("/docs/assets/fonts/manrope-latin.woff2"); rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "font/woff2" {
		t.Errorf("GET font = %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec = get("/docs/nope"); rec.Code != http.StatusNotFound {
		t.Errorf("GET /docs/nope = %d, want 404", rec.Code)
	}
}
