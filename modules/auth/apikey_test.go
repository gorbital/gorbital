package auth_test

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"gorbital.dev/actor"
	"gorbital.dev/modules/auth"
)

func TestNewAPIKey(t *testing.T) {
	seen := map[string]bool{}
	for range 200 {
		key, lookupID, hash := auth.NewAPIKey()
		if len(key) != auth.APIKeyLength || !strings.HasPrefix(key, "gbk_"+lookupID+"_") || len(lookupID) != 26 {
			t.Fatalf("NewAPIKey() = %q, lookup %q; want gbk_<26 characters>_<52 characters>", key, lookupID)
		}
		if got, err := auth.ParseAPIKey(key); err != nil || got != lookupID {
			t.Fatalf("ParseAPIKey(NewAPIKey()) = %q, %v; want %q", got, err, lookupID)
		}
		if !auth.APIKeyMatches(key, hash) || string(hash) != string(auth.HashAPIKey(key)) {
			t.Fatalf("NewAPIKey() hash doesn't match its key")
		}
		if strings.Contains(string(hash), lookupID) || seen[key] || seen[lookupID] {
			t.Fatalf("NewAPIKey() repeated or leaked a part: %q", key)
		}
		seen[key], seen[lookupID] = true, true
	}
}

func TestParseAPIKey(t *testing.T) {
	key, lookupID, _ := auth.NewAPIKey()
	secret := key[len("gbk_")+27:]
	for name, bad := range map[string]string{
		"empty":                  "",
		"prefix only":            "gbk_",
		"session token":          "c2Vzc2lvbi10b2tlbi13aXRoLTMyLWJ5dGVzLW9mLXJhbmRvbW5lc3M",
		"other prefix":           "gbx_" + key[4:],
		"uppercase prefix":       "GBK_" + key[4:],
		"uppercase key":          "gbk_" + strings.ToUpper(key[4:]),
		"one character short":    key[:len(key)-1],
		"one character long":     key + "a",
		"separator moved":        "gbk_" + lookupID[:25] + "_" + lookupID[25:] + secret,
		"separator missing":      "gbk_" + lookupID + "a" + secret,
		"second separator":       "gbk_" + lookupID + "_" + secret[:51] + "_",
		"digit outside base32":   "gbk_" + lookupID + "_" + secret[:51] + "1",
		"padding":                "gbk_" + lookupID + "_" + secret[:51] + "=",
		"base64url character":    "gbk_" + lookupID[:25] + "-" + "_" + secret,
		"space inside":           "gbk_" + lookupID + "_" + secret[:51] + " ",
		"leading space":          " " + key[:len(key)-1],
		"non-ASCII lookalike":    "gbk_" + lookupID + "_" + secret[:50] + "а", // Cyrillic a, two bytes
		"NUL byte":               "gbk_" + lookupID + "_" + secret[:51] + "\x00",
		"prefix twice":           "gbk_gbk_" + key[8:],
		"lookup ID only":         "gbk_" + lookupID,
		"secret without a key":   "gbk__" + secret + strings.Repeat("a", 26),
		"percent-encoded prefix": "gbk%5F" + key[4:len(key)-2],
	} {
		if got, err := auth.ParseAPIKey(bad); !errors.Is(err, auth.ErrUnauthenticated) || got != "" {
			t.Errorf("%s: ParseAPIKey(%q) = %q, %v; want ErrUnauthenticated", name, bad, got, err)
		}
	}
	if !auth.IsAPIKey(key) || auth.IsAPIKey("GBK_"+key[4:]) || auth.IsAPIKey("session") {
		t.Error("IsAPIKey() doesn't match the prefix exactly")
	}
}

func TestAPIKeyMatches(t *testing.T) {
	key, _, hash := auth.NewAPIKey()
	other, _, otherHash := auth.NewAPIKey()
	flipped := func(i int) []byte {
		h := slices.Clone(hash)
		h[i] ^= 1
		return h
	}
	for name, tt := range map[string]struct {
		key  string
		hash []byte
		want bool
	}{
		"the key":              {key, hash, true},
		"another key":          {other, hash, false},
		"another key's hash":   {key, otherHash, false},
		"first byte differs":   {key, flipped(0), false},
		"last byte differs":    {key, flipped(len(hash) - 1), false},
		"nil hash":             {key, nil, false},
		"short hash":           {key, hash[:31], false},
		"long hash":            {key, append(slices.Clone(hash), 0), false},
		"hash of the lookup":   {key, auth.HashAPIKey(key[:31]), false},
		"key with a new line":  {key + "\n", hash, false},
		"empty key and hash":   {"", nil, false},
		"empty key's own hash": {"", auth.HashAPIKey(""), true}, // a hash comparison, not a format check: parse first
	} {
		if got := auth.APIKeyMatches(tt.key, tt.hash); got != tt.want {
			t.Errorf("%s: APIKeyMatches() = %v, want %v", name, got, tt.want)
		}
	}
}

