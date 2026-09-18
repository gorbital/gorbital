package signintest

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"gorbital.dev/config"
	"gorbital.dev/gorbital"
	authlib "gorbital.dev/modules/auth"
	"gorbital.dev/modules/auth/passkey"
	"gorbital.dev/modules/auth/passkey/passkeytest"
	"gorbital.dev/modules/auth/social"
	"gorbital.dev/modules/auth/social/socialtest"
)

const (
	portalResult   = "http://127.0.0.1:3100/auth/test-result/"
	googleClient   = "1234567890-" + "abcdefghijklmnop" + ".apps.googleusercontent.com" // split so secret scanners skip a fake ID
	googleSecret   = "GOCSPX-" + "abcdefghijklmnopqrstuvwxyz12"
	gitHubClient   = "Ov23liAbCdEfGhIjKlMn"
	gitHubSecretV  = "0123456789abcdef0123456789abcdef01234567"
	appleServiceID = "com.example.web"
)

// clock is a settable clock for tests.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type fixture struct {
	t      *Tester
	srv    *socialtest.Server
	clock  *clock
	config Config
}

func appleKeyPEM(t *testing.T) (*ecdsa.PrivateKey, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	return key, string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

// newFixture configures Google, Apple and GitHub against a fake provider,
// passkeys on localhost and keys, with change applied to the Config.
func newFixture(t *testing.T, change ...func(*Config)) *fixture {
	t.Helper()
	srv := socialtest.New(t)
	c := &clock{now: time.Now()}
	srv.Now = c.Now
	key, keyPEM := appleKeyPEM(t)

	var app gorbital.Config
	app.Env, app.Addr, app.MailDelivery = "development", "127.0.0.1:8080", gorbital.MailDevMail
	a := &app.Auth
	a.PublicURL = "http://localhost:8080"
	a.GoogleClientID, a.GoogleClientSecret, a.GoogleIOSClientID = googleClient, config.NewSecret(googleSecret), "987654321-"+"zyxwvutsrqponmlk"+".apps.googleusercontent.com"
	a.AppleTeamID, a.AppleKeyID, a.ApplePrivateKey, a.AppleServicesID, a.AppleBundleIDs = "TEAM123456", "KEY1234567", config.NewSecret(keyPEM), appleServiceID, []string{"com.example.app"}
	a.GitHubClientID, a.GitHubClientSecret = gitHubClient, config.NewSecret(gitHubSecretV)
	a.WebAuthnRPID, a.WebAuthnOrigins = "localhost", []string{"http://localhost:8080", "http://localhost:3000"}

	google, err := social.NewGoogle(social.GoogleConfig{ClientID: googleClient, ClientSecret: googleSecret, NativeClientIDs: []string{a.GoogleIOSClientID}, Endpoints: srv.Endpoints(), Now: c.Now})
	if err != nil {
		t.Fatal(err)
	}
	apple, err := social.NewApple(social.AppleConfig{TeamID: a.AppleTeamID, KeyID: a.AppleKeyID, PrivateKey: key, ServicesID: appleServiceID, BundleIDs: a.AppleBundleIDs, Endpoints: srv.Endpoints(), Now: c.Now})
	if err != nil {
		t.Fatal(err)
	}
	gitHub, err := social.NewGitHub(social.GitHubConfig{ClientID: gitHubClient, ClientSecret: gitHubSecretV, Endpoints: srv.GitHubEndpoints()})
	if err != nil {
		t.Fatal(err)
	}
	passkeys, err := passkey.New(passkey.Config{RPID: "localhost", RPDisplayName: "acme", Origins: a.WebAuthnOrigins})
	if err != nil {
		t.Fatal(err)
	}
	keyring, err := authlib.ParseKeyring(authlib.NewKeyringKey("k1"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		AppName: "acme", App: app, Google: google, Apple: apple, GitHub: gitHub,
		GoogleEndpoints: srv.Endpoints(), AppleEndpoints: srv.Endpoints(), GitHubEndpoints: srv.GitHubEndpoints(),
		Passkeys: passkeys, Keyring: keyring, Now: c.Now,
	}
	for _, ch := range change {
		ch(&cfg)
	}
	return &fixture{t: New(cfg), srv: srv, clock: c, config: cfg}
}

func TestCheckResultURL(t *testing.T) {
	for raw, want := range map[string]bool{
		portalResult: true,
		"http://localhost:3101/auth/test-result/":           true,
		"http://[::1]:3100/x":                               true,
		"https://127.0.0.1/x?y=1":                           true,
		"":                                                  false,
		"https://evil.example/":                             false,
		"http://127.0.0.1.evil.example:3100/":               false,
		"http://user@127.0.0.1:3100/":                       false,
		"http://127.0.0.1:3100/#frag":                       false,
		"javascript:alert(1)":                               false,
		"//127.0.0.1:3100/":                                 false,
		"http://127.0.0.1:3100/\\@evil.example":             false,
		"http://localhost./":                                false,
		"http://127.0.0.2:3100/":                            false,
		"http://127.0.0.1:/":                                false,
		"http://127.0.0.1:3100/" + strings.Repeat("a", 600): false,
	} {
		_, err := CheckResultURL(raw)
		if (err == nil) != want {
			t.Errorf("CheckResultURL(%q) error = %v, want accepted %v", raw, err, want)
		}
	}
}

func TestRedact(t *testing.T) {
	jwt := "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.c2lnbmF0dXJlLXZhbHVlLWhlcmU"
	msg := redact(`oauth2: cannot fetch token: 400 Response: {"id_token":"`+jwt+`","secret":"GOCSPX-abc123"}`, "GOCSPX-abc123")
	if strings.Contains(msg, jwt) || strings.Contains(msg, "GOCSPX-abc123") || strings.Contains(msg, "c2lnbmF0dXJl") {
		t.Errorf("redact left a token or secret: %s", msg)
	}
	if long := redact(strings.Repeat("é", 400)); len(long) > 504 || !strings.HasSuffix(long, "…") {
		t.Errorf("redact didn't bound the message: %d bytes", len(long))
	}
}

func findCheck(checks []Check, code string) (Check, bool) {
	for _, c := range checks {
		if c.Code == code {
			return c, true
		}
	}
	return Check{}, false
}

func method(o Overview, key string) Method {
	for _, m := range o.Methods {
		if m.Key == key {
			return m
		}
	}
	return Method{}
}

// TestOverview: the offline checks find what's right and wrong in the
// variables, name the fix and its page, and never carry a secret.
func TestOverview(t *testing.T) {
	f := newFixture(t)
	o := f.t.Overview()
	data, _ := json.Marshal(o)
	for _, secret := range []string{googleSecret, gitHubSecretV, "PRIVATE KEY"} {
		if strings.Contains(string(data), secret) {
			t.Errorf("the overview carries a secret %q", secret)
		}
	}
	google := method(o, MethodGoogle)
	if !google.Configured || !google.Live.Available || !google.IDToken || google.CallbackURL != "http://localhost:8080/v1/auth/google/callback" {
		t.Errorf("google = %+v", google)
	}
	for _, code := range []string{"google_client_id_format", "google_client_secret", "google_native_client_id", "public_url", "callback_port", "callback_url"} {
		if c, found := findCheck(google.Checks, code); !found || c.Status != StatusOK {
			t.Errorf("google check %s = %+v (found %v), want ok", code, c, found)
		}
	}
	apple := method(o, MethodApple)
	if c, _ := findCheck(apple.Checks, "apple_public_url_https"); c.Status != StatusFail || c.Link != LinkTunnel {
		t.Errorf("apple on http://localhost: %+v, want a failure linking to the tunnel", c)
	}
	if apple.Live.Available || apple.Live.Link != LinkTunnel {
		t.Errorf("apple live = %+v, want unavailable with the tunnel", apple.Live)
	}
	for _, code := range []string{"apple_team_id", "apple_key_id", "apple_private_key", "apple_services_id"} {
		if c, _ := findCheck(apple.Checks, code); c.Status != StatusOK {
			t.Errorf("apple check %s = %+v, want ok", code, c)
		}
	}
	if m := method(o, MethodPasskeys); !m.Live.Available || m.Origin != "http://localhost:8080" || m.RPID != "localhost" {
		t.Errorf("passkeys = %+v", m)
	}
	if c, _ := findCheck(method(o, MethodAuthenticatorApp).Checks, "auth_encryption_keys"); c.Status != StatusOK {
		t.Errorf("keys = %+v", c)
	}
	if c, _ := findCheck(method(o, MethodEmail).Checks, "mail_delivery"); c.Status != StatusOK {
		t.Errorf("mail = %+v", c)
	}

	// What's wrong is named, with the fix.
	bad := newFixture(t, func(c *Config) {
		c.App.Auth.GoogleClientID = "my-client"
		c.App.Auth.PublicURL = "http://localhost:9999"
		c.App.Auth.GitHubClientID = "Iv23liAbCdEfGhIjKlMn"
		c.App.Auth.AppleServicesID = "com.example.app"
		c.App.Auth.WebAuthnOrigins = []string{"http://localhost:3000"}
		c.GitHub, c.Keyring = nil, nil
	}).t.Overview()
	for _, want := range []struct{ method, code, status string }{
		{MethodGoogle, "google_client_id_format", StatusFail},
		{MethodGoogle, "callback_port", StatusFail},
		{MethodApple, "apple_services_id", StatusFail},
		{MethodGitHub, "github_configured", StatusSkip},
		{MethodPasskeys, "webauthn_live_origin", StatusWarn},
		{MethodAuthenticatorApp, "auth_encryption_keys", StatusWarn},
	} {
		if c, _ := findCheck(method(bad, want.method).Checks, want.code); c.Status != want.status || c.Message == "" {
			t.Errorf("%s %s = %+v, want %s", want.method, want.code, c, want.status)
		}
	}
	if m := method(bad, MethodPasskeys); m.Live.Available {
		t.Errorf("passkeys on an origin that isn't allowed: live %+v", m.Live)
	}
	app := newFixture(t, func(c *Config) { c.App.Auth.GitHubClientID = "Iv23liAbCdEfGhIjKlMn" }).t.Overview()
	if c, _ := findCheck(method(app, MethodGitHub).Checks, "github_client_id_format"); c.Status != StatusWarn {
		t.Errorf("a GitHub App's client ID = %+v, want a warning", c)
	}
	public := newFixture(t, func(c *Config) {
		c.App.Auth.WebAuthnRPID = "github.io"
		c.App.Auth.WebAuthnOrigins = []string{"https://x.github.io"}
	}).t.Overview()
	if c, _ := findCheck(method(public, MethodPasskeys).Checks, "webauthn_rp_id"); c.Status != StatusFail {
		t.Errorf("a public suffix as RP ID = %+v, want a failure", c)
	}
}

type recorder struct {
	called bool
	body   string
}

func (r *recorder) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.called = true
		b, _ := io.ReadAll(req.Body)
		r.body = string(b)
		w.WriteHeader(http.StatusTeapot)
	})
}

