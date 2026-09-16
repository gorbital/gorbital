package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"gorbital.dev/actor"
	"gorbital.dev/modules/auth"
)

// Middleware makes the client available to every audit event recorded for
// the request, not only auth's own (security review OPS-7).
func TestMiddlewareSetsTheActorClient(t *testing.T) {
	var got actor.Client
	h := auth.Middleware(fakeAuthenticator{})(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, _ = actor.ClientFrom(r.Context())
	}))
	req := httptest.NewRequest(http.MethodPut, "/ops/settings/x", nil)
	req.RemoteAddr = "[2001:db8::1%eth0]:5000"
	req.Header.Set("User-Agent", "ops-cli/1")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if got.IP != "2001:db8::1" || got.UserAgent != "ops-cli/1" {
		t.Errorf("actor.ClientFrom() IP, UserAgent = %q, %q; want the cleaned client", got.IP, got.UserAgent)
	}
}
