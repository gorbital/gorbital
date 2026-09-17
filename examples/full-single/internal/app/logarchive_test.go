package app_test

import (
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLogArchive turns logs.archive.enabled on, makes a request the access
// log records, and finds the partial hour in file storage after the app
// shuts down (ADR-0079).
func TestLogArchive(t *testing.T) {
	storageDir, archiveDir := t.TempDir(), filepath.Join(t.TempDir(), "logs")
	a, base := devServer(t, map[string]string{"STORAGE_LOCAL_DIR": storageDir, "LOG_ARCHIVE_DIR": archiveDir})

	// Off by default: the request is logged, nothing is collected.
	if code, _ := devDo(t, base, http.MethodGet, "/v1/ping", ""); code != http.StatusOK {
		t.Fatalf("GET /v1/ping = %d", code)
	}
	if _, err := os.Stat(archiveDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("archive directory exists while the setting is off (err %v)", err)
	}
	if code, body := devDo(t, base, http.MethodGet, "/ops/settings/logs.archive.enabled", ""); code != http.StatusOK || !strings.Contains(body, `"value":false`) || !strings.Contains(body, `"reason_required":true`) {
		t.Fatalf("GET setting = %d %s", code, body)
	}

	// On: the next records go to the current hour's file.
	if code, body := devDo(t, base, http.MethodPut, "/ops/settings/logs.archive.enabled", `{"value":true,"version":0,"reason":"keep the logs"}`); code != http.StatusOK {
		t.Fatalf("PUT setting = %d %s", code, body)
	}
	if code, _ := devDo(t, base, http.MethodGet, "/v1/ping?archived=1", ""); code != http.StatusOK {
		t.Fatalf("GET /v1/ping = %d", code)
	}
	spools, err := filepath.Glob(filepath.Join(archiveDir, "*.jsonl"))
	if err != nil || len(spools) != 1 {
		t.Fatalf("spool files = %v, %v; want one", spools, err)
	}

	// Shutting down stores the partial hour and removes the spool.
	if err := a.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	objects, err := filepath.Glob(filepath.Join(storageDir, "objects", "logs", "acme-api", "*", "*", "*", "*.partial-*.jsonl.gz"))
	if err != nil || len(objects) != 1 {
		t.Fatalf("archived objects = %v, %v; want one partial hour", objects, err)
	}
	if left, _ := filepath.Glob(filepath.Join(archiveDir, "*")); len(left) != 0 {
		t.Errorf("files left in %s: %v", archiveDir, left)
	}
	f, err := os.Open(objects[0])
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("%s is not gzip: %v", objects[0], err)
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	var request string
	for _, l := range lines {
		if strings.Contains(l, `"path":"/v1/ping"`) {
			request = l
		}
	}
	if request == "" {
		t.Fatalf("no request record among %d lines: %s", len(lines), raw)
	}
	for _, want := range []string{`"msg":"http request"`, `"service":"acme-api"`, `"source":"http"`, `"status":200`, `"request_id":`} {
		if !strings.Contains(request, want) {
			t.Errorf("request record %s lacks %s", request, want)
		}
	}
	if strings.Contains(string(raw), `"query":"archived=1"`) || strings.Count(string(raw), `"path":"/v1/ping"`) != 1 {
		t.Errorf("archive = %s, want the one request logged while on and no query string", raw)
	}
	meta, err := os.ReadFile(strings.Replace(objects[0], string(filepath.Separator)+"objects"+string(filepath.Separator), string(filepath.Separator)+"meta"+string(filepath.Separator), 1) + ".json")
	if err != nil || !strings.Contains(string(meta), "application/gzip") {
		t.Errorf("metadata = %s, %v; want application/gzip", meta, err)
	}
}
