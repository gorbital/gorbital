package usecase_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	authlib "gorbital.dev/modules/auth"
	"gorbital.dev/modules/auth/passkey"
	"gorbital.dev/modules/auth/passkey/passkeytest"
	"gorbital.dev/ratelimit"

	authdomain "example.com/shelfie/internal/modules/auth/domain"
	authusecase "example.com/shelfie/internal/modules/auth/usecase"
)

const passkeyOrigin = "http://localhost:8080"

var testPasskeys = func() *passkey.Service {
	s, err := passkey.New(passkey.Config{RPID: "localhost", RPDisplayName: "acme-api", Origins: []string{passkeyOrigin}})
	if err != nil {
		panic(err)
	}
	return s
}()

// withPasskeys configures a relying party on localhost.
func withPasskeys(c *authusecase.Config) { c.Passkeys = testPasskeys }

// addPasskey registers a passkey from auth for the signed-in user of ctx.
func (f *fixture) addPasskey(t *testing.T, ctx context.Context, password string, auth *passkeytest.Authenticator) authusecase.PasskeyRegistration {
	t.Helper()
	c, err := f.svc.BeginPasskeyRegistration(ctx, password)
	if err != nil {
		t.Fatalf("BeginPasskeyRegistration() error = %v", err)
	}
	resp, err := auth.Create(c.Options)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := f.svc.FinishPasskeyRegistration(ctx, c.Token, "Laptop", resp)
	if err != nil {
		t.Fatalf("FinishPasskeyRegistration() error = %v", err)
	}
	return reg
}

// passkeyLogin signs in without a password, with auth's passkey.
func (f *fixture) passkeyLogin(t *testing.T, auth *passkeytest.Authenticator) (authusecase.LoginResult, error) {
	t.Helper()
	c, err := f.svc.BeginPasskeyLogin(requestCtx())
	if err != nil {
		t.Fatalf("BeginPasskeyLogin() error = %v", err)
	}
	resp, err := auth.Get(c.Options)
	if err != nil {
		t.Fatal(err)
	}
	return f.svc.FinishPasskeyLogin(requestCtx(), c.Token, resp)
}

