package usecase_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"apistock.dev/actor"
	"apistock.dev/config"
	authlib "apistock.dev/modules/auth"

	authdomain "example.com/acme-api/internal/modules/auth/domain"
	authusecase "example.com/acme-api/internal/modules/auth/usecase"
)

func TestRegisterVerifyLogin(t *testing.T) {
	f := newFixture(t)
	ctx := requestCtx()

	if err := f.svc.Register(ctx, "  Ada@Example.com ", password); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	sent := f.emails.last(t, "verify")
	if sent.to != "Ada@Example.com" || !sixDigits.MatchString(sent.code) || sent.ttl != authlib.DefaultVerificationCodeTTL {
		t.Errorf("verification email = %+v", sent)
	}
	if _, err := f.svc.Login(ctx, "ada@example.com", password); !errors.Is(err, authdomain.ErrEmailNotVerified) {
		t.Errorf("Login() before verifying error = %v, want ErrEmailNotVerified", err)
	}
	if err := f.svc.VerifyEmail(ctx, "ADA@example.com", sent.code); err != nil {
		t.Fatalf("VerifyEmail() error = %v", err)
	}
	if err := f.svc.VerifyEmail(ctx, "ada@example.com", sent.code); !errors.Is(err, authdomain.ErrInvalidCode) {
		t.Errorf("VerifyEmail() with a used code error = %v, want ErrInvalidCode", err)
	}

	res, err := f.svc.Login(ctx, "ada@example.com", password)
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if len(res.Token) < 40 || res.User.Email != "Ada@Example.com" || !res.User.EmailVerified() || !strings.HasPrefix(res.User.ID, "usr_") ||
		res.Session.IP != "203.0.113.9" || res.Session.UserAgent != "test-agent/1" || !res.Session.ExpiresAt().Equal(f.clock.now().Add(authlib.DefaultSessionIdleTTL)) {
		t.Errorf("Login() = %+v", res)
	}
	p, err := f.svc.Authenticate(context.Background(), res.Token)
	if err != nil || p.UserID != res.User.ID || p.SessionID != res.Session.ID || len(p.Permissions) != 0 {
		t.Fatalf("Authenticate() = %+v, %v", p, err)
	}

	for _, action := range []string{"auth.user.registered", "auth.email.verified", "auth.login.failed", "auth.login.succeeded"} {
		if !slices.Contains(f.audit.actions(), action) {
			t.Errorf("audit actions %v lack %s", f.audit.actions(), action)
		}
	}
	if e, _ := f.audit.find("auth.login.succeeded"); e.ActorKind != actor.KindUser || e.ActorID != res.User.ID || e.IP != "203.0.113.9" {
		t.Errorf("login audit event = %+v", e)
	}
	if e, _ := f.audit.find("auth.login.failed"); e.Metadata["reason"] != "email_not_verified" || e.ActorKind != actor.KindAnonymous {
		t.Errorf("failed login audit event = %+v", e)
	}
}

func TestRegisterValidatesInput(t *testing.T) {
	f := newFixture(t, func(c *authusecase.Config) {
		c.PasswordChecker = func(_ context.Context, pw string) error {
			if strings.Contains(pw, "password") {
				return errors.New("appears in a list of breached passwords")
			}
			return nil
		}
	})
	ctx := requestCtx()
	if err := f.svc.Register(ctx, "not an email", password); !errors.Is(err, authlib.ErrInvalidEmail) {
		t.Errorf("Register(bad email) error = %v", err)
	}
	var pwErr *authlib.PasswordError
	for _, pw := range []string{"short", "my password is long"} {
		if err := f.svc.Register(ctx, "ada@example.com", pw); !errors.As(err, &pwErr) || !errors.Is(err, authlib.ErrWeakPassword) {
			t.Errorf("Register(password %q) error = %v, want *PasswordError", pw, err)
		}
	}
	if !strings.Contains(pwErr.Reason, "breached") {
		t.Errorf("checker reason = %q", pwErr.Reason)
	}
	if len(f.emails.sent) != 0 {
		t.Errorf("emails sent for invalid input: %+v", f.emails.sent)
	}
}