func startSocial(t *testing.T, f *fixture, provider string) (Start, url.Values) {
	t.Helper()
	st, err := f.t.StartSocial(provider, portalResult)
	if err != nil {
		t.Fatalf("StartSocial(%s) error = %v", provider, err)
	}
	u, _ := url.Parse(st.URL)
	return st, u.Query()
}

// TestGoogleRoundTrip: a test's state is finished by the interceptor with a
// real code exchange and ID token verification, redirects to the portal's
// page, and never reaches sign-in; replays and late returns stay with the
// test; other states pass through untouched.
func TestGoogleRoundTrip(t *testing.T) {
	f := newFixture(t)
	next := &recorder{}
	h := f.t.Callbacks(next.handler())
	st, q := startSocial(t, f, social.Google)
	if q.Get("redirect_uri") != "http://localhost:8080/v1/auth/google/callback" || q.Get("client_id") != googleClient || q.Get("code_challenge") == "" || q.Get("nonce") == "" {
		t.Fatalf("authorization URL %s", st.URL)
	}
	if r, _ := f.t.Result(st.ID); r.State != StatePending {
		t.Errorf("result before the return = %+v", r)
	}

	code := f.srv.Code(socialtest.Claims{Subject: "g-ada", Audience: googleClient, Email: "ada@example.com", EmailVerified: true, Name: "Ada", Nonce: q.Get("nonce")}, "")
	target := "/v1/auth/google/callback?state=" + url.QueryEscape(q.Get("state")) + "&code=" + url.QueryEscape(code)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	want := portalResult + "#id=" + st.ID + "&method=google"
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != want || next.called || len(rec.Result().Cookies()) != 0 {
		t.Fatalf("callback = %d %q cookies %v (sign-in called %v)", rec.Code, rec.Header().Get("Location"), rec.Result().Cookies(), next.called)
	}
	r, _ := f.t.Result(st.ID)
	if r.State != StatePassed || r.Code != "ok" || r.Identity == nil || r.Identity.Subject != "g-ada" || r.Identity.Email != "ada@example.com" || !r.Identity.EmailVerified || r.Identity.Audience != googleClient {
		t.Errorf("result = %+v %+v", r, r.Identity)
	}
	data, _ := json.Marshal(r)
	if strings.Contains(string(data), "access-") || strings.Contains(string(data), "eyJ") {
		t.Errorf("the result carries a token: %s", data)
	}

	// Replayed: same page, result unchanged, sign-in never sees it.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	if again, _ := f.t.Result(st.ID); rec.Code != http.StatusSeeOther || next.called || again.State != StatePassed {
		t.Errorf("replayed callback = %d, sign-in called %v, result %+v", rec.Code, next.called, again)
	}

	// Sign-in's own states, forged ones and other paths pass through.
	for _, target := range []string{
		"/v1/auth/google/callback?state=not-a-test&code=x",
		"/v1/auth/google/callback?code=x",
		"/v1/auth/google/start?state=" + url.QueryEscape(q.Get("state")),
		"/v1/auth/github/callback?state=" + strings.Repeat("a", 300),
	} {
		next.called = false
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		if !next.called || rec.Code != http.StatusTeapot {
			t.Errorf("%s: sign-in called %v (%d), want it to handle the request", target, next.called, rec.Code)
		}
	}

	// The provider's refusal, and a code the provider refuses.
	for _, tt := range []struct{ query, code string }{
		{"&error=access_denied", "access_denied"},
		{"&error=redirect_uri_mismatch", "redirect_uri_mismatch"},
		{"&code=unknown-code", "invalid_grant"},
		{"", "invalid_request"},
	} {
		st, q := startSocial(t, f, social.Google)
		rec := httptest.NewRecorder()
		next.called = false
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/auth/google/callback?state="+url.QueryEscape(q.Get("state"))+tt.query, nil))
		r, _ := f.t.Result(st.ID)
		if rec.Code != http.StatusSeeOther || next.called || r.State != StateFailed || r.Code != tt.code || r.Message == "" {
			t.Errorf("callback%s = %d, result %+v, want failed %s", tt.query, rec.Code, r, tt.code)
		}
	}

	// A state returned to another provider's callback fails the test.
	st, q = startSocial(t, f, social.Google)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/auth/github/callback?code=x&state="+url.QueryEscape(q.Get("state")), nil))
	if r, _ := f.t.Result(st.ID); rec.Code != http.StatusSeeOther || r.Code != "invalid_request" {
		t.Errorf("state on another callback = %d %+v", rec.Code, r)
	}

	// Late: the test expired, the return still lands on the result.
	st, q = startSocial(t, f, social.Google)
	f.clock.Add(SocialTTL + time.Second)
	if r, _ := f.t.Result(st.ID); r.State != StateExpired {
		t.Errorf("result after the TTL = %+v", r)
	}
	next.called = false
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/auth/google/callback?code=x&state="+url.QueryEscape(q.Get("state")), nil))
	if r, _ := f.t.Result(st.ID); rec.Code != http.StatusSeeOther || next.called || r.State != StateExpired {
		t.Errorf("late callback = %d, sign-in called %v, result %+v", rec.Code, next.called, r)
	}
	f.clock.Add(ResultTTL)
	if _, err := f.t.Result(st.ID); !errors.Is(err, ErrTestNotFound) {
		t.Errorf("result after ResultTTL: %v", err)
	}
}