func TestPasskeyRegistrationAndSignIn(t *testing.T) {
	f := newFixture(t, withKeys, withPasskeys)
	f.signUp(t, "ada@example.com")
	ctx, res := f.login(t, "ada@example.com")

	if _, err := f.svc.BeginPasskeyRegistration(ctx, "wrong password here"); !errors.Is(err, authdomain.ErrInvalidCredentials) {
		t.Errorf("BeginPasskeyRegistration(wrong password) error = %v", err)
	}
	laptop := passkeytest.New(passkeyOrigin)
	reg := f.addPasskey(t, ctx, password, laptop)
	if reg.Passkey.Name != "Laptop" || len(reg.RecoveryCodes) != authlib.RecoveryCodeCount || f.emails.count("passkey_added") != 1 {
		t.Errorf("first passkey = %+v, %d emails; want recovery codes as the first second factor", reg, f.emails.count("passkey_added"))
	}
	verified := f.principalCtx(t, res.Token)
	if p, _ := authlib.PrincipalFrom(verified); !p.MFAVerified {
		t.Error("session after adding a passkey isn't verified with a second factor")
	}

	// A session that just verified a second factor adds another without the
	// password; a ceremony works once.
	c, err := f.svc.BeginPasskeyRegistration(verified, "")
	if err != nil {
		t.Fatal(err)
	}
	phone := passkeytest.New(passkeyOrigin)
	resp, _ := phone.Create(c.Options)
	second, err := f.svc.FinishPasskeyRegistration(verified, c.Token, " Phone ", resp)
	if err != nil || second.Passkey.Name != "Phone" || second.RecoveryCodes != nil {
		t.Fatalf("second passkey = %+v, %v", second, err)
	}
	if _, err := f.svc.FinishPasskeyRegistration(verified, c.Token, "Again", resp); !errors.Is(err, authdomain.ErrInvalidPasskey) {
		t.Errorf("FinishPasskeyRegistration(used ceremony) error = %v", err)
	}

	// Passwordless sign-in.
	done, err := f.passkeyLogin(t, laptop)
	if err != nil || done.Token == "" || !done.Session.MFAVerified() {
		t.Fatalf("passkey sign-in = %+v, %v", done, err)
	}

	// A password sign-in now asks for a second factor, which a passkey answers.
	started, err := f.svc.Login(requestCtx(), "ada@example.com", password)
	if err != nil || started.Challenge == nil || !slices.Equal(started.Challenge.Methods, []string{"passkey", "recovery_code"}) {
		t.Fatalf("Login() with passkeys = %+v, %v", started, err)
	}
	pc, err := f.svc.BeginPasskeySecondFactor(requestCtx(), started.Challenge.Token)
	if err != nil {
		t.Fatal(err)
	}
	resp, _ = phone.Get(pc.Options)
	assertion := &authdomain.PasskeyAssertion{CeremonyToken: pc.Token, Credential: resp}
	finished, err := f.svc.LoginMFA(requestCtx(), started.Challenge.Token, authdomain.SecondFactor{Passkey: assertion})
	if err != nil || !finished.Session.MFAVerified() {
		t.Fatalf("LoginMFA(passkey) = %+v, %v", finished, err)
	}

	// Passkeys are listed, renamed and removed.
	if list, err := f.svc.ListPasskeys(verified); err != nil || len(list) != 2 || list[0].LastUsedAt == nil {
		t.Errorf("ListPasskeys() = %+v, %v", list, err)
	}
	if err := f.svc.RenamePasskey(verified, second.Passkey.ID, "Work phone"); err != nil {
		t.Errorf("RenamePasskey() error = %v", err)
	}
	if err := f.svc.RenamePasskey(verified, "pky_unknown", "x"); !errors.Is(err, authdomain.ErrPasskeyNotFound) {
		t.Errorf("RenamePasskey(unknown) error = %v", err)
	}
	if err := f.svc.RenamePasskey(verified, second.Passkey.ID, strings.Repeat("x", 101)); !errors.Is(err, authdomain.ErrInvalidPasskeyName) {
		t.Errorf("RenamePasskey(long name) error = %v", err)
	}
	if err := f.svc.RemovePasskey(verified, second.Passkey.ID, ""); err != nil || f.emails.count("passkey_removed") != 1 {
		t.Errorf("RemovePasskey() error = %v", err)
	}
	if me, err := f.svc.Me(verified); err != nil || !me.MFAEnabled {
		t.Errorf("Me() with a passkey = %+v, %v; want MFAEnabled", me, err)
	}
	if !hasAll(f.audit.actions(), "auth.passkey.registered", "auth.passkey.renamed", "auth.passkey.removed", "auth.login.succeeded", "auth.mfa.challenge_succeeded") {
		t.Errorf("audit actions = %v", f.audit.actions())
	}
}

func TestPasskeyRejections(t *testing.T) {
	// Registering the most passkeys needs more checks of the password than
	// the re-authentication limit allows.
	f := newFixture(t, withPasskeys, func(c *authusecase.Config) { c.ReauthLimiter = ratelimit.New(1, 100) })
	f.signUp(t, "ada@example.com")
	ctx, res := f.login(t, "ada@example.com")
	laptop := passkeytest.New(passkeyOrigin)
	f.addPasskey(t, ctx, password, laptop)
	// The session is verified now; later requests see it.
	ctx = f.principalCtx(t, res.Token)

	// Another site can't register a passkey.
	c, _ := f.svc.BeginPasskeyRegistration(ctx, password)
	resp, _ := passkeytest.New("https://login.example.net").Create(c.Options)
	if _, err := f.svc.FinishPasskeyRegistration(ctx, c.Token, "", resp); !errors.Is(err, authdomain.ErrInvalidPasskey) {
		t.Errorf("FinishPasskeyRegistration(wrong origin) error = %v", err)
	}

	// Without user verification, sign-in fails.
	laptop.UserVerified = false
	if _, err := f.passkeyLogin(t, laptop); !errors.Is(err, authdomain.ErrInvalidPasskey) {
		t.Errorf("passkey sign-in without user verification error = %v", err)
	}
	laptop.UserVerified = true

	// A counter that didn't increase is refused and recorded.
	if _, err := f.passkeyLogin(t, laptop); err != nil {
		t.Fatal(err)
	}
	laptop.SetSignCount(0)
	if _, err := f.passkeyLogin(t, laptop); !errors.Is(err, authdomain.ErrInvalidPasskey) {
		t.Errorf("passkey sign-in with a repeated counter error = %v", err)
	}
	if _, ok := f.audit.find("auth.passkey.clone_warning"); !ok {
		t.Error("no auth.passkey.clone_warning event")
	}

	// A passkey the server doesn't know.
	c, _ = f.svc.BeginPasskeyRegistration(ctx, password)
	stranger := passkeytest.New(passkeyOrigin)
	if _, err := stranger.Create(c.Options); err != nil {
		t.Fatal(err)
	}
	if _, err := f.passkeyLogin(t, stranger); !errors.Is(err, authdomain.ErrInvalidPasskey) {
		t.Errorf("sign-in with an unregistered passkey error = %v", err)
	}

	// At most MaxPasskeys.
	for range authdomain.MaxPasskeys - 1 {
		f.addPasskey(t, ctx, password, passkeytest.New(passkeyOrigin))
	}
	if _, err := f.svc.BeginPasskeyRegistration(ctx, password); !errors.Is(err, authdomain.ErrPasskeyLimitReached) {
		t.Errorf("BeginPasskeyRegistration() at the limit error = %v", err)
	}

	plain := newFixture(t)
	if _, err := plain.svc.BeginPasskeyLogin(requestCtx()); !errors.Is(err, authdomain.ErrPasskeysUnavailable) {
		t.Errorf("BeginPasskeyLogin() without a relying party error = %v", err)
	}
}

