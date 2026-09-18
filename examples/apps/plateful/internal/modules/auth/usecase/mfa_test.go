package usecase_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"gorbital.dev/actor"
	authlib "gorbital.dev/modules/auth"

	authdomain "example.com/plateful/internal/modules/auth/domain"
	authrepository "example.com/plateful/internal/modules/auth/repository"
	authusecase "example.com/plateful/internal/modules/auth/usecase"
)

var (
	testKeySpec = authlib.NewKeyringKey("test")
	testKeyring = mustKeyring(testKeySpec)
)

func mustKeyring(spec string) *authlib.Keyring {
	k, err := authlib.ParseKeyring(spec)
	if err != nil {
		panic(err)
	}
	return k
}

// withKeys configures encryption keys, which two-factor authentication needs.
func withKeys(c *authusecase.Config) { c.Keyring = testKeyring }

// totpCode returns the authenticator app's code for secret at the fixture's
// time.
func (f *fixture) totpCode(t *testing.T, secret string) string {
	t.Helper()
	code, err := authlib.TOTPCode(secret, f.clock.now())
	if err != nil {
		t.Fatal(err)
	}
	return code
}

// enroll turns two-factor authentication on for the signed-in user of ctx
// and returns the secret and recovery codes.
func (f *fixture) enroll(t *testing.T, ctx context.Context) (string, []string) {
	t.Helper()
	e, err := f.svc.StartTOTPEnrollment(ctx, password)
	if err != nil {
		t.Fatalf("StartTOTPEnrollment() error = %v", err)
	}
	codes, err := f.svc.ConfirmTOTP(ctx, f.totpCode(t, e.Secret))
	if err != nil {
		t.Fatalf("ConfirmTOTP() error = %v", err)
	}
	return e.Secret, codes
}

// principalCtx returns a request context for token as the middleware would
// build it now.
func (f *fixture) principalCtx(t *testing.T, token string) context.Context {
	t.Helper()
	p, err := f.svc.Authenticate(context.Background(), token)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	return authlib.WithPrincipal(requestCtx(), p)
}

// wrongCode returns a 6-digit code other than code.
func wrongCode(code string) string {
	n, _ := strconv.Atoi(code)
	return fmt.Sprintf("%06d", (n+1)%1_000_000)
}

