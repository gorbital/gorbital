package authhttp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"strings"
	"testing"

	"gorbital.dev/actor"
	"gorbital.dev/config"
	"gorbital.dev/gorbital"
	"gorbital.dev/modules/auth/passkey"
	"gorbital.dev/modules/auth/passkey/passkeytest"
)

// devOrigin is the browser origin passkeys use in development (ADR-0044).
const devOrigin = "http://localhost:8080"

var androidFingerprint = passkey.FormatFingerprint(sha256.Sum256([]byte("test signing certificate")))

// optionsOf returns the passkey options of a ceremony response as JSON.
func optionsOf(t *testing.T, r response) []byte {
	t.Helper()
	options, err := json.Marshal(r.json["options"])
	if err != nil || r.json["ceremony_token"] == nil {
		t.Fatalf("ceremony response = %d %s", r.code, r.body)
	}
	return options
}

// TestPasskeysEndToEnd follows ADR-0044 over HTTP: add a passkey, sign in
// with it alone, use it as a second factor, and manage it.
func TestPasskeysEndToEnd(t *testing.T) {
	a := newApp(t, map[string]string{
		"WEBAUTHN_APPLE_APP_IDS": "ABCDE12345.com.example.app",
		"WEBAUTHN_ANDROID_APPS":  "com.example.app=SHA256:" + androidFingerprint,
	})
	h := a.Handler()
	const email = "ada@example.com"
	if _, err := a.Auth().CreateUser(actor.With(context.Background(), actor.System("test")), email, testPassword, true); err != nil {
		t.Fatal(err)
	}
	login := do(t, h, "POST", "/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":%q,"transport":"bearer"}`, email, testPassword))
	bearer := []string{"Authorization", "Bearer " + login.json["token"].(string)}

	// Add a passkey.
	if r := do(t, h, "POST", "/v1/auth/passkeys/registration", `{"password":"wrong password here"}`, bearer...); r.code != http.StatusUnauthorized || r.json["code"] != "invalid_credentials" {
		t.Errorf("POST /v1/auth/passkeys/registration with a wrong password = %d %s", r.code, r.body)
	}
	begin := do(t, h, "POST", "/v1/auth/passkeys/registration", fmt.Sprintf(`{"password":%q}`, testPassword), bearer...)
	laptop := passkeytest.New(devOrigin)
	credential, err := laptop.Create(optionsOf(t, begin))
	if err != nil {
		t.Fatal(err)
	}
	created := do(t, h, "POST", "/v1/auth/passkeys", fmt.Sprintf(`{"ceremony_token":%q,"name":"Laptop","credential":%s}`, begin.json["ceremony_token"], credential), bearer...)
	pk, _ := created.json["passkey"].(map[string]any)
	codes, _ := created.json["recovery_codes"].([]any)
	if created.code != http.StatusCreated || pk["name"] != "Laptop" || len(codes) != 10 {
		t.Fatalf("POST /v1/auth/passkeys = %d %s", created.code, created.body)
	}

	// Sign in with the passkey alone; a ceremony works once.
	options := do(t, h, "POST", "/v1/auth/passkeys/login/options", "")
	assertion, _ := laptop.Get(optionsOf(t, options))
	body := fmt.Sprintf(`{"ceremony_token":%q,"credential":%s,"transport":"bearer"}`, options.json["ceremony_token"], assertion)
	signedIn := do(t, h, "POST", "/v1/auth/passkeys/login", body)
	session, _ := signedIn.json["session"].(map[string]any)
	if signedIn.code != http.StatusOK || signedIn.json["token"] == nil || session["mfa_verified"] != true {
		t.Fatalf("POST /v1/auth/passkeys/login = %d %s", signedIn.code, signedIn.body)
	}
	if r := do(t, h, "POST", "/v1/auth/passkeys/login", body); r.code != http.StatusUnauthorized || r.json["code"] != "invalid_passkey" {
		t.Errorf("POST /v1/auth/passkeys/login with a used ceremony = %d %s", r.code, r.body)
	}

	// A password sign-in asks for a second factor, which the passkey gives.
	started := do(t, h, "POST", "/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, email, testPassword))
	mfa, _ := started.json["mfa"].(map[string]any)
	if started.code != http.StatusAccepted || fmt.Sprint(mfa["methods"]) != "[passkey recovery_code]" {
		t.Fatalf("POST /v1/auth/login with a passkey = %d %s", started.code, started.body)
	}
	second := do(t, h, "POST", "/v1/auth/login/mfa/passkey", fmt.Sprintf(`{"challenge_token":%q}`, mfa["challenge_token"]))
	assertion, _ = laptop.Get(optionsOf(t, second))
	finished := do(t, h, "POST", "/v1/auth/login/mfa", fmt.Sprintf(`{"challenge_token":%q,"passkey":{"ceremony_token":%q,"credential":%s},"transport":"bearer"}`,
		mfa["challenge_token"], second.json["ceremony_token"], assertion))
	if finished.code != http.StatusOK || finished.json["token"] == nil {
		t.Fatalf("POST /v1/auth/login/mfa with a passkey = %d %s", finished.code, finished.body)
	}
	verified := []string{"Authorization", "Bearer " + finished.json["token"].(string)}

	// A signed-in user confirms a change with a passkey.
	check := do(t, h, "POST", "/v1/auth/passkeys/verification", "", verified...)
	assertion, _ = laptop.Get(optionsOf(t, check))
	regenerated := do(t, h, "POST", "/v1/auth/mfa/recovery-codes", fmt.Sprintf(`{"passkey":{"ceremony_token":%q,"credential":%s}}`, check.json["ceremony_token"], assertion), verified...)
	if fresh, _ := regenerated.json["recovery_codes"].([]any); regenerated.code != http.StatusOK || len(fresh) != 10 {
		t.Errorf("POST /v1/auth/mfa/recovery-codes with a passkey = %d %s", regenerated.code, regenerated.body)
	}

	// Manage passkeys.
	list := do(t, h, "GET", "/v1/auth/passkeys", "", verified...)
	if items, _ := list.json["passkeys"].([]any); list.code != http.StatusOK || len(items) != 1 {
		t.Errorf("GET /v1/auth/passkeys = %d %s", list.code, list.body)
	}
	id := pk["id"].(string)
	if r := do(t, h, "PATCH", "/v1/auth/passkeys/"+id, `{"name":"Work laptop"}`, verified...); r.code != http.StatusNoContent {
		t.Errorf("PATCH /v1/auth/passkeys/{id} = %d %s", r.code, r.body)
	}
	if r := do(t, h, "DELETE", "/v1/auth/passkeys/pky_unknown", "", verified...); r.code != http.StatusNotFound || r.json["code"] != "passkey_not_found" {
		t.Errorf("DELETE unknown passkey = %d %s", r.code, r.body)
	}
	if r := do(t, h, "DELETE", "/v1/auth/passkeys/"+id, "", verified...); r.code != http.StatusNoContent {
		t.Errorf("DELETE /v1/auth/passkeys/{id} from a verified session = %d %s", r.code, r.body)
	}

	// Native apps' association files.
	aasa := do(t, h, "GET", "/.well-known/apple-app-site-association", "")
	if aasa.code != http.StatusOK || aasa.header.Get("Content-Type") != "application/json" || !strings.Contains(aasa.body, "ABCDE12345.com.example.app") {
		t.Errorf("GET apple-app-site-association = %d %q %s", aasa.code, aasa.header.Get("Content-Type"), aasa.body)
	}
	links := do(t, h, "GET", "/.well-known/assetlinks.json", "")
	if links.code != http.StatusOK || !strings.Contains(links.body, "com.example.app") || !strings.Contains(links.body, androidFingerprint) {
		t.Errorf("GET assetlinks.json = %d %s", links.code, links.body)
	}
}

// TestWellKnownFilesNeedConfiguration checks the /.well-known files, which
// sign-in serves through gorbital.AuthSetup.Handle, are absent when no
// native app is configured.
func TestWellKnownFilesNeedConfiguration(t *testing.T) {
	h := newApp(t, nil).Handler()
	for _, path := range []string{"/.well-known/apple-app-site-association", "/.well-known/assetlinks.json"} {
		if r := do(t, h, "GET", path, ""); r.code != http.StatusNotFound {
			t.Errorf("GET %s without native apps = %d, want 404", path, r.code)
		}
	}
}

// passkeyProductionEnv is what gorbital.LoadConfig requires in production
// beside sign-in's variables, as the golden app's mailProviderEnv: the mail
// provider and file storage off this machine (ADR-0075).
var passkeyProductionEnv = map[string]string{"RESEND_API_KEY": "re_123",
	"STORAGE_DRIVER": "s3", "STORAGE_REGION": "eu-west-1", "STORAGE_BUCKET": "files", "STORAGE_ACCESS_KEY": "AKIA", "STORAGE_SECRET_KEY": "secret"}

// loadConfigWithSignIn loads a configuration as a v0.1 app's LoadConfig
// did: gorbital.LoadConfig and the checks it leaves to sign-in
// (Authenticator.CheckConfig), with their errors joined.
func loadConfigWithSignIn(env map[string]string) (gorbital.Config, error) {
	cfg, err := gorbital.LoadConfig(config.Source{Getenv: func(k string) string { return env[k] }, ReadFile: os.ReadFile})
	return cfg, errors.Join(err, New().CheckConfig(cfg))
}

func TestWebAuthnConfiguration(t *testing.T) {
	load := func(env map[string]string) (gorbital.Config, error) {
		base := map[string]string{"APP_ENV": "development", "AUTH_ENCRYPTION_KEYS": testEncryptionKeys}
		if env["APP_ENV"] == "production" {
			maps.Copy(base, passkeyProductionEnv)
		}
		for k, v := range env {
			base[k] = v
		}
		return loadConfigWithSignIn(base)
	}
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{"development defaults to localhost", map[string]string{}, ""},
		{"production without passkeys", map[string]string{"APP_ENV": "production"}, ""},
		{"production with https", map[string]string{"APP_ENV": "production", "WEBAUTHN_RP_ID": "example.com", "WEBAUTHN_ORIGINS": "https://app.example.com"}, ""},
		{"production over http", map[string]string{"APP_ENV": "production", "WEBAUTHN_RP_ID": "localhost", "WEBAUTHN_ORIGINS": "http://localhost:8080"}, "must use https in production"},
		{"RP ID without origins", map[string]string{"WEBAUTHN_RP_ID": "example.com"}, "WEBAUTHN_ORIGINS is required"},
		{"origins without RP ID", map[string]string{"APP_ENV": "production", "WEBAUTHN_ORIGINS": "https://app.example.com"}, "WEBAUTHN_RP_ID is required"},
		{"origin on another domain", map[string]string{"WEBAUTHN_RP_ID": "example.com", "WEBAUTHN_ORIGINS": "https://example.net"}, "isn't on the relying party ID"},
		{"iOS apps without RP ID in production", map[string]string{"APP_ENV": "production", "WEBAUTHN_APPLE_APP_IDS": "ABCDE12345.com.example.app"}, "WEBAUTHN_RP_ID is required"},
		{"bad Apple app ID", map[string]string{"WEBAUTHN_APPLE_APP_IDS": "com.example.app"}, "WEBAUTHN_APPLE_APP_IDS"},
		{"bad Android fingerprint", map[string]string{"WEBAUTHN_ANDROID_APPS": "com.example.app=SHA256:AB"}, "WEBAUTHN_ANDROID_APPS"},
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

	// The sign-in methods block names what's missing (ADR-0045).
	cfg, _ := load(map[string]string{"WEBAUTHN_APPLE_APP_IDS": "ABCDE12345.com.example.app"})
	var out bytes.Buffer
	writeSignInMethods(&out, cfg)
	for _, want := range []string{
		"Sign-in methods", "✓ Email and password", "✓ Authenticator apps (2FA)", "✓ Passkeys in browsers", "RP ID localhost",
		"✓ Passkeys in iOS apps", "ABCDE12345.com.example.app",
		"– Passkeys in Android apps", "set WEBAUTHN_ANDROID_APPS in .env", "AUTH_PROVIDERS.md#passkeys-in-android-apps",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("WriteSignInMethods() lacks %q:\n%s", want, out.String())
		}
	}
	noKeys, _ := load(map[string]string{"APP_ENV": "production", "AUTH_ENCRYPTION_KEYS": testEncryptionKeys})
	out.Reset()
	writeSignInMethods(&out, noKeys)
	if !strings.Contains(out.String(), "– Passkeys in browsers") || !strings.Contains(out.String(), "set WEBAUTHN_RP_ID, WEBAUTHN_ORIGINS in .env") {
		t.Errorf("WriteSignInMethods() in production without passkeys:\n%s", out.String())
	}
}