func TestPasskeysAndRolesRequiringMFA(t *testing.T) {
	f := newFixture(t, withKeys, withPasskeys, func(c *authusecase.Config) {
		c.Catalog = catalog()
		c.Catalog.RequireMFA("platform_admin")
	})
	f.signUp(t, "admin@example.com")
	u, _ := f.svc.UserByEmail(operator(), "admin@example.com")
	if err := f.svc.GrantRole(operator(), u.ID, "platform_admin"); err != nil {
		t.Fatal(err)
	}
	ctx, res := f.login(t, "admin@example.com")
	reg := f.addPasskey(t, ctx, password, passkeytest.New(passkeyOrigin))
	verified := f.principalCtx(t, res.Token)
	if p, _ := authlib.PrincipalFrom(verified); len(p.Permissions) != 4 {
		t.Errorf("principal after adding a passkey = %+v, want the role's permissions", p)
	}

	if err := f.svc.RemovePasskey(verified, reg.Passkey.ID, ""); !errors.Is(err, authdomain.ErrMFARequiredByRole) {
		t.Errorf("RemovePasskey(last second factor) error = %v", err)
	}
	// With an authenticator app too, the passkey can go.
	_, codes := f.enroll(t, verified)
	if err := f.svc.RemovePasskey(verified, reg.Passkey.ID, ""); err != nil {
		t.Errorf("RemovePasskey() with an authenticator app error = %v", err)
	}
	// And the authenticator app can go while a passkey remains, keeping the recovery codes.
	f.addPasskey(t, verified, "", passkeytest.New(passkeyOrigin))
	if err := f.svc.DisableTOTP(verified, password, authdomain.SecondFactor{RecoveryCode: codes[0]}); err != nil {
		t.Fatalf("DisableTOTP() with a passkey left error = %v", err)
	}
	started, err := f.svc.Login(requestCtx(), "admin@example.com", password)
	if err != nil || started.Challenge == nil || !slices.Equal(started.Challenge.Methods, []string{"passkey", "recovery_code"}) {
		t.Fatalf("Login() = %+v, %v", started, err)
	}
	if _, err := f.svc.LoginMFA(requestCtx(), started.Challenge.Token, authdomain.SecondFactor{RecoveryCode: codes[1]}); err != nil {
		t.Errorf("LoginMFA(recovery code kept with a passkey) error = %v", err)
	}

	// An operator reset removes passkeys too.
	if err := f.svc.ResetMFA(operator(), u.ID); err != nil {
		t.Fatal(err)
	}
	if res, err := f.svc.Login(requestCtx(), "admin@example.com", password); err != nil || res.Challenge != nil {
		t.Errorf("Login() after a reset = %+v, %v; want a session", res, err)
	}
}

// verifyWithPasskey returns auth's passkey response to a verification
// ceremony of the signed-in user of ctx, as a second factor.
func (f *fixture) verifyWithPasskey(t *testing.T, ctx context.Context, auth *passkeytest.Authenticator) authdomain.SecondFactor {
	t.Helper()
	c, err := f.svc.BeginPasskeyVerification(ctx)
	if err != nil {
		t.Fatalf("BeginPasskeyVerification() error = %v", err)
	}
	resp, err := auth.Get(c.Options)
	if err != nil {
		t.Fatal(err)
	}
	return authdomain.SecondFactor{Passkey: &authdomain.PasskeyAssertion{CeremonyToken: c.Token, Credential: resp}}
}

