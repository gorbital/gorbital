package orgshttp

import (
	"fmt"
	"net/http"
	"testing"
)

// flagState returns a PUT /ops/flags/{key} body.
func flagState(version int, reason, state string) string {
	return fmt.Sprintf(`{"version":%d,"reason":%q,"state":%s}`, version, reason, state)
}

// clientFlags returns the client flags path lists for the caller signed in
// with headers.
func clientFlags(t *testing.T, h http.Handler, path string, headers ...string) map[string]any {
	t.Helper()
	r := do(t, h, "GET", path, "", headers...)
	got, _ := r.json["flags"].(map[string]any)
	if r.code != http.StatusOK || got == nil || r.header.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("GET %s = %d %s (Cache-Control %q), want the caller's flags, not cached", path, r.code, r.body, r.header.Get("Cache-Control"))
	}
	return got
}

// withKey adds an Idempotency-Key header to headers.
func withKey(headers []string, key string) []string {
	return append([]string{"Idempotency-Key", key}, headers...)
}
