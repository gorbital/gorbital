package app_test

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"testing"
	"time"

	"gorbital.dev/actor"
	authlib "gorbital.dev/modules/auth"
)

// testEncryptionKeys is AUTH_ENCRYPTION_KEYS for every test app in this
// process, so two instances on one database share them.
var testEncryptionKeys = authlib.NewKeyringKey("test")

// signInWithTOTP signs in to an account with two-factor authentication and
// returns a bearer token for a session verified with a second factor.
func signInWithTOTP(t *testing.T, h http.Handler, email, password, secret string) string {
	t.Helper()
	r := do(t, h, "POST", "/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, email, password))
	mfa, _ := r.json["mfa"].(map[string]any)
	challenge, _ := mfa["challenge_token"].(string)
	if r.code != http.StatusAccepted || challenge == "" {
		t.Fatalf("POST /v1/auth/login as %s = %d %s, want 202 with a challenge", email, r.code, r.body)
	}
	code, err := authlib.TOTPCode(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	r = do(t, h, "POST", "/v1/auth/login/mfa", fmt.Sprintf(`{"challenge_token":%q,"code":%q,"transport":"bearer"}`, challenge, code))
	token, _ := r.json["token"].(string)
	if r.code != http.StatusOK || token == "" {
		t.Fatalf("POST /v1/auth/login/mfa as %s = %d %s", email, r.code, r.body)
	}
	return token
}

// TestTwoFactorEndToEnd follows ADR-0043 over HTTP: an ops role's
// permissions wait for two-factor authentication, which the user turns on,
// then signs in with a second factor.
func TestTwoFactorEndToEnd(t *testing.T) {
	a := newApp(t, nil)
	h := a.Handler()
	ctx := actor.With(context.Background(), actor.System("test"))
	const email = "ops@example.com"
	u, err := a.Auth().CreateUser(ctx, email, testPassword, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Auth().GrantRole(ctx, u.ID, "ops_viewer"); err != nil {
		t.Fatal(err)
	}
	login := fmt.Sprintf(`{"email":%q,"password":%q,"transport":"bearer"}`, email, testPassword)

	// Signed in with only a password, the role's permissions wait.
	r := do(t, h, "POST", "/v1/auth/login", login)
	token, _ := r.json["token"].(string)
	if r.code != http.StatusOK || token == "" {
		t.Fatalf("POST /v1/auth/login = %d %s", r.code, r.body)
	}
	bearer := []string{"Authorization", "Bearer " + token}
	if r := do(t, h, "GET", "/ops/settings", "", bearer...); r.code != http.StatusForbidden || r.json["code"] != "mfa_required" {
		t.Errorf("GET /ops/settings without 2FA = %d %s, want 403 mfa_required", r.code, r.body)
	}
	me := do(t, h, "GET", "/v1/auth/me", "", bearer...)
	if stepUp, _ := me.json["step_up_permissions"].([]any); me.json["mfa_required"] != true || me.json["mfa_enabled"] != false || !slices.Contains(stepUp, any("ops.settings.read")) {
		t.Errorf("GET /v1/auth/me without 2FA = %s", me.body)
	}

	// Turn two-factor authentication on.
	if r := do(t, h, "POST", "/v1/auth/mfa/totp", `{"password":"wrong password here"}`, bearer...); r.code != http.StatusUnauthorized || r.json["code"] != "invalid_credentials" {
		t.Errorf("POST /v1/auth/mfa/totp with a wrong password = %d %s", r.code, r.body)
	}
	start := do(t, h, "POST", "/v1/auth/mfa/totp", fmt.Sprintf(`{"password":%q}`, testPassword), bearer...)
	secret, _ := start.json["secret"].(string)
	if start.code != http.StatusOK || secret == "" || start.json["uri"] == nil {
		t.Fatalf("POST /v1/auth/mfa/totp = %d %s", start.code, start.body)
	}
	code, _ := authlib.TOTPCode(secret, time.Now())
	n, _ := strconv.Atoi(code)
	if r := do(t, h, "POST", "/v1/auth/mfa/totp/confirm", fmt.Sprintf(`{"code":"%06d"}`, (n+1)%1_000_000), bearer...); r.code != http.StatusUnauthorized || r.json["code"] != "invalid_mfa" {
		t.Errorf("POST /v1/auth/mfa/totp/confirm with a wrong code = %d %s", r.code, r.body)
	}
	confirm := do(t, h, "POST", "/v1/auth/mfa/totp/confirm", fmt.Sprintf(`{"code":%q}`, code), bearer...)
	codes, _ := confirm.json["recovery_codes"].([]any)
	if confirm.code != http.StatusOK || len(codes) != authlib.RecoveryCodeCount {
		t.Fatalf("POST /v1/auth/mfa/totp/confirm = %d %s", confirm.code, confirm.body)
	}
	// Confirming verified this session.
	if r := do(t, h, "GET", "/ops/settings", "", bearer...); r.code != http.StatusOK {
		t.Errorf("GET /ops/settings after turning 2FA on = %d %s", r.code, r.body)
	}
	if r := do(t, h, "DELETE", "/v1/auth/mfa/totp", fmt.Sprintf(`{"password":%q,"recovery_code":%q}`, testPassword, codes[1]), bearer...); r.code != http.StatusConflict || r.json["code"] != "mfa_required_by_role" {
		t.Errorf("DELETE /v1/auth/mfa/totp with an ops role = %d %s", r.code, r.body)
	}

	// Signing in again needs a second factor: the code just used is spent, a
	// recovery code works once.
	challenge := func() string {
		t.Helper()
		r := do(t, h, "POST", "/v1/auth/login", login)
		mfa, _ := r.json["mfa"].(map[string]any)
		if r.code != http.StatusAccepted || mfa == nil || r.json["token"] != nil || r.json["session"] != nil {
			t.Fatalf("POST /v1/auth/login with 2FA = %d %s, want 202 with only a challenge", r.code, r.body)
		}
		return mfa["challenge_token"].(string)
	}
	first := challenge()
	if r := do(t, h, "POST", "/v1/auth/login/mfa", fmt.Sprintf(`{"challenge_token":%q,"code":%q}`, first, code)); r.code != http.StatusUnauthorized || r.json["code"] != "invalid_mfa" {
		t.Errorf("POST /v1/auth/login/mfa with a used code = %d %s", r.code, r.body)
	}
	r = do(t, h, "POST", "/v1/auth/login/mfa", fmt.Sprintf(`{"challenge_token":%q,"recovery_code":%q,"transport":"bearer"}`, first, codes[0]))
	session, _ := r.json["session"].(map[string]any)
	verifiedToken, _ := r.json["token"].(string)
	if r.code != http.StatusOK || verifiedToken == "" || session["mfa_verified"] != true {
		t.Fatalf("POST /v1/auth/login/mfa with a recovery code = %d %s", r.code, r.body)
	}
	if r := do(t, h, "POST", "/v1/auth/login/mfa", fmt.Sprintf(`{"challenge_token":%q,"recovery_code":%q}`, challenge(), codes[0])); r.code != http.StatusUnauthorized {
		t.Errorf("POST /v1/auth/login/mfa with a used recovery code = %d %s", r.code, r.body)
	}

	verified := []string{"Authorization", "Bearer " + verifiedToken}
	events := do(t, h, "GET", "/ops/audit?action=auth.mfa.recovery_code_used", "", verified...)
	if list, _ := events.json["events"].([]any); events.code != http.StatusOK || len(list) != 1 {
		t.Errorf("GET /ops/audit?action=auth.mfa.recovery_code_used = %d %s, want one event", events.code, events.body)
	}
}