func TestRegisterNeverRevealsExistingAccounts(t *testing.T) {
	f := newFixture(t)
	ctx := requestCtx()
	f.signUp(t, "ada@example.com")

	// A verified account: an "account exists" notice, at most once a minute,
	// and the password stays.
	for range 2 {
		if err := f.svc.Register(ctx, "ADA@example.com", "another long password"); err != nil {
			t.Fatalf("Register(existing) error = %v, want nil", err)
		}
	}
	if f.emails.count("exists") != 1 || f.emails.count("verify") != 1 {
		t.Errorf("emails = %+v, want one account-exists notice (the second throttled) and no new code", f.emails.sent)
	}
	if _, err := f.svc.Login(ctx, "ada@example.com", "another long password"); !errors.Is(err, authdomain.ErrInvalidCredentials) {
		t.Errorf("registering again changed a verified account's password: %v", err)
	}
	f.clock.advance(authlib.CodeResendInterval)
	if err := f.svc.Register(ctx, "ada@example.com", "another long password"); err != nil || f.emails.count("exists") != 2 {
		t.Errorf("Register(existing) a minute later = %v, %d notices, want a second notice", err, f.emails.count("exists"))
	}
}

// TestRegisterAgainKeepsThePassword: someone registering an address whose
// owner hasn't verified yet can't choose the password of the account the
// owner then verifies, nor mint codes faster than resend allows.
func TestRegisterAgainKeepsThePassword(t *testing.T) {
	f := newFixture(t)
	ctx := requestCtx()
	const ownerPassword, otherPassword = "the owner's long password", "someone else's long password"

	if err := f.svc.Register(ctx, "bob@example.com", ownerPassword); err != nil {
		t.Fatal(err)
	}
	first := f.emails.last(t, "verify").code
	for range 3 {
		if err := f.svc.Register(ctx, "bob@example.com", otherPassword); err != nil {
			t.Fatalf("Register(unverified again) error = %v, want nil", err)
		}
	}
	if n := f.emails.count("verify"); n != 1 {
		t.Errorf("verification emails = %d, want registering again within a minute throttled", n)
	}

	f.clock.advance(authlib.CodeResendInterval)
	if err := f.svc.Register(ctx, "bob@example.com", otherPassword); err != nil || f.emails.count("verify") != 2 {
		t.Fatalf("Register(unverified again) a minute later = %v, %d emails, want a new code", err, f.emails.count("verify"))
	}
	second := f.emails.last(t, "verify").code
	if first != second {
		if err := f.svc.VerifyEmail(ctx, "bob@example.com", first); !errors.Is(err, authdomain.ErrInvalidCode) {
			t.Errorf("VerifyEmail(old code) error = %v, want ErrInvalidCode", err)
		}
	}
	if err := f.svc.VerifyEmail(ctx, "bob@example.com", second); err != nil {
		t.Fatalf("VerifyEmail(new code) error = %v", err)
	}
	if _, err := f.svc.Login(ctx, "bob@example.com", otherPassword); !errors.Is(err, authdomain.ErrInvalidCredentials) {
		t.Errorf("Login() with the password from registering again = %v, want ErrInvalidCredentials", err)
	}
	if _, err := f.svc.Login(ctx, "bob@example.com", ownerPassword); err != nil {
		t.Errorf("Login() with the owner's password error = %v", err)
	}
	registered := 0
	for _, action := range f.audit.actions() {
		if action == "auth.user.registered" {
			registered++
		}
	}
	if registered != 1 {
		t.Errorf("auth.user.registered events = %d, want 1 for the one account created", registered)
	}
}

func TestLoginErrorsAreUniform(t *testing.T) {
	f := newFixture(t, func(c *authusecase.Config) { c.LoginAttempts, c.LoginWindow = 3, time.Minute })
	ctx := requestCtx()
	f.signUp(t, "ada@example.com")

	for name, email := range map[string]string{"unknown": "nobody@example.com", "malformed": "nope"} {
		if _, err := f.svc.Login(ctx, email, password); !errors.Is(err, authdomain.ErrInvalidCredentials) {
			t.Errorf("%s address: Login() error = %v, want ErrInvalidCredentials", name, err)
		}
	}
	for range 3 {
		if _, err := f.svc.Login(ctx, "ada@example.com", "wrong password here"); !errors.Is(err, authdomain.ErrInvalidCredentials) {
			t.Errorf("wrong password: Login() error = %v", err)
		}
	}
	_, err := f.svc.Login(ctx, "ada@example.com", password)
	var limited *authdomain.RateLimitError
	if !errors.As(err, &limited) || limited.RetryAfter <= 0 || !errors.Is(err, authdomain.ErrTooManyAttempts) {
		t.Fatalf("Login() after 3 attempts error = %v, want *RateLimitError", err)
	}
	f.clock.advance(time.Minute)
	if _, err := f.svc.Login(ctx, "ada@example.com", password); err != nil {
		t.Errorf("Login() after the window error = %v", err)
	}
}