// TestAPIKeyMatchesInConstantTime checks the comparison's construction: the
// stored hash is compared with crypto/subtle, never with bytes.Equal, ==
// or an early return, so how long a wrong key takes doesn't say how much of
// its hash matched. Timing measurements are too noisy for a unit test.
func TestAPIKeyMatchesInConstantTime(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "apikey.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var fn *ast.FuncDecl
	for _, d := range f.Decls {
		if d, ok := d.(*ast.FuncDecl); ok && d.Name.Name == "APIKeyMatches" {
			fn = d
		}
	}
	if fn == nil {
		t.Fatal("APIKeyMatches not found in apikey.go")
	}
	var constantTime bool
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			if id, ok := n.X.(*ast.Ident); ok {
				switch id.Name + "." + n.Sel.Name {
				case "subtle.ConstantTimeCompare":
					constantTime = true
				case "bytes.Equal", "bytes.Compare", "slices.Equal", "reflect.DeepEqual":
					t.Errorf("APIKeyMatches uses %s.%s, which returns at the first difference", id.Name, n.Sel.Name)
				}
			}
		case *ast.ReturnStmt, *ast.IfStmt, *ast.RangeStmt, *ast.ForStmt:
			if _, ok := n.(*ast.ReturnStmt); !ok {
				t.Errorf("APIKeyMatches branches or loops (%T): every key must do the same work", n)
			}
		}
		return true
	})
	if !constantTime {
		t.Error("APIKeyMatches doesn't compare with subtle.ConstantTimeCompare")
	}
}

func TestNewTokenIsNeverAnAPIKey(t *testing.T) {
	for range 1000 {
		if token, _ := auth.NewToken(); auth.IsAPIKey(token) {
			t.Fatalf("NewToken() = %q, which looks like an API key", token)
		}
	}
}

func TestPrincipalRestrict(t *testing.T) {
	granted, stepUp := []string{"a.b.read", "a.b.write", "c.d.read"}, []string{"ops.settings.read"}
	for _, tt := range []struct {
		name       string
		p          auth.Principal
		wantGrant  []string
		wantStepUp []string
	}{
		{"session keeps everything", auth.Principal{SessionID: "ses_1"}, granted, stepUp},
		{"session ignores scopes", auth.Principal{SessionID: "ses_1", Scopes: []string{"a.b.read"}}, granted, stepUp},
		{"key without scopes: no step-up", auth.Principal{APIKeyID: "key_1"}, granted, nil},
		{"key with scopes", auth.Principal{APIKeyID: "key_1", Scopes: []string{"c.d.read", "a.b.read"}}, []string{"a.b.read", "c.d.read"}, nil},
		{"scope the roles don't grant", auth.Principal{APIKeyID: "key_1", Scopes: []string{"a.b.read", "x.y.admin"}}, []string{"a.b.read"}, nil},
		{"scope held back for 2FA", auth.Principal{APIKeyID: "key_1", Scopes: []string{"ops.settings.read"}}, []string{}, nil},
	} {
		gotGrant, gotStepUp := tt.p.Restrict(slices.Clone(granted), slices.Clone(stepUp))
		if !slices.Equal(gotGrant, tt.wantGrant) || !slices.Equal(gotStepUp, tt.wantStepUp) {
			t.Errorf("%s: Restrict() = %v, %v; want %v, %v", tt.name, gotGrant, gotStepUp, tt.wantGrant, tt.wantStepUp)
		}
	}
}

type fakeAPIKeys struct {
	key   string
	p     auth.Principal
	err   error
	calls *int
}

func (f fakeAPIKeys) AuthenticateAPIKey(_ context.Context, key string) (auth.Principal, error) {
	if f.calls != nil {
		*f.calls++
	}
	if f.err != nil {
		return auth.Principal{}, f.err
	}
	if key != f.key {
		return auth.Principal{}, auth.ErrUnauthenticated
	}
	return f.p, nil
}

// countingSessions fails the test if it sees an API key.
type countingSessions struct {
	t     *testing.T
	calls *int
}

func (c countingSessions) Authenticate(_ context.Context, token string) (auth.Principal, error) {
	*c.calls++
	if auth.IsAPIKey(token) {
		c.t.Errorf("the session authenticator was given an API key")
	}
	return auth.Principal{}, auth.ErrUnauthenticated
}