func TestTOTPEnrollmentAndSignIn(t *testing.T) {
	f := newFixture(t, withKeys)
	f.signUp(t, "ada@example.com")
	ctx, current := f.login(t, "ada@example.com")
	_, other := f.login(t, "ada@example.com")

	if _, err := f.svc.StartTOTPEnrollment(ctx, "wrong password here"); !errors.Is(err, authdomain.ErrInvalidCredentials) {
		t.Errorf("StartTOTPEnrollment(wrong password) error = %v", err)
	}
	if _, err := f.svc.ConfirmTOTP(ctx, "123456"); !errors.Is(err, authdomain.ErrMFANotEnabled) {
		t.Errorf("ConfirmTOTP() before setup error = %v", err)
	}
	e, err := f.svc.StartTOTPEnrollment(ctx, password)
	if err != nil || len(e.Secret) != 32 || !strings.HasPrefix(e.URI, "otpauth://totp/app:") || !strings.Contains(e.URI, "secret="+e.Secret) {
		t.Fatalf("StartTOTPEnrollment() = %+v, %v", e, err)
	}
	var stored []byte
	if err := f.pool.QueryRow(ctx, `SELECT secret_ciphertext FROM auth_totp`).Scan(&stored); err != nil || strings.Contains(string(stored), e.Secret) {
		t.Errorf("stored secret = %q, %v; want it encrypted", stored, err)
	}
	if _, err := f.svc.ConfirmTOTP(ctx, wrongCode(f.totpCode(t, e.Secret))); !errors.Is(err, authdomain.ErrInvalidMFA) {
		t.Errorf("ConfirmTOTP(wrong code) error = %v", err)
	}
	codes, err := f.svc.ConfirmTOTP(ctx, f.totpCode(t, e.Secret))
	if err != nil || len(codes) != authlib.RecoveryCodeCount || f.emails.count("mfa_enabled") != 1 {
		t.Fatalf("ConfirmTOTP() = %v, %v", codes, err)
	}
	if _, err := f.svc.StartTOTPEnrollment(ctx, password); !errors.Is(err, authdomain.ErrMFAAlreadyEnabled) {
		t.Errorf("StartTOTPEnrollment() when on error = %v", err)
	}
	if p, err := f.svc.Authenticate(context.Background(), current.Token); err != nil || !p.MFAVerified {
		t.Errorf("current session after confirming = %+v, %v; want verified with a second factor", p, err)
	}
	if _, err := f.svc.Authenticate(context.Background(), other.Token); !errors.Is(err, authlib.ErrUnauthenticated) {
		t.Errorf("other session after confirming error = %v, want ended", err)
	}

	// Signing in now needs a second factor.
	started, err := f.svc.Login(requestCtx(), "ada@example.com", password)
	if err != nil || started.Challenge == nil || started.Token != "" || len(started.Challenge.Methods) != 2 {
		t.Fatalf("Login() with 2FA = %+v, %v; want a challenge and no session", started, err)
	}
	// The code confirmed a moment ago can't be used again.
	if _, err := f.svc.LoginMFA(requestCtx(), started.Challenge.Token, authdomain.SecondFactor{Code: f.totpCode(t, e.Secret)}); !errors.Is(err, authdomain.ErrInvalidMFA) {
		t.Errorf("LoginMFA(reused code) error = %v", err)
	}
	f.clock.advance(30 * time.Second)
	done, err := f.svc.LoginMFA(requestCtx(), started.Challenge.Token, authdomain.SecondFactor{Code: f.totpCode(t, e.Secret)})
	if err != nil || done.Token == "" || !done.Session.MFAVerified() {
		t.Fatalf("LoginMFA() = %+v, %v", done, err)
	}
	if _, err := f.svc.LoginMFA(requestCtx(), started.Challenge.Token, authdomain.SecondFactor{Code: "123456"}); !errors.Is(err, authdomain.ErrInvalidMFA) {
		t.Errorf("LoginMFA(finished challenge) error = %v", err)
	}

	// A recovery code works once, in any case and spacing.
	started, _ = f.svc.Login(requestCtx(), "ada@example.com", password)
	if _, err := f.svc.LoginMFA(requestCtx(), started.Challenge.Token, authdomain.SecondFactor{RecoveryCode: strings.ToUpper(codes[0])}); err != nil {
		t.Fatalf("LoginMFA(recovery code) error = %v", err)
	}
	if sent := f.emails.last(t, "recovery_used"); sent.remaining != authlib.RecoveryCodeCount-1 {
		t.Errorf("recovery code email remaining = %d", sent.remaining)
	}
	started, _ = f.svc.Login(requestCtx(), "ada@example.com", password)
	if _, err := f.svc.LoginMFA(requestCtx(), started.Challenge.Token, authdomain.SecondFactor{RecoveryCode: codes[0]}); !errors.Is(err, authdomain.ErrInvalidMFA) {
		t.Errorf("LoginMFA(used recovery code) error = %v", err)
	}

	if !hasAll(f.audit.actions(), "auth.mfa.totp_enrollment_started", "auth.mfa.totp_enabled", "auth.mfa.challenge_succeeded", "auth.mfa.challenge_failed", "auth.mfa.recovery_code_used") {
		t.Errorf("audit actions = %v", f.audit.actions())
	}
	for _, ev := range f.audit.events {
		if meta := fmt.Sprint(ev.Metadata); strings.Contains(meta, e.Secret) || strings.Contains(meta, codes[1]) {
			t.Errorf("audit event %s metadata %s holds a secret or recovery code", ev.Action, meta)
		}
	}
}