func TestCodesExpireAndLimitAttempts(t *testing.T) {
	f := newFixture(t, func(c *authusecase.Config) { c.VerificationCodeTTL = config.Static(10 * time.Minute) })
	ctx := requestCtx()
	if err := f.svc.Register(ctx, "ada@example.com", password); err != nil {
		t.Fatal(err)
	}
	code := f.emails.last(t, "verify").code
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	for range authlib.CodeMaxAttempts {
		if err := f.svc.VerifyEmail(ctx, "ada@example.com", wrong); !errors.Is(err, authdomain.ErrInvalidCode) {
			t.Fatalf("VerifyEmail(wrong) error = %v", err)
		}
	}
	if err := f.svc.VerifyEmail(ctx, "ada@example.com", code); !errors.Is(err, authdomain.ErrInvalidCode) {
		t.Errorf("VerifyEmail(right code after 5 wrong attempts) error = %v, want ErrInvalidCode", err)
	}

	if err := f.svc.ResendVerification(ctx, "ada@example.com"); err != nil {
		t.Fatal(err)
	}
	if n := f.emails.count("verify"); n != 1 {
		t.Errorf("verification emails = %d, want the resend throttled", n)
	}
	f.clock.advance(time.Minute)
	if err := f.svc.ResendVerification(ctx, "ada@example.com"); err != nil || f.emails.count("verify") != 2 {
		t.Fatalf("ResendVerification() = %v, emails %d", err, f.emails.count("verify"))
	}
	fresh := f.emails.last(t, "verify")
	if fresh.ttl != 10*time.Minute {
		t.Errorf("code TTL = %v, want the configured 10m", fresh.ttl)
	}
	f.clock.advance(11 * time.Minute)
	if err := f.svc.VerifyEmail(ctx, "ada@example.com", fresh.code); !errors.Is(err, authdomain.ErrInvalidCode) {
		t.Errorf("VerifyEmail(expired code) error = %v, want ErrInvalidCode", err)
	}
	if err := f.svc.ResendVerification(ctx, "nobody@example.com"); err != nil {
		t.Errorf("ResendVerification(unknown) error = %v, want nil", err)
	}
}

func TestPasswordReset(t *testing.T) {
	f := newFixture(t)
	ctx := requestCtx()
	f.signUp(t, "ada@example.com")
	_, first := f.login(t, "ada@example.com")

	if err := f.svc.RequestPasswordReset(ctx, "nobody@example.com"); err != nil || f.emails.count("reset") != 0 {
		t.Fatalf("RequestPasswordReset(unknown) = %v, emails %d; want nil and no email", err, f.emails.count("reset"))
	}
	if err := f.svc.RequestPasswordReset(ctx, "ada@example.com"); err != nil {
		t.Fatal(err)
	}
	code := f.emails.last(t, "reset")
	if code.ttl != authlib.DefaultResetCodeTTL {
		t.Errorf("reset code TTL = %v", code.ttl)
	}
	if err := f.svc.ResetPassword(ctx, "ada@example.com", "123", "new long password!"); !errors.Is(err, authdomain.ErrInvalidCode) {
		t.Errorf("ResetPassword(wrong code) error = %v", err)
	}
	if err := f.svc.ResetPassword(ctx, "ada@example.com", code.code, "short"); !errors.Is(err, authlib.ErrWeakPassword) {
		t.Errorf("ResetPassword(weak) error = %v", err)
	}
	if err := f.svc.ResetPassword(ctx, "ada@example.com", code.code, "new long password!"); err != nil {
		t.Fatalf("ResetPassword() error = %v", err)
	}
	if _, err := f.svc.Authenticate(ctx, first.Token); !errors.Is(err, authlib.ErrUnauthenticated) {
		t.Errorf("session after reset error = %v, want ended", err)
	}
	if _, err := f.svc.Login(ctx, "ada@example.com", "new long password!"); err != nil {
		t.Errorf("Login(new password) error = %v", err)
	}
	if f.emails.count("changed") != 1 {
		t.Error("no password-changed notice")
	}
}

func TestResetVerifiesTheAddress(t *testing.T) {
	f := newFixture(t)
	ctx := requestCtx()
	if err := f.svc.Register(ctx, "ada@example.com", password); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.RequestPasswordReset(ctx, "ada@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.ResetPassword(ctx, "ada@example.com", f.emails.last(t, "reset").code, "new long password!"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Login(ctx, "ada@example.com", "new long password!"); err != nil {
		t.Errorf("Login() after resetting an unverified account error = %v", err)
	}
}

