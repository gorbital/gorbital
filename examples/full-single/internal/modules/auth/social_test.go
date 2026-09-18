package authhttp

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorbital.dev/actor"
	"gorbital.dev/config"
	"gorbital.dev/gorbital"
	"gorbital.dev/mail"
	"gorbital.dev/modules/auth/social/socialtest"
	"gorbital.dev/modules/postgres/pgtest"
	"gorbital.dev/modules/storage/local"
)

// appleKeyFile writes a Sign in with Apple .p8 key and returns its path.
func appleKeyFile(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	path := filepath.Join(t.TempDir(), "AuthKey_KEY1234567.p8")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func socialEnv(t *testing.T) map[string]string {
	return map[string]string{
		"GOOGLE_CLIENT_ID": "web-client", "GOOGLE_CLIENT_SECRET": "secret", "GOOGLE_IOS_CLIENT_ID": "ios-client",
		"APPLE_TEAM_ID": "TEAM123456", "APPLE_KEY_ID": "KEY1234567", "APPLE_PRIVATE_KEY_FILE": appleKeyFile(t),
		"APPLE_SERVICES_ID": "com.example.web", "APPLE_BUNDLE_IDS": "com.example.app",
		"APP_CORS_ORIGINS": "https://app.example.com",
	}
}

// socialProductionEnv is what else production requires of a test app, as
// the golden app's mailProviderEnv: the email provider's key and file
// storage off this machine (ADR-0075).
var socialProductionEnv = map[string]string{
	"RESEND_API_KEY": "re_123",
	"STORAGE_DRIVER": "s3", "STORAGE_REGION": "eu-west-1", "STORAGE_BUCKET": "files", "STORAGE_ACCESS_KEY": "AKIA", "STORAGE_SECRET_KEY": "secret",
}

// loadSocialConfig loads the configuration as a v0.1 app's LoadConfig did:
// gorbital.LoadConfig, then the checks sign-in leaves to the authenticator.
func loadSocialConfig(src config.Source) (gorbital.Config, error) {
	cfg, err := gorbital.LoadConfig(src)
	if err != nil {
		return cfg, err
	}
	return cfg, New().CheckConfig(cfg)
}

// newSocialProductionApp is newApp with what gorbital.New requires in
// production and the golden app built in (infra_mail.go, storage.go): an
// email provider, here discarding email, and a store for STORAGE_DRIVER=s3,
// here a local one, since neither is used. It returns the handler.
func newSocialProductionApp(t *testing.T, env map[string]string, configure ...func(*gorbital.Config, *Authenticator)) http.Handler {
	t.Helper()
	full := map[string]string{"DATABASE_URL": pgtest.NewDatabase(t)}
	maps.Copy(full, env)
	cfg := testConfig(t, full)
	ctx := context.Background()
	auth := New()
	for _, c := range configure {
		c(&cfg, auth)
	}
	discard := mail.SenderFunc(func(context.Context, mail.Message) error { return nil })
	store, err := local.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	opts := append(appOptions(auth), gorbital.WithMailer(discard), gorbital.WithStorage(store))
	if err := gorbital.Migrate(ctx, cfg, io.Discard, opts...); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	a, err := gorbital.New(ctx, cfg, opts...)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := a.Close(context.WithoutCancel(ctx)); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return a.Handler()
}

// cookieValue returns the value of the cookie name set by r.
func cookieValue(r response, name string) string {
	for _, c := range r.header.Values("Set-Cookie") {
		if v, ok := strings.CutPrefix(c, name+"="); ok {
			value, _, _ := strings.Cut(v, ";")
			return value
		}
	}
	return ""
}

// TestSocialSignInEndToEnd follows ADR-0046 over HTTP against a fake
// provider: both web flows, a native sign-in, identities and Apple's
// notifications.
func TestSocialSignInEndToEnd(t *testing.T) {
	srv := socialtest.New(t)
	h := newApp(t, socialEnv(t), func(_ *gorbital.Config, a *Authenticator) {
		a.endpoints.Google, a.endpoints.Apple = srv.Endpoints(), srv.Endpoints()
	}).Handler()

	// Google in a browser.
	start := do(t, h, "GET", "/v1/auth/google/start?return_to="+url.QueryEscape("https://app.example.com/after"), "")
	location, _ := url.Parse(start.header.Get("Location"))
	browser := cookieValue(start, "__Host-oauth")
	if start.code != http.StatusFound || !strings.HasPrefix(location.String(), srv.URL) || browser == "" ||
		!strings.Contains(strings.Join(start.header.Values("Set-Cookie"), ";"), "SameSite=None") {
		t.Fatalf("GET /v1/auth/google/start = %d %v %v", start.code, start.header, start.body)
	}
	q := location.Query()
	code := srv.Code(socialtest.Claims{Subject: "g-ada", Audience: "web-client", Email: "ada@example.com", EmailVerified: true, Nonce: q.Get("nonce")}, "")
	callback := do(t, h, "GET", "/v1/auth/google/callback?code="+url.QueryEscape(code)+"&state="+url.QueryEscape(q.Get("state")), "", "Cookie", "__Host-oauth="+browser)
	if callback.code != http.StatusSeeOther || callback.header.Get("Location") != "https://app.example.com/after" || cookieValue(callback, "__Host-session") == "" {
		t.Fatalf("GET /v1/auth/google/callback = %d Location %q cookies %v %s", callback.code, callback.header.Get("Location"), callback.header.Values("Set-Cookie"), callback.body)
	}

	// Without the browser's cookie the frontend gets an error code.
	start = do(t, h, "GET", "/v1/auth/google/start?return_to="+url.QueryEscape("https://app.example.com/after"), "")
	location, _ = url.Parse(start.header.Get("Location"))
	failed := do(t, h, "GET", "/v1/auth/google/callback?code=x&state="+url.QueryEscape(location.Query().Get("state")), "")
	if failed.code != http.StatusSeeOther || failed.header.Get("Location") != "https://app.example.com/after#error=invalid_state" {
		t.Errorf("callback without the cookie = %d %q", failed.code, failed.header.Get("Location"))
	}
	if r := do(t, h, "GET", "/v1/auth/google/start?return_to="+url.QueryEscape("https://evil.example/"), ""); r.code != http.StatusUnprocessableEntity || r.json["code"] != "invalid_return_to" {
		t.Errorf("start with another site's return_to = %d %s", r.code, r.body)
	}

	// Apple posts from its own site, with the name the first time.
	start = do(t, h, "GET", "/v1/auth/apple/start", "")
	location, _ = url.Parse(start.header.Get("Location"))
	q = location.Query()
	code = srv.Code(socialtest.Claims{Subject: "001.bob", Audience: "com.example.web", Email: "bob@example.com", Extra: map[string]any{"email_verified": "true"}, Nonce: q.Get("nonce")}, "")
	form := url.Values{"code": {code}, "state": {q.Get("state")}, "user": {`{"name":{"firstName":"Bob","lastName":"Smith"}}`}}
	apple := do(t, h, "POST", "/v1/auth/apple/callback", form.Encode(),
		"Content-Type", "application/x-www-form-urlencoded", "Cookie", "__Host-oauth="+cookieValue(start, "__Host-oauth"),
		"Origin", "https://appleid.apple.com", "Sec-Fetch-Site", "cross-site")
	session := cookieValue(apple, "__Host-session")
	if apple.code != http.StatusSeeOther || apple.header.Get("Location") != "http://localhost:8080/docs" || session == "" {
		t.Fatalf("POST /v1/auth/apple/callback = %d %q %s", apple.code, apple.header.Get("Location"), apple.body)
	}
	identities := do(t, h, "GET", "/v1/auth/identities", "", "Cookie", "__Host-session="+session)
	items, _ := identities.json["identities"].([]any)
	if identities.code != http.StatusOK || len(items) != 1 || items[0].(map[string]any)["name"] != "Bob Smith" {
		t.Errorf("GET /v1/auth/identities = %d %s", identities.code, identities.body)
	}

	// An iOS app signs in with Google's ID token and a nonce.
	nonce := do(t, h, "POST", "/v1/auth/google/nonce", "")
	token := srv.IDToken(socialtest.Claims{Subject: "g-carol", Audience: "ios-client", Email: "carol@example.com", EmailVerified: true, Nonce: fmt.Sprint(nonce.json["nonce"])})
	native := do(t, h, "POST", "/v1/auth/google/token", fmt.Sprintf(`{"id_token":%q,"nonce":%q}`, token, nonce.json["nonce"]))
	if user, _ := native.json["user"].(map[string]any); native.code != http.StatusOK || native.json["token"] == nil || user["has_password"] != false {
		t.Fatalf("POST /v1/auth/google/token = %d %s", native.code, native.body)
	}
	if r := do(t, h, "POST", "/v1/auth/google/token", fmt.Sprintf(`{"id_token":%q,"nonce":%q}`, token, nonce.json["nonce"])); r.code != http.StatusUnauthorized || r.json["code"] != "invalid_social_token" {
		t.Errorf("POST /v1/auth/google/token with a used nonce = %d %s", r.code, r.body)
	}

	// Apple's notifications arrive from Apple's servers.
	notice := do(t, h, "POST", "/v1/auth/apple/notifications", fmt.Sprintf(`{"payload":%q}`, srv.Notification("com.example.web", "email-disabled", "001.bob")),
		"Origin", "https://appleid.apple.com", "Sec-Fetch-Site", "cross-site")
	if notice.code >= 300 {
		t.Errorf("POST /v1/auth/apple/notifications = %d %s", notice.code, notice.body)
	}
	if r := do(t, h, "POST", "/v1/auth/apple/notifications", `{"payload":"forged"}`); r.code != http.StatusUnauthorized {
		t.Errorf("POST /v1/auth/apple/notifications with a forged payload = %d %s", r.code, r.body)
	}
}

// TestSocialLinkingEndToEnd: Google sign-in with an address Google doesn't
// manage doesn't sign in to the existing account of that address; its owner
// links Google while signed in (security review AUTH-M-1). This was the
// reviewers' proof of concept.
func TestSocialLinkingEndToEnd(t *testing.T) {
	srv := socialtest.New(t)
	a := newApp(t, socialEnv(t), func(_ *gorbital.Config, a *Authenticator) {
		a.endpoints.Google, a.endpoints.Apple = srv.Endpoints(), srv.Endpoints()
	})
	h := a.Handler()
	ctx := actor.With(context.Background(), actor.System("test"))
	const email = "victim@corp.example"
	owner, err := a.Auth().CreateUser(ctx, email, testPassword, true)
	if err != nil {
		t.Fatal(err)
	}
	google := socialtest.Claims{Subject: "g-victim", Audience: "web-client", Email: email, EmailVerified: true}
	webSignIn := func() response {
		t.Helper()
		start := do(t, h, "GET", "/v1/auth/google/start?return_to="+url.QueryEscape("https://app.example.com/after"), "")
		location, _ := url.Parse(start.header.Get("Location"))
		c := google
		c.Nonce = location.Query().Get("nonce")
		return do(t, h, "GET", "/v1/auth/google/callback?code="+url.QueryEscape(srv.Code(c, ""))+"&state="+url.QueryEscape(location.Query().Get("state")), "",
			"Cookie", "__Host-oauth="+cookieValue(start, "__Host-oauth"))
	}
	if r := webSignIn(); r.header.Get("Location") != "https://app.example.com/after#error=social_link_required" || cookieValue(r, "__Host-session") != "" {
		t.Fatalf("Google sign-in with the address of an account = %d %q %v, want #error=social_link_required and no session", r.code, r.header.Get("Location"), r.header.Values("Set-Cookie"))
	}

	session := cookieHeader(t, do(t, h, "POST", "/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, email, testPassword)))
	link := func(headers ...string) response {
		t.Helper()
		nonce := fmt.Sprint(do(t, h, "POST", "/v1/auth/google/nonce", "").json["nonce"])
		c := google
		c.Nonce = nonce
		return do(t, h, "POST", "/v1/auth/identities", fmt.Sprintf(`{"provider":"google","id_token":%q,"nonce":%q,"password":%q}`, srv.IDToken(c), nonce, testPassword), headers...)
	}
	if r := link(session...); r.code != http.StatusCreated || r.json["provider"] != "google" || r.json["email"] != email {
		t.Fatalf("POST /v1/auth/identities = %d %s, want 201", r.code, r.body)
	}
	if r := link(session...); r.code != http.StatusOK {
		t.Errorf("POST /v1/auth/identities again = %d %s, want 200", r.code, r.body)
	}
	signedIn := webSignIn()
	me := do(t, h, "GET", "/v1/auth/me", "", "Cookie", "__Host-session="+cookieValue(signedIn, "__Host-session"))
	if user, _ := me.json["user"].(map[string]any); me.code != http.StatusOK || user["id"] != owner.ID {
		t.Errorf("Google sign-in after linking = %d %s, want the owner's account", me.code, me.body)
	}

	if _, err := a.Auth().CreateUser(ctx, "other@corp.example", testPassword, true); err != nil {
		t.Fatal(err)
	}
	other := cookieHeader(t, do(t, h, "POST", "/v1/auth/login", fmt.Sprintf(`{"email":"other@corp.example","password":%q}`, testPassword)))
	if r := link(other...); r.code != http.StatusConflict || r.json["code"] != "identity_in_use" {
		t.Errorf("POST /v1/auth/identities with another account's Google = %d %s, want 409 identity_in_use", r.code, r.body)
	}
	if r := link(); r.code != http.StatusUnauthorized {
		t.Errorf("POST /v1/auth/identities signed out = %d %s, want 401", r.code, r.body)
	}
}

// TestAppleNotificationReplayEndToEnd is the reviewers' proof of concept: a
// year-old notification is refused, and a leaked one can't sign the person
// out again after they sign in with Apple once more (security review
// AUTH-M-3).
func TestAppleNotificationReplayEndToEnd(t *testing.T) {
	srv := socialtest.New(t)
	h := newApp(t, socialEnv(t), func(_ *gorbital.Config, a *Authenticator) {
		a.endpoints.Google, a.endpoints.Apple = srv.Endpoints(), srv.Endpoints()
	}).Handler()
	signIn := func() []string {
		t.Helper()
		nonce := fmt.Sprint(do(t, h, "POST", "/v1/auth/apple/nonce", "").json["nonce"])
		sum := sha256.Sum256([]byte(nonce))
		token := srv.IDToken(socialtest.Claims{Subject: "001.bob", Audience: "com.example.app", Email: "bob@icloud.com", Extra: map[string]any{"email_verified": "true"}, Nonce: hex.EncodeToString(sum[:])})
		r := do(t, h, "POST", "/v1/auth/apple/token", fmt.Sprintf(`{"id_token":%q,"nonce":%q}`, token, nonce))
		if r.code != http.StatusOK {
			t.Fatalf("POST /v1/auth/apple/token = %d %s", r.code, r.body)
		}
		return []string{"Authorization", "Bearer " + fmt.Sprint(r.json["token"])}
	}
	notify := func(payload string) response {
		return do(t, h, "POST", "/v1/auth/apple/notifications", fmt.Sprintf(`{"payload":%q}`, payload), "Origin", "https://appleid.apple.com", "Sec-Fetch-Site", "cross-site")
	}

	session := signIn()
	revoked := srv.Notification("com.example.web", "consent-revoked", "001.bob")
	if r := notify(revoked); r.code != http.StatusNoContent || do(t, h, "GET", "/v1/auth/me", "", session...).code != http.StatusUnauthorized {
		t.Fatalf("consent-revoked = %d %s, want 204 and the session ended", r.code, r.body)
	}
	session = signIn()
	if r := notify(revoked); r.code != http.StatusNoContent {
		t.Errorf("replayed notification = %d %s, want 204", r.code, r.body)
	}
	if r := do(t, h, "GET", "/v1/auth/me", "", session...); r.code != http.StatusOK {
		t.Errorf("session after a replayed notification = %d %s, want 200", r.code, r.body)
	}
	srv.Now = func() time.Time { return time.Now().AddDate(-1, 0, 0) }
	if r := notify(srv.Notification("com.example.web", "consent-revoked", "001.bob")); r.code != http.StatusUnauthorized || r.json["code"] != "invalid_social_token" {
		t.Errorf("year-old notification = %d %s, want 401 invalid_social_token", r.code, r.body)
	}
	if r := do(t, h, "GET", "/v1/auth/me", "", session...); r.code != http.StatusOK {
		t.Errorf("session after a year-old notification = %d %s, want 200", r.code, r.body)
	}
}

func TestSocialConfiguration(t *testing.T) {
	keyFile := appleKeyFile(t)
	badKey := filepath.Join(t.TempDir(), "bad.p8")
	_ = os.WriteFile(badKey, []byte("not a key"), 0o600)
	load := func(env map[string]string) (gorbital.Config, error) {
		base := map[string]string{"APP_ENV": "development", "AUTH_ENCRYPTION_KEYS": testEncryptionKeys}
		if env["APP_ENV"] == "production" {
			maps.Copy(base, socialProductionEnv)
		}
		for k, v := range env {
			base[k] = v
		}
		return loadSocialConfig(config.Source{Getenv: func(k string) string { return base[k] }, ReadFile: os.ReadFile})
	}
	apple := map[string]string{"APPLE_TEAM_ID": "TEAM123456", "APPLE_KEY_ID": "KEY1234567", "APPLE_PRIVATE_KEY_FILE": keyFile, "APPLE_SERVICES_ID": "com.example.web"}
	with := func(extra map[string]string) map[string]string {
		out := map[string]string{}
		for k, v := range apple {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{"nothing", map[string]string{}, ""},
		{"Google", map[string]string{"GOOGLE_CLIENT_ID": "web", "GOOGLE_CLIENT_SECRET": "s"}, ""},
		{"Google without the secret", map[string]string{"GOOGLE_CLIENT_ID": "web"}, "GOOGLE_CLIENT_SECRET is required"},
		{"Google iOS without the web client", map[string]string{"GOOGLE_IOS_CLIENT_ID": "ios"}, "GOOGLE_CLIENT_ID is required"},
		{"Apple", apple, ""},
		{"Apple without the key", with(map[string]string{"APPLE_PRIVATE_KEY_FILE": ""}), "sign-in with Apple also needs APPLE_PRIVATE_KEY_FILE"},
		{"Apple without clients", with(map[string]string{"APPLE_SERVICES_ID": ""}), "APPLE_SERVICES_ID or APPLE_BUNDLE_IDS"},
		{"Apple with a bad key", with(map[string]string{"APPLE_PRIVATE_KEY_FILE": badKey}), "APPLE_PRIVATE_KEY_FILE"},
		{"production without the public URL", map[string]string{"APP_ENV": "production", "GOOGLE_CLIENT_ID": "web", "GOOGLE_CLIENT_SECRET": "s"}, "APP_PUBLIC_URL is required"},
		{"production over http", map[string]string{"APP_ENV": "production", "APP_PUBLIC_URL": "http://api.example.com", "GOOGLE_CLIENT_ID": "web", "GOOGLE_CLIENT_SECRET": "s"}, "must use https"},
		{"public URL with a path", map[string]string{"APP_PUBLIC_URL": "https://api.example.com/v1", "GOOGLE_CLIENT_ID": "web", "GOOGLE_CLIENT_SECRET": "s"}, "must be a scheme and host"},
		{"GitHub", map[string]string{"GITHUB_CLIENT_ID": "id", "GITHUB_CLIENT_SECRET": "s"}, ""},
		{"GitHub without the secret", map[string]string{"GITHUB_CLIENT_ID": "id"}, "GITHUB_CLIENT_SECRET is required with GITHUB_CLIENT_ID"},
		{"GitHub without the client ID", map[string]string{"GITHUB_CLIENT_SECRET": "s"}, "GITHUB_CLIENT_ID is required with GITHUB_CLIENT_SECRET"},
		{"GitHub in production without the public URL", map[string]string{"APP_ENV": "production", "GITHUB_CLIENT_ID": "id", "GITHUB_CLIENT_SECRET": "s", "AUTH_DEFAULT_RETURN_TO": "https://api.example.com/"}, "APP_PUBLIC_URL is required"},
		{"production without a default return address", map[string]string{"APP_ENV": "production", "APP_PUBLIC_URL": "https://api.example.com", "GOOGLE_CLIENT_ID": "web", "GOOGLE_CLIENT_SECRET": "s"}, "AUTH_DEFAULT_RETURN_TO is required"},
		{"production GitHub without a default return address", map[string]string{"APP_ENV": "production", "APP_PUBLIC_URL": "https://api.example.com", "GITHUB_CLIENT_ID": "id", "GITHUB_CLIENT_SECRET": "s"}, "AUTH_DEFAULT_RETURN_TO is required"},
		{"production with a default return address", map[string]string{"APP_ENV": "production", "APP_PUBLIC_URL": "https://api.example.com", "GOOGLE_CLIENT_ID": "web", "GOOGLE_CLIENT_SECRET": "s", "APP_CORS_ORIGINS": "https://app.example.com", "AUTH_DEFAULT_RETURN_TO": "https://app.example.com/signed-in"}, ""},
		{"default return address on the API", map[string]string{"APP_ENV": "production", "APP_PUBLIC_URL": "https://api.example.com", "GOOGLE_CLIENT_ID": "web", "GOOGLE_CLIENT_SECRET": "s", "AUTH_DEFAULT_RETURN_TO": "https://API.example.com/welcome"}, ""},
		{"default return address on another site", map[string]string{"APP_ENV": "production", "APP_PUBLIC_URL": "https://api.example.com", "GOOGLE_CLIENT_ID": "web", "GOOGLE_CLIENT_SECRET": "s", "APP_CORS_ORIGINS": "https://app.example.com", "AUTH_DEFAULT_RETURN_TO": "https://evil.example/"}, "must be on APP_PUBLIC_URL or an origin in APP_CORS_ORIGINS"},
		{"default return address over http in production", map[string]string{"APP_ENV": "production", "APP_PUBLIC_URL": "https://api.example.com", "GOOGLE_CLIENT_ID": "web", "GOOGLE_CLIENT_SECRET": "s", "APP_CORS_ORIGINS": "https://app.example.com", "AUTH_DEFAULT_RETURN_TO": "http://app.example.com/"}, "must use https in production"},
		{"default return address with a fragment", map[string]string{"AUTH_DEFAULT_RETURN_TO": "http://localhost:8080/#x"}, "without user information or a fragment"},
		{"default return address with user information", map[string]string{"AUTH_DEFAULT_RETURN_TO": "http://user@localhost:8080/"}, "without user information or a fragment"},
		{"relative default return address", map[string]string{"AUTH_DEFAULT_RETURN_TO": "/welcome"}, "must be an absolute"},
		{"development without docs or a default return address", map[string]string{"APP_DOCS_ENABLED": "false", "GITHUB_CLIENT_ID": "id", "GITHUB_CLIENT_SECRET": "s"}, "AUTH_DEFAULT_RETURN_TO is required with Google, Apple or GitHub sign-in when APP_DOCS_ENABLED=false"},
		{"production without web sign-in", map[string]string{"APP_ENV": "production"}, ""},
		{"production with Apple in iOS apps only", map[string]string{"APP_ENV": "production", "APPLE_TEAM_ID": "TEAM123456", "APPLE_KEY_ID": "KEY1234567", "APPLE_PRIVATE_KEY_FILE": keyFile, "APPLE_BUNDLE_IDS": "com.example.app"}, ""},
	}
	for _, tt := range tests {
		_, err := load(tt.env)
		switch {
		case tt.wantErr == "" && err != nil:
			t.Errorf("%s: LoadConfig() error = %v", tt.name, err)
		case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
			t.Errorf("%s: LoadConfig() error = %v, want one containing %q", tt.name, err, tt.wantErr)
		}
	}

	if cfg, err := load(map[string]string{"GITHUB_CLIENT_ID": "id", "GITHUB_CLIENT_SECRET": "s"}); err != nil || cfg.Auth.DefaultReturnTo != "http://localhost:8080/docs" {
		t.Errorf("development default return address = %q, %v; want the API docs", cfg.Auth.DefaultReturnTo, err)
	}
	cfg, _ := load(map[string]string{"GOOGLE_CLIENT_ID": "web", "GOOGLE_CLIENT_SECRET": "s", "APPLE_TEAM_ID": "T", "APPLE_KEY_ID": "K", "APPLE_PRIVATE_KEY_FILE": keyFile, "APPLE_BUNDLE_IDS": "com.example.app"})
	var out bytes.Buffer
	writeSignInMethods(&out, cfg)
	for _, want := range []string{
		"✓ Google sign-in", "callback http://localhost:8080/v1/auth/google/callback", "– Google sign-in in iOS apps", "set GOOGLE_IOS_CLIENT_ID in .env",
		"– Apple sign-in ", "AUTH_PROVIDERS.md#apple-sign-in", "✓ Apple sign-in in iOS apps", "com.example.app",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("WriteSignInMethods() lacks %q:\n%s", want, out.String())
		}
	}
}

// gitHubEnv turns on GitHub sign-in, with a frontend origin to return to.
var gitHubEnv = map[string]string{
	"GITHUB_CLIENT_ID": "github-client", "GITHUB_CLIENT_SECRET": "github-secret", "APP_CORS_ORIGINS": "https://app.example.com",
}

// gitHubCallback finishes a GitHub flow whose start (a redirect or a link's
// JSON) sent the browser to GitHub at location, as GitHub would for user.
func gitHubCallback(t *testing.T, h http.Handler, srv *socialtest.Server, location, browser string, user socialtest.GitHubUser) response {
	t.Helper()
	u, err := url.Parse(location)
	if err != nil || !strings.HasPrefix(location, srv.URL+"/login/oauth/authorize") {
		t.Fatalf("GitHub location = %q, %v", location, err)
	}
	q := u.Query()
	code := srv.GitHubCode(user, q.Get("code_challenge"))
	return do(t, h, "GET", "/v1/auth/github/callback?code="+url.QueryEscape(code)+"&state="+url.QueryEscape(q.Get("state")), "",
		"Cookie", "__Host-oauth="+browser)
}

// TestGitHubSignInEndToEnd follows ADR-0059 over HTTP against a fake GitHub:
// a new account, a person without a verified email, an address with an
// account, linking while signed in bound to the browser and the session,
// unlinking and the sign-in methods status.
func TestGitHubSignInEndToEnd(t *testing.T) {
	srv := socialtest.New(t)
	a := newApp(t, gitHubEnv, func(_ *gorbital.Config, a *Authenticator) { a.endpoints.GitHub = srv.GitHubEndpoints() })
	h := a.Handler()
	after := "https://app.example.com/after"
	start := func() (location, browser string) {
		t.Helper()
		r := do(t, h, "GET", "/v1/auth/github/start?return_to="+url.QueryEscape(after), "")
		if r.code != http.StatusFound || cookieValue(r, "__Host-oauth") == "" {
			t.Fatalf("GET /v1/auth/github/start = %d %v %s", r.code, r.header, r.body)
		}
		return r.header.Get("Location"), cookieValue(r, "__Host-oauth")
	}
	octo := socialtest.GitHubUser{ID: 583231, Login: "octocat", Name: "The Octocat", Emails: []socialtest.GitHubEmail{{Email: "octocat@example.com", Primary: true, Verified: true}}}

	location, browser := start()
	if q, _ := url.Parse(location); q.Query().Get("scope") != "read:user user:email" || q.Query().Get("code_challenge_method") != "S256" {
		t.Errorf("GitHub authorization URL = %s", location)
	}
	signedIn := gitHubCallback(t, h, srv, location, browser, octo)
	if signedIn.code != http.StatusSeeOther || signedIn.header.Get("Location") != after || cookieValue(signedIn, "__Host-session") == "" {
		t.Fatalf("GET /v1/auth/github/callback = %d %q %v", signedIn.code, signedIn.header.Get("Location"), signedIn.header.Values("Set-Cookie"))
	}
	me := do(t, h, "GET", "/v1/auth/me", "", "Cookie", "__Host-session="+cookieValue(signedIn, "__Host-session"))
	if user, _ := me.json["user"].(map[string]any); me.code != http.StatusOK || user["email"] != "octocat@example.com" || user["has_password"] != false || user["email_verified"] != false {
		t.Errorf("GET /v1/auth/me after GitHub = %d %s, want an unverified account without a password", me.code, me.body)
	}

	// No verified primary email, and an address with an account.
	location, browser = start()
	if r := gitHubCallback(t, h, srv, location, browser, socialtest.GitHubUser{ID: 2, Login: "nobody"}); r.header.Get("Location") != after+"#error=social_email_unverified" || cookieValue(r, "__Host-session") != "" {
		t.Errorf("GitHub without a verified email = %q %v", r.header.Get("Location"), r.header.Values("Set-Cookie"))
	}
	ctx := actor.With(context.Background(), actor.System("test"))
	owner, err := a.Auth().CreateUser(ctx, "ada@gmail.com", testPassword, true)
	if err != nil {
		t.Fatal(err)
	}
	ada := socialtest.GitHubUser{ID: 1815, Login: "ada", Emails: []socialtest.GitHubEmail{{Email: "ada@gmail.com", Primary: true, Verified: true}}}
	location, browser = start()
	if r := gitHubCallback(t, h, srv, location, browser, ada); r.header.Get("Location") != after+"#error=social_link_required" || cookieValue(r, "__Host-session") != "" {
		t.Errorf("GitHub with the address of an account = %q %v", r.header.Get("Location"), r.header.Values("Set-Cookie"))
	}

	// The owner links GitHub while signed in.
	session := cookieHeader(t, do(t, h, "POST", "/v1/auth/login", fmt.Sprintf(`{"email":"ada@gmail.com","password":%q}`, testPassword)))
	link := func(body string, headers ...string) response {
		t.Helper()
		return do(t, h, "POST", "/v1/auth/github/link", body, headers...)
	}
	body := fmt.Sprintf(`{"password":%q,"return_to":"https://app.example.com/settings"}`, testPassword)
	if r := link(`{"password":"wrong password"}`, session...); r.code != http.StatusUnauthorized || r.json["code"] != "invalid_credentials" {
		t.Errorf("POST /v1/auth/github/link with a wrong password = %d %s", r.code, r.body)
	}
	if r := link(body); r.code != http.StatusUnauthorized {
		t.Errorf("POST /v1/auth/github/link signed out = %d %s, want 401", r.code, r.body)
	}
	if r := link(body, append(session, "Sec-Fetch-Site", "cross-site", "Origin", "https://evil.example")...); r.code != http.StatusForbidden {
		t.Errorf("cross-site POST /v1/auth/github/link = %d %s, want 403", r.code, r.body)
	}
	if r := do(t, h, "POST", "/v1/auth/google/link", body, session...); r.code != http.StatusUnprocessableEntity {
		t.Errorf("POST /v1/auth/google/link = %d %s, want 422 (GitHub only)", r.code, r.body)
	}

	started := link(body, session...)
	linkBrowser := cookieValue(started, "__Host-oauth")
	if started.code != http.StatusOK || linkBrowser == "" || started.header.Get("Cache-Control") != "no-store" || started.json["url"] == nil {
		t.Fatalf("POST /v1/auth/github/link = %d %v %s", started.code, started.header, started.body)
	}
	// Someone else's browser following the link's GitHub return gets nothing.
	if r := gitHubCallback(t, h, srv, fmt.Sprint(started.json["url"]), "attacker-browser", octo); r.header.Get("Location") != "https://app.example.com/settings#error=invalid_state" {
		t.Errorf("link callback in another browser = %q", r.header.Get("Location"))
	}
	started = link(body, session...)
	linked := gitHubCallback(t, h, srv, fmt.Sprint(started.json["url"]), cookieValue(started, "__Host-oauth"), ada)
	if linked.code != http.StatusSeeOther || linked.header.Get("Location") != "https://app.example.com/settings" || cookieValue(linked, "__Host-session") != "" {
		t.Fatalf("link callback = %d %q %v, want the return address and no new session", linked.code, linked.header.Get("Location"), linked.header.Values("Set-Cookie"))
	}
	identities := do(t, h, "GET", "/v1/auth/identities", "", session...)
	items, _ := identities.json["identities"].([]any)
	if identities.code != http.StatusOK || len(items) != 1 || items[0].(map[string]any)["provider"] != "github" {
		t.Fatalf("GET /v1/auth/identities = %d %s", identities.code, identities.body)
	}
	location, browser = start()
	viaGitHub := gitHubCallback(t, h, srv, location, browser, ada)
	me = do(t, h, "GET", "/v1/auth/me", "", "Cookie", "__Host-session="+cookieValue(viaGitHub, "__Host-session"))
	if user, _ := me.json["user"].(map[string]any); me.code != http.StatusOK || user["id"] != owner.ID {
		t.Errorf("GitHub sign-in after linking = %d %s, want the owner's account", me.code, me.body)
	}

	// Another account's GitHub, and a link whose session signed out.
	started = link(body, session...)
	if r := gitHubCallback(t, h, srv, fmt.Sprint(started.json["url"]), cookieValue(started, "__Host-oauth"), octo); r.header.Get("Location") != "https://app.example.com/settings#error=identity_in_use" {
		t.Errorf("linking another account's GitHub = %q", r.header.Get("Location"))
	}
	other := cookieHeader(t, do(t, h, "POST", "/v1/auth/login", fmt.Sprintf(`{"email":"ada@gmail.com","password":%q}`, testPassword)))
	started = link(body, other...)
	if r := do(t, h, "POST", "/v1/auth/logout", "", other...); r.code != http.StatusNoContent {
		t.Fatalf("POST /v1/auth/logout = %d %s", r.code, r.body)
	}
	if r := gitHubCallback(t, h, srv, fmt.Sprint(started.json["url"]), cookieValue(started, "__Host-oauth"), socialtest.GitHubUser{ID: 7, Login: "work", Emails: ada.Emails}); r.header.Get("Location") != "https://app.example.com/settings#error=unauthenticated" {
		t.Errorf("link after signing out = %q", r.header.Get("Location"))
	}

	// Unlinking.
	id := items[0].(map[string]any)["id"].(string)
	if r := do(t, h, "DELETE", "/v1/auth/identities/"+id, fmt.Sprintf(`{"password":%q}`, testPassword), session...); r.code != http.StatusNoContent {
		t.Errorf("DELETE /v1/auth/identities/%s = %d %s", id, r.code, r.body)
	}

	// The sign-in methods status.
	var out bytes.Buffer
	writeSignInMethods(&out, testConfig(t, gitHubEnv))
	if !strings.Contains(out.String(), "✓ GitHub sign-in") || !strings.Contains(out.String(), "callback http://localhost:8080/v1/auth/github/callback") {
		t.Errorf("WriteSignInMethods() with GitHub:\n%s", out.String())
	}
	out.Reset()
	writeSignInMethods(&out, testConfig(t, nil))
	if !strings.Contains(out.String(), "– GitHub sign-in") || !strings.Contains(out.String(), "set GITHUB_CLIENT_ID, GITHUB_CLIENT_SECRET in .env") || !strings.Contains(out.String(), "AUTH_PROVIDERS.md#github-sign-in") {
		t.Errorf("WriteSignInMethods() without GitHub:\n%s", out.String())
	}
}

// TestSignInReturnsToTheDefaultAddress: a web sign-in without return_to
// ends at AUTH_DEFAULT_RETURN_TO, which production requires (its /docs is
// off by default and answered 404), and at the API docs in development.
func TestSignInReturnsToTheDefaultAddress(t *testing.T) {
	srv := socialtest.New(t)
	production := map[string]string{
		"APP_ENV": "production", "APP_PUBLIC_URL": "https://api.example.com", "AUTH_DEFAULT_RETURN_TO": "https://app.example.com/signed-in",
	}
	maps.Copy(production, gitHubEnv)
	maps.Copy(production, socialProductionEnv)
	for name, tt := range map[string]struct {
		env  map[string]string
		want string
	}{
		"production":  {production, "https://app.example.com/signed-in"},
		"development": {gitHubEnv, "http://localhost:8080/docs"},
	} {
		h := newSocialProductionApp(t, tt.env, func(_ *gorbital.Config, a *Authenticator) { a.endpoints.GitHub = srv.GitHubEndpoints() })
		r := do(t, h, "GET", "/v1/auth/github/start", "")
		location := r.header.Get("Location")
		signedIn := gitHubCallback(t, h, srv, location, cookieValue(r, "__Host-oauth"), socialtest.GitHubUser{ID: 1, Login: "a", Emails: []socialtest.GitHubEmail{{Email: name + "@example.com", Primary: true, Verified: true}}})
		if signedIn.header.Get("Location") != tt.want || cookieValue(signedIn, "__Host-session") == "" {
			t.Errorf("%s: sign-in without return_to = %q %v, want %q", name, signedIn.header.Get("Location"), signedIn.header.Values("Set-Cookie"), tt.want)
		}
		if r := do(t, h, "GET", "/v1/auth/github/callback?code=x&state=unknown", ""); r.header.Get("Location") != tt.want+"#error=invalid_state" {
			t.Errorf("%s: callback with an unknown state = %q, want %q", name, r.header.Get("Location"), tt.want+"#error=invalid_state")
		}
		wantDocs := http.StatusOK
		if name == "production" {
			wantDocs = http.StatusNotFound
		}
		if r := do(t, h, "GET", "/docs", ""); r.code != wantDocs {
			t.Errorf("%s: GET /docs = %d, want %d", name, r.code, wantDocs)
		}
	}
}
