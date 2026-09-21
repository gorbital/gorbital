package signintest

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"
)

// FuzzCheckResultURL: whatever is accepted is an http(s) URL on a loopback
// name, without user information or fragment, and a redirect built from it
// stays there.
func FuzzCheckResultURL(f *testing.F) {
	for _, s := range []string{portalResult, "http://[::1]:3100/x", "https://evil.example/", "http://127.0.0.1:3100/#x", "//127.0.0.1/", "http://localhost@evil/"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		got, err := CheckResultURL(raw)
		if err != nil {
			return
		}
		u, perr := url.Parse(resultRedirect(got, "slt_x", "google"))
		if perr != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
			t.Fatalf("accepted %q -> %q (%v)", raw, got, perr)
		}
		switch strings.ToLower(u.Hostname()) {
		case "localhost", "127.0.0.1", "::1":
		default:
			t.Fatalf("accepted %q on host %q", raw, u.Hostname())
		}
	})
}

// FuzzRedact: no JWT-shaped or long base64 run survives, secrets are gone,
// and the message stays bounded and valid UTF-8 when the input is.
func FuzzRedact(f *testing.F) {
	f.Add("oauth2: \"invalid_grant\" \"Bad Request\"", "GOCSPX-secret")
	f.Add("eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiIxIn0.c2lnbmF0dXJlLXZhbHVl", "x")
	f.Fuzz(func(t *testing.T, msg, secret string) {
		out := redact(msg, secret)
		if len(out) > 504 {
			t.Fatalf("len %d", len(out))
		}
		if jwtLike.MatchString(out) || longToken.MatchString(out) {
			t.Fatalf("token-like text survived: %q", out)
		}
		plain := len(secret) >= 6 && !strings.Contains("[redacted]", secret) && strings.Trim(secret, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-") == ""
		if plain && strings.IndexFunc(msg, func(r rune) bool { return r < ' ' || r == 0x7f }) < 0 && strings.Contains(out, secret) {
			t.Fatalf("secret %q survived in %q", secret, out)
		}
		if utf8.ValidString(msg) && !utf8.ValidString(out) {
			t.Fatalf("invalid UTF-8 from valid input: %q", out)
		}
	})
}

// FuzzCallback: any provider return, GET or Apple's form post, is either a
// known test's (there is none here) or passed to sign-in with its body
// intact; nothing panics.
func FuzzCallback(f *testing.F) {
	f.Add("state=abc&code=def", true)
	f.Add("state="+strings.Repeat("a", 300), false)
	f.Add("%zz&state=", true)
	tester := New(Config{})
	f.Fuzz(func(t *testing.T, query string, post bool) {
		var body string
		h := tester.Callbacks(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			data, _ := io.ReadAll(r.Body)
			body = string(data)
			w.WriteHeader(http.StatusTeapot)
		}))
		var req *http.Request
		if post {
			req = httptest.NewRequest(http.MethodPost, "/v1/auth/apple/callback", strings.NewReader(query))
		} else {
			req = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/v1/auth/google/callback", nil)
			req.URL.RawQuery = query
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusTeapot {
			t.Fatalf("a request with no test state wasn't passed on: %d", rec.Code)
		}
		if post && body != query {
			t.Fatalf("Apple's body changed: %q -> %q", query, body)
		}
	})
}