func TestMFAChallengeLimits(t *testing.T) {
	f := newFixture(t, withKeys)
	f.signUp(t, "ada@example.com")
	ctx, _ := f.login(t, "ada@example.com")
	secret, _ := f.enroll(t, ctx)
	f.clock.advance(30 * time.Second)

	started, _ := f.svc.Login(requestCtx(), "ada@example.com", password)
	right := f.totpCode(t, secret)
	for range authlib.MFAChallengeMaxAttempts {
		if _, err := f.svc.LoginMFA(requestCtx(), started.Challenge.Token, authdomain.SecondFactor{Code: wrongCode(right)}); !errors.Is(err, authdomain.ErrInvalidMFA) {
			t.Fatalf("LoginMFA(wrong code) error = %v", err)
		}
	}
	if _, err := f.svc.LoginMFA(requestCtx(), started.Challenge.Token, authdomain.SecondFactor{Code: right}); !errors.Is(err, authdomain.ErrInvalidMFA) {
		t.Errorf("LoginMFA() after %d wrong codes error = %v, want the challenge exhausted", authlib.MFAChallengeMaxAttempts, err)
	}

	started, _ = f.svc.Login(requestCtx(), "ada@example.com", password)
	f.clock.advance(authlib.MFAChallengeTTL + time.Second)
	if _, err := f.svc.LoginMFA(requestCtx(), started.Challenge.Token, authdomain.SecondFactor{Code: f.totpCode(t, secret)}); !errors.Is(err, authdomain.ErrInvalidMFA) {
		t.Errorf("LoginMFA() after the challenge expired error = %v", err)
	}
	for _, token := range []string{"", "unknown-token"} {
		if _, err := f.svc.LoginMFA(requestCtx(), token, authdomain.SecondFactor{Code: f.totpCode(t, secret)}); !errors.Is(err, authdomain.ErrInvalidMFA) {
			t.Errorf("LoginMFA(%q) error = %v", token, err)
		}
	}
	started, _ = f.svc.Login(requestCtx(), "ada@example.com", password)
	if _, err := f.svc.LoginMFA(requestCtx(), started.Challenge.Token, authdomain.SecondFactor{}); !errors.Is(err, authdomain.ErrInvalidMFA) {
		t.Errorf("LoginMFA(no factor) error = %v", err)
	}
}

func TestRolesRequiringMFA(t *testing.T) {
	f := newFixture(t, withKeys, func(c *authusecase.Config) {
		c.Catalog = catalog()
		c.Catalog.RequireMFA("platform_admin")
	})
	f.signUp(t, "admin@example.com")
	u, _ := f.svc.UserByEmail(operator(), "admin@example.com")
	if err := f.svc.GrantRole(operator(), u.ID, "platform_admin"); err != nil {
		t.Fatal(err)
	}
	ctx, res := f.login(t, "admin@example.com")

	p, err := f.svc.Authenticate(context.Background(), res.Token)
	if err != nil || len(p.Permissions) != 2 || p.MFAVerified || !slices.Equal(p.StepUp, []string{"ops.settings.read", "ops.settings.write"}) {
		t.Fatalf("Authenticate() without 2FA = %+v, %v; want the role's permissions held back", p, err)
	}
	if err := actor.Require(authlib.WithPrincipal(context.Background(), p), "ops.settings.write"); !errors.Is(err, actor.ErrStepUpRequired) {
		t.Errorf("actor.Require() without 2FA = %v, want ErrStepUpRequired", err)
	}
	if me, err := f.svc.Me(ctx); err != nil || !me.MFARequired || me.MFAEnabled || len(me.StepUp) != 2 {
		t.Errorf("Me() without 2FA = %+v, %v", me, err)
	}

	_, codes := f.enroll(t, ctx)
	verified := f.principalCtx(t, res.Token)
	if p, _ := authlib.PrincipalFrom(verified); len(p.Permissions) != 4 || len(p.StepUp) != 0 {
		t.Errorf("principal after turning 2FA on = %+v, want every permission", p)
	}
	if err := f.svc.DisableTOTP(verified, password, authdomain.SecondFactor{RecoveryCode: codes[0]}); !errors.Is(err, authdomain.ErrMFARequiredByRole) {
		t.Errorf("DisableTOTP() with a role requiring 2FA error = %v", err)
	}
}