func TestChangePasswordAndDeleteAccount(t *testing.T) {
	f := newFixture(t, func(c *authusecase.Config) { c.DeletedAccountRetention = config.Static(24 * time.Hour) })
	ctx := requestCtx()
	f.signUp(t, "ada@example.com")
	current, currentRes := f.login(t, "ada@example.com")
	_, other := f.login(t, "ada@example.com")

	if err := f.svc.ChangePassword(ctx, password, "new long password!"); !errors.Is(err, authlib.ErrUnauthenticated) {
		t.Errorf("ChangePassword() without a session error = %v", err)
	}
	if err := f.svc.ChangePassword(current, "wrong current pw", "new long password!"); !errors.Is(err, authdomain.ErrInvalidCredentials) {
		t.Errorf("ChangePassword(wrong current) error = %v", err)
	}
	if err := f.svc.ChangePassword(current, password, "new long password!"); err != nil {
		t.Fatalf("ChangePassword() error = %v", err)
	}
	if _, err := f.svc.Authenticate(ctx, currentRes.Token); err != nil {
		t.Errorf("current session after changing password error = %v, want kept", err)
	}
	if _, err := f.svc.Authenticate(ctx, other.Token); !errors.Is(err, authlib.ErrUnauthenticated) {
		t.Errorf("other session after changing password error = %v, want ended", err)
	}

	if err := f.svc.DeleteAccount(current, password, authdomain.SecondFactor{}); !errors.Is(err, authdomain.ErrInvalidCredentials) {
		t.Errorf("DeleteAccount(old password) error = %v", err)
	}
	if err := f.svc.DeleteAccount(current, "new long password!", authdomain.SecondFactor{}); err != nil {
		t.Fatalf("DeleteAccount() error = %v", err)
	}
	if _, err := f.svc.Authenticate(ctx, currentRes.Token); !errors.Is(err, authlib.ErrUnauthenticated) {
		t.Errorf("session after deleting the account error = %v", err)
	}
	if _, err := f.svc.Login(ctx, "ada@example.com", "new long password!"); !errors.Is(err, authdomain.ErrInvalidCredentials) {
		t.Errorf("Login() to a deleted account error = %v", err)
	}
	f.signUp(t, "ada@example.com") // the address is free again
	again, _ := f.login(t, "ada@example.com")
	if err := f.svc.Logout(again); err != nil {
		t.Fatal(err)
	}

	res, err := f.svc.Cleanup(ctx)
	if err != nil || res.Users != 0 {
		t.Fatalf("Cleanup() before retention = %+v, %v", res, err)
	}
	f.clock.advance(25 * time.Hour)
	res, err = f.svc.Cleanup(ctx)
	if err != nil || res.Users != 1 || res.Codes == 0 {
		t.Errorf("Cleanup() after retention = %+v, %v; want the deleted account and old codes removed", res, err)
	}
	f.clock.advance(8 * 24 * time.Hour)
	if res, err := f.svc.Cleanup(ctx); err != nil || res.Sessions == 0 {
		t.Errorf("Cleanup() a week later = %+v, %v; want ended sessions removed", res, err)
	}
}

func TestCreateUserForOperators(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.CreateUser(context.Background(), "admin@example.com", password, true); !errors.Is(err, authdomain.ErrActorRequired) {
		t.Errorf("CreateUser() without actor error = %v", err)
	}
	u, err := f.svc.CreateUser(operator(), "admin@example.com", password, true)
	if err != nil || !u.EmailVerified() {
		t.Fatalf("CreateUser() = %+v, %v", u, err)
	}
	if _, err := f.svc.CreateUser(operator(), "ADMIN@example.com", password, true); !errors.Is(err, authdomain.ErrEmailTaken) {
		t.Errorf("CreateUser(taken) error = %v", err)
	}
	if _, err := f.svc.Login(requestCtx(), "admin@example.com", password); err != nil {
		t.Errorf("Login() as the created user error = %v", err)
	}
	if byEmail, err := f.svc.UserByEmail(context.Background(), "Admin@Example.com"); err != nil || byEmail.ID != u.ID {
		t.Errorf("UserByEmail() = %+v, %v", byEmail, err)
	}
	if _, err := f.svc.User(context.Background(), "usr_missing"); !errors.Is(err, authdomain.ErrUserNotFound) {
		t.Errorf("User(missing) error = %v", err)
	}
}

func TestNewServiceValidates(t *testing.T) {
	if _, err := authusecase.NewService(authusecase.Config{LoginAttempts: -1, LoginWindow: time.Minute}); err == nil ||
		!strings.Contains(err.Error(), "required") || !strings.Contains(err.Error(), "login limit") {
		t.Errorf("NewService(empty config) error = %v", err)
	}
}