// TestAppleRoundTrip: Apple's form post is read for a test and put back for
// sign-in otherwise; the first-time name is kept. Apple needs https.
func TestAppleRoundTrip(t *testing.T) {
	if _, err := newFixture(t).t.StartSocial(social.Apple, portalResult); err == nil {
		t.Fatal("StartSocial(apple) on http://localhost: no error")
	} else if u := (*UnavailableError)(nil); !errors.As(err, &u) || u.Link != LinkTunnel {
		t.Fatalf("StartSocial(apple) on http://localhost = %v", err)
	}
	f := newFixture(t, func(c *Config) { c.App.Auth.PublicURL = "https://dev.example.com" })
	next := &recorder{}
	h := f.t.Callbacks(next.handler())
	st, q := startSocial(t, f, social.Apple)
	if q.Get("response_mode") != "form_post" || q.Get("redirect_uri") != "https://dev.example.com/v1/auth/apple/callback" {
		t.Fatalf("Apple URL %s", st.URL)
	}
	code := f.srv.Code(socialtest.Claims{Subject: "001.bob", Audience: appleServiceID, Email: "x@privaterelay.appleid.com", PrivateEmail: true, Extra: map[string]any{"email_verified": "true"}, Nonce: q.Get("nonce")}, "refresh-secret")
	form := url.Values{"state": {q.Get("state")}, "code": {code}, "user": {`{"name":{"firstName":"Bob","lastName":"Smith"}}`}}
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/apple/callback", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	r, _ := f.t.Result(st.ID)
	if rec.Code != http.StatusSeeOther || next.called || r.State != StatePassed || r.Identity.Name != "Bob Smith" || !r.Identity.PrivateEmail {
		t.Fatalf("Apple callback = %d, result %+v %+v", rec.Code, r, r.Identity)
	}
	if data, _ := json.Marshal(r); strings.Contains(string(data), "refresh-secret") {
		t.Errorf("the result carries Apple's refresh token: %s", data)
	}

	body := url.Values{"state": {"sign-in-state"}, "code": {"c"}}.Encode()
	req = httptest.NewRequest(http.MethodPost, "/v1/auth/apple/callback", strings.NewReader(body))
	h.ServeHTTP(httptest.NewRecorder(), req)
	if !next.called || next.body != body {
		t.Errorf("sign-in's Apple post: called %v with body %q, want %q", next.called, next.body, body)
	}
}