func TestMiddlewareAPIKeys(t *testing.T) {
	key, _, _ := auth.NewAPIKey()
	var sessionCalls, keyCalls int
	sessions := countingSessions{t: t, calls: &sessionCalls}
	type result struct {
		status int
		header http.Header
		actor  actor.Actor
		p      auth.Principal
		ok     bool
	}
	serve := func(opts []auth.MiddlewareOption, mutate func(*http.Request)) result {
		var res result
		h := auth.Middleware(sessions, opts...)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			res.actor = actor.FromOrAnonymous(r.Context())
			res.p, res.ok = auth.PrincipalFrom(r.Context())
		}))
		req := httptest.NewRequest(http.MethodGet, "/v1/projects", nil)
		mutate(req)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		res.status, res.header = rec.Code, rec.Header()
		return res
	}
	bearer := func(token string) func(*http.Request) {
		return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }
	}
	userKey := fakeAPIKeys{key: key, calls: &keyCalls, p: auth.Principal{
		UserID: "usr_1", APIKeyID: "key_1", Scopes: []string{"projects.project.read"},
		Permissions: []string{"projects.project.read", "projects.project.write"}, StepUp: []string{"ops.settings.read"},
	}}

	// Without WithAPIKeys, a key is nobody's, and never a session.
	if r := serve(nil, bearer(key)); r.ok || r.actor.Kind != actor.KindAnonymous || r.status != http.StatusOK || sessionCalls != 0 {
		t.Errorf("API key without WithAPIKeys: %+v, session authenticator calls %d; want anonymous", r, sessionCalls)
	}

	opts := []auth.MiddlewareOption{auth.WithAPIKeys(userKey)}
	r := serve(opts, bearer(key))
	if !r.ok || r.actor.Kind != actor.KindUser || r.actor.ID != "usr_1" || r.p.APIKeyID != "key_1" || r.p.SessionID != "" {
		t.Errorf("user's API key: %+v", r)
	}
	// The actor gets only what Restrict leaves, even from an authenticator
	// that returned more.
	if !slices.Equal(r.actor.Permissions, []string{"projects.project.read"}) || len(r.actor.StepUp) != 0 || !slices.Equal(r.p.Permissions, r.actor.Permissions) {
		t.Errorf("user's API key permissions: actor %+v, principal %+v; want only its scope and no step-up", r.actor, r.p)
	}
	if keyCalls != 1 || sessionCalls != 0 {
		t.Errorf("calls: API keys %d, sessions %d; want 1, 0", keyCalls, sessionCalls)
	}

	// A key in a cookie goes to neither authenticator.
	keyCalls = 0
	r = serve(opts, func(req *http.Request) {
		req.AddCookie(auth.SessionCookie(auth.DefaultCookieName, key, time.Now().Add(time.Hour)))
	})
	if r.ok || keyCalls != 0 || sessionCalls != 0 {
		t.Errorf("API key in the session cookie: %+v, calls %d, %d; want anonymous and no lookups", r, keyCalls, sessionCalls)
	}

	// Wrong and malformed keys continue anonymously.
	wrong := key[:len(key)-1] + "a"
	if wrong == key {
		wrong = key[:len(key)-1] + "b"
	}
	for _, token := range []string{wrong, "gbk_", "gbk_nope"} {
		if r := serve(opts, bearer(token)); r.ok || r.status != http.StatusOK {
			t.Errorf("Bearer %q: %+v; want anonymous", token, r)
		}
	}
	if sessionCalls != 0 {
		t.Errorf("session authenticator saw %d API keys", sessionCalls)
	}
	// A session token still goes to the session authenticator.
	serve(opts, bearer("session-token"))
	if sessionCalls != 1 {
		t.Errorf("session token: session authenticator calls %d, want 1", sessionCalls)
	}

	service := fakeAPIKeys{key: key, p: auth.Principal{ServiceAccountID: "svc_1", APIKeyID: "key_2", OrgID: "org_1", Permissions: []string{"a.b.read"}}}
	if r := serve([]auth.MiddlewareOption{auth.WithAPIKeys(service)}, bearer(key)); r.actor.Kind != actor.KindService || r.actor.ID != "svc_1" || r.actor.OrgID != "" || r.p.OrgID != "org_1" {
		t.Errorf("service account's API key: actor %+v, principal %+v; want a service actor not yet in an organisation", r.actor, r.p)
	}

	limited := fakeAPIKeys{err: &auth.RateLimitError{RetryAfter: 1500 * time.Millisecond}}
	if r := serve([]auth.MiddlewareOption{auth.WithAPIKeys(limited)}, bearer(key)); r.status != http.StatusTooManyRequests || r.header.Get("Retry-After") != "2" || r.ok {
		t.Errorf("limited client: status %d, Retry-After %q; want 429 and 2", r.status, r.header.Get("Retry-After"))
	}
	down := fakeAPIKeys{err: errors.New("database is down")}
	if r := serve([]auth.MiddlewareOption{auth.WithAPIKeys(down)}, bearer(key)); r.status != http.StatusServiceUnavailable {
		t.Errorf("API key authenticator failure: status %d, want 503", r.status)
	}
	var limitErr *auth.RateLimitError
	if err := error(&auth.RateLimitError{RetryAfter: time.Minute}); !errors.Is(err, auth.ErrRateLimited) || !errors.As(err, &limitErr) || strings.Contains(err.Error(), "gbk_") {
		t.Errorf("RateLimitError = %v", err)
	}
}
