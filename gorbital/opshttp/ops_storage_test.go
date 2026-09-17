package opshttp_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"gorbital.dev/gorbital/internal/opstest"
)

// This test is a v0.1 golden app's internal/app/ops_storage_test.go, run
// against the library module. devServer and devDo are in
// devoperator_test.go.

// devPut sends a raw PUT with the dev console token.
func devPut(t *testing.T, base, path, contentType, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, base+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+devToken)
	req.Header.Set("Content-Type", contentType)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(raw)
}

func TestOpsStorage(t *testing.T) {
	dir := t.TempDir()
	_, base := devServer(t, map[string]string{"STORAGE_LOCAL_DIR": dir})

	code, body := devDo(t, base, http.MethodGet, "/ops/storage", "")
	if code != http.StatusOK || !strings.Contains(body, `"driver":"local"`) || !strings.Contains(body, `"local":true`) || !strings.Contains(body, `"status":"ok"`) {
		t.Fatalf("status = %d %s", code, body)
	}

	// Upload, describe, download.
	code, body = devPut(t, base, "/ops/storage/object?key=docs/hello.txt", "text/plain", "hello world")
	if code != http.StatusCreated || !strings.Contains(body, `"size":11`) || !strings.Contains(body, `"content_type":"text/plain"`) {
		t.Fatalf("upload = %d %s", code, body)
	}
	if code, body := devDo(t, base, http.MethodGet, "/ops/storage/object?key=docs/hello.txt", ""); code != http.StatusOK || !strings.Contains(body, `"key":"docs/hello.txt"`) {
		t.Errorf("stat = %d %s", code, body)
	}
	if code, body := devDo(t, base, http.MethodGet, "/ops/storage/object/content?key=docs/hello.txt", ""); code != http.StatusOK || body != "hello world" {
		t.Errorf("download = %d %q", code, body)
	}
	if code, body := devDo(t, base, http.MethodGet, "/ops/storage/object?key=docs/nope.txt", ""); code != http.StatusNotFound || !strings.Contains(body, "storage_object_not_found") {
		t.Errorf("missing = %d %s", code, body)
	}
	if code, body := devPut(t, base, "/ops/storage/object?key=../etc/passwd", "text/plain", "x"); code != http.StatusUnprocessableEntity || !strings.Contains(body, "invalid_storage_key") {
		t.Errorf("bad key = %d %s", code, body)
	}

	// Directories and listing.
	if code, body := devDo(t, base, http.MethodPost, "/ops/storage/directories", `{"prefix":"images"}`); code != http.StatusCreated || !strings.Contains(body, `"prefix":"images/"`) {
		t.Errorf("mkdir = %d %s", code, body)
	}
	code, body = devDo(t, base, http.MethodGet, "/ops/storage/objects", "")
	if code != http.StatusOK || !strings.Contains(body, `"prefixes":["docs/","images/"]`) || !strings.Contains(body, `"objects":[]`) {
		t.Errorf("root list = %d %s", code, body)
	}
	code, body = devDo(t, base, http.MethodGet, "/ops/storage/objects?prefix=docs/", "")
	if code != http.StatusOK || !strings.Contains(body, `"key":"docs/hello.txt"`) {
		t.Errorf("docs list = %d %s", code, body)
	}

	// Move, signed URL, delete.
	if code, body := devDo(t, base, http.MethodPost, "/ops/storage/object/move", `{"from":"docs/hello.txt","to":"archive/hello.txt"}`); code != http.StatusOK || !strings.Contains(body, `"key":"archive/hello.txt"`) {
		t.Errorf("move = %d %s", code, body)
	}
	code, body = devDo(t, base, http.MethodPost, "/ops/storage/signed-url", `{"key":"archive/hello.txt","expiry_seconds":60}`)
	if code != http.StatusCreated || !strings.Contains(body, `"method":"GET"`) {
		t.Fatalf("signed url = %d %s", code, body)
	}
	var signed struct {
		URL string `json:"url"`
	}
	_ = json.Unmarshal([]byte(body), &signed)
	// The URL names the configured address; the test server listens
	// elsewhere.
	u, err := url.Parse(signed.URL)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.Get(base + u.RequestURI())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || string(raw) != "hello world" {
		t.Errorf("signed download = %d %q (%s)", res.StatusCode, raw, signed.URL)
	}
	if code, _ := devDo(t, base, http.MethodDelete, "/ops/storage/object?key=archive/hello.txt", ""); code != http.StatusNoContent {
		t.Errorf("delete = %d", code)
	}
	if code, body := devDo(t, base, http.MethodGet, "/ops/audit?actor_kind=system", ""); code != http.StatusOK || !strings.Contains(body, "storage.object.uploaded") || !strings.Contains(body, "storage.object.deleted") || !strings.Contains(body, "storage.signed_url.created") {
		t.Errorf("audit = %d %s", code, body)
	}

	// ops_viewer reads, but doesn't write.
	a, base2 := devServer(t, map[string]string{"STORAGE_LOCAL_DIR": dir})
	viewer, _ := a.SignIn(t, "viewer@example.com", "ops_viewer")
	h := a.Handler()
	if r := opstest.Do(t, h, http.MethodGet, "/ops/storage", "", viewer...); r.Code != http.StatusOK {
		t.Errorf("ops_viewer status = %d %s", r.Code, r.Body)
	}
	if r := opstest.Do(t, h, http.MethodDelete, "/ops/storage/object?key=x.txt", "", viewer...); r.Code != http.StatusForbidden {
		t.Errorf("ops_viewer delete = %d %s", r.Code, r.Body)
	}
	_ = base2
}