func TestDisableTOTPAndRegenerateRecoveryCodes(t *testing.T) {
	f := newFixture(t, withKeys)
	f.signUp(t, "ada@example.com")
	ctx, res := f.login(t, "ada@example.com")
	secret, codes := f.enroll(t, ctx)
	ctx = f.principalCtx(t, res.Token)
	f.clock.advance(30 * time.Second)

	fresh, err := f.svc.RegenerateRecoveryCodes(ctx, authdomain.SecondFactor{Code: f.totpCode(t, secret)})
	if err != nil || len(fresh) != authlib.RecoveryCodeCount || slices.Contains(fresh, codes[0]) {
		t.Fatalf("RegenerateRecoveryCodes() = %v, %v", fresh, err)
	}
	if _, err := f.svc.RegenerateRecoveryCodes(ctx, authdomain.SecondFactor{Code: f.totpCode(t, secret)}); !errors.Is(err, authdomain.ErrInvalidMFA) {
		t.Errorf("RegenerateRecoveryCodes(reused code) error = %v", err)
	}
	if err := f.svc.DisableTOTP(ctx, password, authdomain.SecondFactor{RecoveryCode: codes[0]}); !errors.Is(err, authdomain.ErrInvalidMFA) {
		t.Errorf("DisableTOTP(replaced recovery code) error = %v", err)
	}
	if err := f.svc.DisableTOTP(ctx, "wrong password here", authdomain.SecondFactor{RecoveryCode: fresh[0]}); !errors.Is(err, authdomain.ErrInvalidCredentials) {
		t.Errorf("DisableTOTP(wrong password) error = %v", err)
	}
	if err := f.svc.DisableTOTP(ctx, password, authdomain.SecondFactor{RecoveryCode: fresh[0]}); err != nil || f.emails.count("mfa_disabled") != 1 {
		t.Fatalf("DisableTOTP() error = %v", err)
	}
	if err := f.svc.DisableTOTP(ctx, password, authdomain.SecondFactor{RecoveryCode: fresh[1]}); !errors.Is(err, authdomain.ErrMFANotEnabled) {
		t.Errorf("DisableTOTP() when off error = %v", err)
	}
	if res, err := f.svc.Login(requestCtx(), "ada@example.com", password); err != nil || res.Challenge != nil || res.Token == "" {
		t.Errorf("Login() after turning 2FA off = %+v, %v; want a session", res, err)
	}
}

func TestDeleteAccountNeedsSecondFactor(t *testing.T) {
	f := newFixture(t, withKeys)
	f.signUp(t, "ada@example.com")
	ctx, res := f.login(t, "ada@example.com")
	_, codes := f.enroll(t, ctx)
	ctx = f.principalCtx(t, res.Token)

	if err := f.svc.DeleteAccount(ctx, password, authdomain.SecondFactor{}); !errors.Is(err, authdomain.ErrInvalidMFA) {
		t.Errorf("DeleteAccount() without a second factor error = %v", err)
	}
	if err := f.svc.DeleteAccount(ctx, password, authdomain.SecondFactor{RecoveryCode: codes[2]}); err != nil {
		t.Errorf("DeleteAccount() with a recovery code error = %v", err)
	}
}