// TestPasskeyChangesConfirmTheUser checks that a session whose second factor
// is old can't change sign-in methods without the password, and that a
// passkey confirms a signed-in user's sensitive changes (ADR-0044).
func TestPasskeyChangesConfirmTheUser(t *testing.T) {
	f := newFixture(t, withPasskeys)
	f.signUp(t, "ada@example.com")
	_, other := f.login(t, "ada@example.com")
	ctx, res := f.login(t, "ada@example.com")
	laptop := passkeytest.New(passkeyOrigin)
	reg := f.addPasskey(t, ctx, password, laptop)

	// The first second factor signs out other devices.
	if _, err := f.svc.Authenticate(context.Background(), other.Token); !errors.Is(err, authlib.ErrUnauthenticated) {
		t.Errorf("Authenticate(other session) after the first passkey error = %v, want ErrUnauthenticated", err)
	}

	// Once the second factor is 10 minutes old, changes need the password.
	f.clock.advance(authlib.RecentVerification)
	stale := f.principalCtx(t, res.Token)
	if _, err := f.svc.BeginPasskeyRegistration(stale, ""); !errors.Is(err, authdomain.ErrInvalidCredentials) {
		t.Errorf("BeginPasskeyRegistration(old second factor, no password) error = %v", err)
	}
	if err := f.svc.RemovePasskey(stale, reg.Passkey.ID, ""); !errors.Is(err, authdomain.ErrInvalidCredentials) {
		t.Errorf("RemovePasskey(old second factor, no password) error = %v", err)
	}
	if _, err := f.svc.BeginPasskeyRegistration(stale, password); err != nil {
		t.Errorf("BeginPasskeyRegistration(old second factor, password) error = %v", err)
	}

	// A passkey replaces the recovery codes; a recovery code can't.
	if _, err := f.svc.RegenerateRecoveryCodes(stale, authdomain.SecondFactor{RecoveryCode: reg.RecoveryCodes[0]}); !errors.Is(err, authdomain.ErrInvalidMFA) {
		t.Errorf("RegenerateRecoveryCodes(recovery code) error = %v", err)
	}
	byPasskey := f.verifyWithPasskey(t, stale, laptop)
	if codes, err := f.svc.RegenerateRecoveryCodes(stale, byPasskey); err != nil || len(codes) != authlib.RecoveryCodeCount {
		t.Fatalf("RegenerateRecoveryCodes(passkey) = %v, %v", codes, err)
	}
	if _, err := f.svc.RegenerateRecoveryCodes(stale, byPasskey); !errors.Is(err, authdomain.ErrInvalidMFA) {
		t.Errorf("RegenerateRecoveryCodes(used verification) error = %v", err)
	}

	// A sign-in's ceremony and another user's verification don't count.
	started, err := f.svc.Login(requestCtx(), "ada@example.com", password)
	if err != nil || started.Challenge == nil {
		t.Fatalf("Login() = %+v, %v", started, err)
	}
	pc, err := f.svc.BeginPasskeySecondFactor(requestCtx(), started.Challenge.Token)
	if err != nil {
		t.Fatal(err)
	}
	resp, _ := laptop.Get(pc.Options)
	signIn := authdomain.SecondFactor{Passkey: &authdomain.PasskeyAssertion{CeremonyToken: pc.Token, Credential: resp}}
	if err := f.svc.DeleteAccount(stale, password, signIn); !errors.Is(err, authdomain.ErrInvalidMFA) {
		t.Errorf("DeleteAccount(sign-in ceremony) error = %v", err)
	}
	f.signUp(t, "bob@example.com")
	bob, _ := f.login(t, "bob@example.com")
	if _, err := f.svc.BeginPasskeyVerification(bob); !errors.Is(err, authdomain.ErrMFANotEnabled) {
		t.Errorf("BeginPasskeyVerification() without passkeys error = %v", err)
	}
	bobKey := passkeytest.New(passkeyOrigin)
	f.addPasskey(t, bob, password, bobKey)
	if err := f.svc.DeleteAccount(stale, password, f.verifyWithPasskey(t, bob, bobKey)); !errors.Is(err, authdomain.ErrInvalidMFA) {
		t.Errorf("DeleteAccount(another user's verification) error = %v", err)
	}

	// An account with only passkeys deletes itself with one.
	if err := f.svc.DeleteAccount(stale, password, f.verifyWithPasskey(t, stale, laptop)); err != nil {
		t.Errorf("DeleteAccount(passkey) error = %v", err)
	}
}