func TestGitHubRoundTrip(t *testing.T) {
	f := newFixture(t)
	h := f.t.Callbacks((&recorder{}).handler())
	st, q := startSocial(t, f, social.GitHub)
	code := f.srv.GitHubCode(socialtest.GitHubUser{ID: 42, Login: "octo", Emails: []socialtest.GitHubEmail{{Email: "octo@example.com", Primary: true, Verified: false}}}, q.Get("code_challenge"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/auth/github/callback?code="+url.QueryEscape(code)+"&state="+url.QueryEscape(q.Get("state")), nil))
	r, _ := f.t.Result(st.ID)
	if rec.Code != http.StatusSeeOther || r.State != StatePassed || r.Identity.Subject != "42" || r.Identity.Name != "octo" || len(r.Warnings) != 1 || r.Warnings[0].Code != "email_not_verified" {
		t.Errorf("GitHub = %d %+v %+v", rec.Code, r, r.Identity)
	}
	// GitHub redirects its own refusal to the callback.
	st, q = startSocial(t, f, social.GitHub)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/auth/github/callback?error=redirect_uri_mismatch&error_description=x&state="+url.QueryEscape(q.Get("state")), nil))
	if r, _ := f.t.Result(st.ID); r.Code != "redirect_uri_mismatch" || !strings.Contains(r.Fix, "http://localhost:8080/v1/auth/github/callback") || r.Link != LinkGuide {
		t.Errorf("GitHub redirect_uri_mismatch = %+v", r)
	}
}

// tokenServer answers every token request with an OAuth error.
func tokenServer(t *testing.T, status int, body string) social.Endpoints {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/keys" {
			_, _ = io.WriteString(w, `{"keys":[]}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return social.Endpoints{AuthURL: srv.URL + "/auth", TokenURL: srv.URL + "/token", KeysURL: srv.URL + "/keys", Issuers: []string{srv.URL}}
}

func TestNetworkChecks(t *testing.T) {
	f := newFixture(t)
	res, err := f.t.NetworkChecks(context.Background(), social.Google)
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"provider_reachable", "clock_skew", "client_credentials"} {
		if c, _ := findCheck(res.Checks, code); c.Status != StatusOK {
			t.Errorf("google %s = %+v, want ok", code, c)
		}
	}
	gh, _ := f.t.NetworkChecks(context.Background(), social.GitHub)
	if c, _ := findCheck(gh.Checks, "client_credentials"); c.Status != StatusOK {
		t.Errorf("github credentials = %+v", c)
	}
	if _, err := newFixture(t, func(c *Config) { c.GitHub = nil }).t.NetworkChecks(context.Background(), social.GitHub); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("an unconfigured provider: %v", err)
	}

	// A refused client, and a clock two minutes fast.
	ep := tokenServer(t, http.StatusUnauthorized, `{"error":"invalid_client","error_description":"The OAuth client was not found."}`)
	bad := newFixture(t, func(c *Config) {
		c.Google, _ = social.NewGoogle(social.GoogleConfig{ClientID: googleClient, ClientSecret: googleSecret, Endpoints: ep})
		c.GoogleEndpoints = ep
		now := c.Now
		c.Now = func() time.Time { return now().Add(2 * time.Minute) }
	})
	res, _ = bad.t.NetworkChecks(context.Background(), social.Google)
	if c, _ := findCheck(res.Checks, "client_credentials"); c.Status != StatusFail || !strings.Contains(c.Message, "client credentials") || strings.Contains(c.Message, googleSecret) {
		t.Errorf("refused client = %+v", c)
	}
	if c, _ := findCheck(res.Checks, "clock_skew"); c.Status != StatusFail || !strings.Contains(c.Message, "ahead") {
		t.Errorf("fast clock = %+v", c)
	}
	unreachable := newFixture(t, func(c *Config) {
		c.GoogleEndpoints.TokenURL = "http://127.0.0.1:1/token"
	})
	res, _ = unreachable.t.NetworkChecks(context.Background(), social.Google)
	if c, _ := findCheck(res.Checks, "provider_reachable"); c.Status != StatusFail {
		t.Errorf("unreachable = %+v", c)
	}
}

func TestClockCheck(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	date := now.Add(-50 * time.Second).Format(http.TimeFormat)
	if c := clockCheck("google", date, "50", now, now); c.Status != StatusOK {
		t.Errorf("a cached answer with Age = %+v, want ok", c)
	}
	if c := clockCheck("google", date, "", now, now); c.Status != StatusWarn {
		t.Errorf("50 seconds ahead = %+v, want a warning", c)
	}
	if c := clockCheck("google", "", "", now, now); c.Status != StatusSkip {
		t.Errorf("no Date = %+v", c)
	}
}

func TestVerifyIDToken(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	token := f.srv.IDToken(socialtest.Claims{Subject: "g-carol", Audience: f.config.App.Auth.GoogleIOSClientID, Email: "carol@example.com", EmailVerified: true, Nonce: "n-1"})
	r, err := f.t.VerifyIDToken(ctx, social.Google, token, "n-1")
	if err != nil || r.State != StatePassed || r.Identity.Subject != "g-carol" || r.Kind != "id_token" {
		t.Fatalf("VerifyIDToken = %+v %v", r, err)
	}
	if got, _ := f.t.Result(r.ID); got.State != StatePassed {
		t.Errorf("stored result = %+v", got)
	}
	for _, tt := range []struct {
		name, provider, token, nonce, code string
	}{
		{"audience", social.Google, f.srv.IDToken(socialtest.Claims{Subject: "s", Audience: "someone-else", Nonce: "n"}), "n", "audience_mismatch"},
		{"nonce", social.Google, f.srv.IDToken(socialtest.Claims{Subject: "s", Audience: googleClient, Nonce: "n"}), "other", "nonce_mismatch"},
		{"old", social.Google, f.srv.IDToken(socialtest.Claims{Subject: "s", Audience: googleClient, Nonce: "n", IssuedAt: f.clock.Now().Add(-30 * time.Minute)}), "n", "clock_skew"},
		{"forged", social.Google, token[:len(token)-4] + "AAAA", "n-1", "id_token_signature"},
		{"apple raw nonce", social.Apple, f.srv.IDToken(socialtest.Claims{Subject: "001", Audience: "com.example.app", Nonce: "raw"}), "raw", "nonce_mismatch"},
	} {
		r, err := f.t.VerifyIDToken(ctx, tt.provider, tt.token, tt.nonce)
		if err != nil || r.State != StateFailed || r.Code != tt.code || strings.Contains(r.Message, tt.token) {
			t.Errorf("%s: %+v %v, want %s", tt.name, r, err, tt.code)
		}
		if tt.name == "apple raw nonce" && !strings.Contains(r.Fix, "SHA-256") {
			t.Errorf("apple raw nonce fix = %q", r.Fix)
		}
	}
	if _, err := f.t.VerifyIDToken(ctx, social.GitHub, token, "n"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("GitHub ID token: %v", err)
	}
	if _, err := f.t.VerifyIDToken(ctx, social.Google, "", "n"); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("empty token: %v", err)
	}
}

func TestTOTP(t *testing.T) {
	f := newFixture(t)
	start, err := f.t.StartTOTP()
	if err != nil || !strings.HasPrefix(start.QRCode, "data:image/png;base64,") || !strings.HasPrefix(start.URI, "otpauth://totp/") || len(start.Checks) != 1 {
		t.Fatalf("StartTOTP = %+v %v", start, err)
	}
	code, _ := authlib.TOTPCode(start.Secret, f.clock.Now().Add(-3*authlib.TOTPPeriod))
	if r, _ := f.t.VerifyTOTP(start.ID, code); r.Passed || r.Code != "clock_drift" || r.DriftSteps != -3 || r.DriftSeconds != -90 || r.AttemptsLeft != MaxTOTPAttempts-1 {
		t.Errorf("a code 90 seconds old = %+v", r)
	}
	if r, _ := f.t.VerifyTOTP(start.ID, "000000x"); r.Passed || r.Code != "invalid_code" {
		t.Errorf("a wrong code = %+v", r)
	}
	code, _ = authlib.TOTPCode(start.Secret, f.clock.Now())
	if r, _ := f.t.VerifyTOTP(start.ID, code); !r.Passed || r.Code != "ok" {
		t.Errorf("the current code = %+v", r)
	}
	if _, err := f.t.VerifyTOTP(start.ID, code); !errors.Is(err, ErrTestNotFound) {
		t.Errorf("a passed test again: %v", err)
	}

	start, _ = f.t.StartTOTP()
	var last TOTPResult
	for range MaxTOTPAttempts {
		last, _ = f.t.VerifyTOTP(start.ID, "12")
	}
	if last.Code != "too_many_attempts" {
		t.Errorf("after %d wrong codes: %+v", MaxTOTPAttempts, last)
	}
	start, _ = f.t.StartTOTP()
	f.clock.Add(TOTPTTL)
	if r, _ := f.t.VerifyTOTP(start.ID, "123456"); r.Code != "expired" {
		t.Errorf("expired = %+v", r)
	}
}

func postJSON(t *testing.T, h http.Handler, path string, body any) (int, passkeyAnswer) {
	t.Helper()
	data, _ := json.Marshal(body)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(data))))
	var a passkeyAnswer
	_ = json.Unmarshal(rec.Body.Bytes(), &a)
	return rec.Code, a
}

func ticketOf(t *testing.T, st Start) string {
	t.Helper()
	_, fragment, _ := strings.Cut(st.URL, "#ticket=")
	if fragment == "" || !strings.HasPrefix(st.URL, "http://localhost:8080"+PasskeyPagePath+"#") {
		t.Fatalf("ceremony URL %s", st.URL)
	}
	return fragment
}

// TestPasskeyCeremony: registration then sign-in with a software
// authenticator through the page's endpoints; the ticket works once; the
// origin and the browser's refusals are reported precisely.
func TestPasskeyCeremony(t *testing.T) {
	f := newFixture(t)
	h := f.t.PasskeyHandler()

	page := httptest.NewRecorder()
	h.ServeHTTP(page, httptest.NewRequest(http.MethodGet, PasskeyPagePath, nil))
	csp := page.Header().Get("Content-Security-Policy")
	if page.Code != 200 || !strings.Contains(csp, "default-src 'none'") || !strings.Contains(csp, "script-src 'nonce-") || !strings.Contains(page.Body.String(), `const path = "/_signin-test/passkey"`) {
		t.Errorf("page = %d %q\n%s", page.Code, csp, page.Body.String()[:200])
	}

	st, err := f.t.StartPasskey(portalResult)
	if err != nil {
		t.Fatal(err)
	}
	ticket := ticketOf(t, st)
	auth := passkeytest.New("http://localhost:8080")
	code, reg := postJSON(t, h, PasskeyPagePath+"/options", map[string]any{"ticket": ticket, "step": "register"})
	if code != 200 || reg.RPID != "localhost" || len(reg.Options) == 0 {
		t.Fatalf("register options = %d %+v", code, reg)
	}
	created, err := auth.Create(reg.Options)
	if err != nil {
		t.Fatal(err)
	}
	if code, next := postJSON(t, h, PasskeyPagePath+"/finish", map[string]any{"ticket": ticket, "step": "register", "response": json.RawMessage(created)}); code != 200 || next.Next != "login" {
		t.Fatalf("register finish = %d %+v", code, next)
	}
	code, login := postJSON(t, h, PasskeyPagePath+"/options", map[string]any{"ticket": ticket, "step": "login"})
	if code != 200 {
		t.Fatalf("login options = %d %+v", code, login)
	}
	got, err := auth.Get(login.Options)
	if err != nil {
		t.Fatal(err)
	}
	code, done := postJSON(t, h, PasskeyPagePath+"/finish", map[string]any{"ticket": ticket, "step": "login", "response": json.RawMessage(got)})
	if code != 200 || done.Next != "done" || done.Redirect != portalResult+"#id="+st.ID+"&method=passkeys" {
		t.Fatalf("login finish = %d %+v", code, done)
	}
	r, _ := f.t.Result(st.ID)
	if r.State != StatePassed || r.Passkey == nil || r.Passkey.RPID != "localhost" || r.Passkey.Origin != "http://localhost:8080" || !r.Passkey.UserVerified {
		t.Errorf("result = %+v %+v", r, r.Passkey)
	}
	if code, _ := postJSON(t, h, PasskeyPagePath+"/options", map[string]any{"ticket": ticket, "step": "login"}); code != http.StatusNotFound {
		t.Errorf("the ticket after the test = %d, want 404", code)
	}
	if code, _ := postJSON(t, h, PasskeyPagePath+"/options", map[string]any{"ticket": "forged", "step": "register"}); code != http.StatusNotFound {
		t.Errorf("a forged ticket = %d, want 404", code)
	}

	// An authenticator on another origin.
	st, _ = f.t.StartPasskey(portalResult)
	ticket = ticketOf(t, st)
	_, reg = postJSON(t, h, PasskeyPagePath+"/options", map[string]any{"ticket": ticket, "step": "register"})
	created, _ = passkeytest.New("http://localhost:4000").Create(reg.Options)
	postJSON(t, h, PasskeyPagePath+"/finish", map[string]any{"ticket": ticket, "step": "register", "response": json.RawMessage(created)})
	if r, _ := f.t.Result(st.ID); r.State != StateFailed || r.Code != "origin_not_allowed" {
		t.Errorf("another origin = %+v", r)
	}

	// The browser's refusal.
	st, _ = f.t.StartPasskey(portalResult)
	ticket = ticketOf(t, st)
	postJSON(t, h, PasskeyPagePath+"/options", map[string]any{"ticket": ticket, "step": "register"})
	postJSON(t, h, PasskeyPagePath+"/finish", map[string]any{"ticket": ticket, "step": "register", "error": map[string]string{"name": "SecurityError", "message": "The relying party ID is not a registrable domain suffix of, nor equal to the current domain."}})
	if r, _ := f.t.Result(st.ID); r.Code != "rp_id_mismatch" || r.Link != LinkEnvironment {
		t.Errorf("SecurityError = %+v", r)
	}

	// Unavailable when the app's origin isn't allowed.
	if _, err := newFixture(t, func(c *Config) { c.App.Auth.PublicURL = "http://localhost:9000" }).t.StartPasskey(portalResult); err == nil {
		t.Error("StartPasskey on an origin that isn't allowed: no error")
	}
}

func TestLimits(t *testing.T) {
	f := newFixture(t)
	for range MaxPending {
		if _, err := f.t.StartSocial(social.Google, portalResult); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.t.StartSocial(social.Google, portalResult); !errors.Is(err, ErrTooManyTests) {
		t.Errorf("start %d: %v, want ErrTooManyTests", MaxPending+1, err)
	}
	if _, err := f.t.StartSocial(social.Google, "https://evil.example/"); !errors.Is(err, ErrInvalidResultURL) {
		t.Errorf("another site's result URL: %v", err)
	}
	if _, err := newFixture(t, func(c *Config) { c.Google = nil }).t.StartSocial(social.Google, portalResult); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("unconfigured: %v", err)
	}
}

// TestConsoleHandler: the endpoints' statuses and codes.
func TestConsoleHandler(t *testing.T) {
	f := newFixture(t)
	h := f.t.ConsoleHandler()
	serve := func(method, path, body string) (int, map[string]any) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		var v map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &v)
		return rec.Code, v
	}
	for _, tt := range []struct {
		method, path, body string
		status             int
		code               string
	}{
		{"GET", "/_dev/auth/test", "", 200, ""},
		{"GET", "/_dev/auth/test/", "", 200, ""},
		{"POST", "/_dev/auth/test/google/start", `{"result_url":"` + portalResult + `"}`, 201, ""},
		{"POST", "/_dev/auth/test/passkeys/start", `{"result_url":"` + portalResult + `"}`, 201, ""},
		{"POST", "/_dev/auth/test/apple/start", `{"result_url":"` + portalResult + `"}`, 409, "live_test_unavailable"},
		{"POST", "/_dev/auth/test/google/start", `{"result_url":"https://evil.example"}`, 422, "invalid_result_url"},
		{"POST", "/_dev/auth/test/google/start", `nope`, 400, "invalid_json"},
		{"POST", "/_dev/auth/test/twitter/start", `{}`, 404, "not_configured"},
		{"POST", "/_dev/auth/test/google/check", `{}`, 200, ""},
		{"POST", "/_dev/auth/test/google/id-token", `{"id_token":"x.y.z","nonce":"n"}`, 200, ""},
		{"POST", "/_dev/auth/test/google/id-token", `{}`, 422, "invalid_request"},
		{"GET", "/_dev/auth/test/results/slt_nope", "", 404, "test_not_found"},
		{"POST", "/_dev/auth/test/totp/start", "", 201, ""},
		{"POST", "/_dev/auth/test/totp/verify", `{"id":"nope","code":"123456"}`, 404, "test_not_found"},
		{"DELETE", "/_dev/auth/test/google/start", "", 404, "not_found"},
	} {
		status, v := serve(tt.method, tt.path, tt.body)
		if status != tt.status || (tt.code != "" && v["code"] != tt.code) {
			t.Errorf("%s %s = %d %v, want %d %s", tt.method, tt.path, status, v, tt.status, tt.code)
		}
	}
}