func TestOperatorEnrollmentResetAndKeyRotation(t *testing.T) {
	f := newFixture(t, withKeys)
	f.signUp(t, "ada@example.com")
	u, _ := f.svc.UserByEmail(operator(), "ada@example.com")

	if _, _, err := f.svc.EnrollTOTP(context.Background(), u.ID); !errors.Is(err, authdomain.ErrActorRequired) {
		t.Errorf("EnrollTOTP() without an actor error = %v", err)
	}
	e, codes, err := f.svc.EnrollTOTP(operator(), u.ID)
	if err != nil || len(codes) != authlib.RecoveryCodeCount {
		t.Fatalf("EnrollTOTP() = %+v, %v, %v", e, codes, err)
	}
	if _, _, err := f.svc.EnrollTOTP(operator(), u.ID); !errors.Is(err, authdomain.ErrMFAAlreadyEnabled) {
		t.Errorf("EnrollTOTP() again error = %v", err)
	}

	// Rotate: a new key first, the old one after it.
	service := func(keys *authlib.Keyring) *authusecase.Service {
		svc, err := authusecase.NewService(authusecase.Config{
			Store: authrepository.NewStore(f.pool), Catalog: catalog(), Recorder: f.audit, Emails: f.emails, Now: f.clock.now, Keyring: keys,
		})
		if err != nil {
			t.Fatal(err)
		}
		return svc
	}
	newKey := authlib.NewKeyringKey("new")
	if n, err := service(mustKeyring(newKey + "," + testKeySpec)).RotateEncryptionKeys(operator()); err != nil || n != 1 {
		t.Fatalf("RotateEncryptionKeys() = %d, %v", n, err)
	}
	var keyID string
	if err := f.pool.QueryRow(context.Background(), `SELECT key_id FROM auth_totp`).Scan(&keyID); err != nil || keyID != "new" {
		t.Errorf("key ID after rotation = %q, %v", keyID, err)
	}
	// Once rotated, the new key alone finishes a sign-in.
	newOnly := service(mustKeyring(newKey))
	started, err := newOnly.Login(requestCtx(), "ada@example.com", password)
	if err != nil || started.Challenge == nil {
		t.Fatalf("Login() = %+v, %v", started, err)
	}
	if _, err := newOnly.LoginMFA(requestCtx(), started.Challenge.Token, authdomain.SecondFactor{Code: f.totpCode(t, e.Secret)}); err != nil {
		t.Errorf("LoginMFA() with the rotated key error = %v", err)
	}

	if err := f.svc.ResetMFA(operator(), u.ID); err != nil {
		t.Fatalf("ResetMFA() error = %v", err)
	}
	if err := f.svc.ResetMFA(operator(), u.ID); !errors.Is(err, authdomain.ErrMFANotEnabled) {
		t.Errorf("ResetMFA() again error = %v", err)
	}
	if res, err := f.svc.Login(requestCtx(), "ada@example.com", password); err != nil || res.Challenge != nil {
		t.Errorf("Login() after the reset = %+v, %v; want a session", res, err)
	}
	if !hasAll(f.audit.actions(), "auth.keys.rotated", "auth.mfa.reset") {
		t.Errorf("audit actions = %v", f.audit.actions())
	}
}

func TestMFAUnavailableWithoutKeys(t *testing.T) {
	f := newFixture(t)
	f.signUp(t, "ada@example.com")
	ctx, _ := f.login(t, "ada@example.com")
	if _, err := f.svc.StartTOTPEnrollment(ctx, password); !errors.Is(err, authdomain.ErrMFAUnavailable) {
		t.Errorf("StartTOTPEnrollment() without keys error = %v", err)
	}

	// An account with 2FA, set up where keys exist, can't sign in without them.
	withKeysSvc, err := authusecase.NewService(authusecase.Config{
		Store: authrepository.NewStore(f.pool), Catalog: catalog(), Recorder: f.audit, Emails: f.emails, Now: f.clock.now, Keyring: testKeyring,
	})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := f.svc.UserByEmail(operator(), "ada@example.com")
	if _, _, err := withKeysSvc.EnrollTOTP(operator(), u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Login(requestCtx(), "ada@example.com", password); !errors.Is(err, authdomain.ErrMFAUnavailable) {
		t.Errorf("Login() without keys error = %v, want ErrMFAUnavailable", err)
	}
}
