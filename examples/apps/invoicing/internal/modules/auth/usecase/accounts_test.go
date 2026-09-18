package usecase_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gorbital.dev/actor"
	"gorbital.dev/config"
	authlib "gorbital.dev/modules/auth"
	"gorbital.dev/modules/auth/social"
	"gorbital.dev/modules/auth/social/socialtest"

	authdomain "example.com/invoicing/internal/modules/auth/domain"
	authusecase "example.com/invoicing/internal/modules/auth/usecase"
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
	if err != nil || p.UserID != res.User.ID || p.SessionID != res.Session.ID || !slices.Equal(p.Permissions, []string{"notes.note.read", "notes.note.write"}) {
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
	// "\u212Aevin" starts with the Kelvin sign, which lowercases to k: it
	// would share kevin@example.com's account (security review AUTH-S-3).
	for _, email := range []string{"not an email", "\u212Aevin@example.com"} {
		if err := f.svc.Register(ctx, email, password); !errors.Is(err, authlib.ErrInvalidEmail) {
			t.Errorf("Register(%q) error = %v", email, err)
		}
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

// TestRegisterAgainKeepsOnlyTheSamePassword: someone registering an address
// whose owner hasn't verified yet can't choose the password of the account
// the owner then verifies, whether they register before or after the owner,
// nor mint codes faster than resend allows (security review AUTH-S-1).
func TestRegisterAgainKeepsOnlyTheSamePassword(t *testing.T) {
	f := newFixture(t)
	ctx := requestCtx()
	const ownerPassword, otherPassword = "the owner's long password", "someone else's long password"

	// The owner registers twice with the same password: it stays.
	for range 2 {
		if err := f.svc.Register(ctx, "bob@example.com", ownerPassword); err != nil {
			t.Fatal(err)
		}
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
	// Another password was given, so neither is kept; the owner sets one.
	for _, pw := range []string{otherPassword, ownerPassword} {
		if _, err := f.svc.Login(ctx, "bob@example.com", pw); !errors.Is(err, authdomain.ErrInvalidCredentials) {
			t.Errorf("Login(%q) after a contested registration = %v, want ErrInvalidCredentials", pw, err)
		}
	}
	if err := f.svc.RequestPasswordReset(ctx, "bob@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.ResetPassword(ctx, "bob@example.com", f.emails.last(t, "reset").code, ownerPassword); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Login(ctx, "bob@example.com", ownerPassword); err != nil {
		t.Errorf("Login() with the owner's reset password error = %v", err)
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

// TestPreRegistrationTakeover is the reviewers' proof of concept: the
// attacker registers first and the owner later, within or after the resend
// interval. The attacker's password never works on the account the owner
// verifies (security review AUTH-S-1).
func TestPreRegistrationTakeover(t *testing.T) {
	for name, wait := range map[string]time.Duration{"after a minute": 2 * time.Minute, "within a minute": 10 * time.Second} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			ctx := requestCtx()
			const attackerPassword, ownerPassword = "attacker chosen password", "the owner's own password"
			if err := f.svc.Register(ctx, "victim@example.com", attackerPassword); err != nil {
				t.Fatal(err)
			}
			f.clock.advance(wait)
			if err := f.svc.Register(ctx, "victim@example.com", ownerPassword); err != nil {
				t.Fatal(err)
			}
			// The owner enters the newest code in their mailbox, whichever
			// registration sent it.
			if err := f.svc.VerifyEmail(ctx, "victim@example.com", f.emails.last(t, "verify").code); err != nil {
				t.Fatalf("owner verify: %v", err)
			}
			if _, err := f.svc.Login(ctx, "victim@example.com", attackerPassword); !errors.Is(err, authdomain.ErrInvalidCredentials) {
				t.Errorf("Login(attacker's password) after the owner verified = %v, want ErrInvalidCredentials", err)
			}
		})
	}
}

// TestVerifyingRemovesWhatCameBefore: whatever an unverified account holds
// when its address is first proven, by code or password reset, is removed
// (security review AUTH-S-1).
func TestVerifyingRemovesWhatCameBefore(t *testing.T) {
	for _, proof := range []string{"verification code", "password reset"} {
		t.Run(proof, func(t *testing.T) {
			f := newFixture(t, withKeys, withPasskeys)
			ctx := requestCtx()
			if err := f.svc.Register(ctx, "eve@example.com", password); err != nil {
				t.Fatal(err)
			}
			u, err := f.svc.UserByEmail(operator(), "eve@example.com")
			if err != nil {
				t.Fatal(err)
			}
			// An operator turned on an authenticator app for the unverified
			// account.
			if _, _, err := f.svc.EnrollTOTP(operator(), u.ID); err != nil {
				t.Fatal(err)
			}
			switch proof {
			case "verification code":
				err = f.svc.VerifyEmail(ctx, "eve@example.com", f.emails.last(t, "verify").code)
			default:
				f.clock.advance(authlib.CodeResendInterval)
				if err := f.svc.RequestPasswordReset(ctx, "eve@example.com"); err != nil {
					t.Fatal(err)
				}
				err = f.svc.ResetPassword(ctx, "eve@example.com", f.emails.last(t, "reset").code, "the owner's new password")
			}
			if err != nil {
				t.Fatal(err)
			}
			res, err := f.svc.Login(ctx, "eve@example.com", map[string]string{"verification code": password, "password reset": "the owner's new password"}[proof])
			if err != nil || res.Challenge != nil || res.Token == "" {
				t.Errorf("Login() after verifying = %+v, %v; want a session without a second factor", res, err)
			}
		})
	}
}

func TestCodeChecksAreLimitedAcrossCodes(t *testing.T) {
	f := newFixture(t)
	ctx := requestCtx()
	f.signUp(t, "ada@example.com")
	f.signUp(t, "bob@example.com")

	// A new code every minute brings no new guesses once the address's
	// budget is spent (security review AUTH-S-2).
	guesses := 0
	var limited *authdomain.RateLimitError
	for guesses < 100 {
		f.clock.advance(authlib.CodeResendInterval)
		if err := f.svc.RequestPasswordReset(ctx, "ada@example.com"); err != nil {
			t.Fatal(err)
		}
		real := f.emails.last(t, "reset").code
		for range authlib.CodeMaxAttempts {
			guess := "000000"
			if guess == real {
				guess = "111111"
			}
			err := f.svc.ResetPassword(ctx, "ada@example.com", guess, "a new long password")
			if errors.As(err, &limited) {
				break
			}
			if !errors.Is(err, authdomain.ErrInvalidCode) {
				t.Fatalf("guess %d error = %v", guesses, err)
			}
			guesses++
		}
		if limited != nil {
			break
		}
	}
	if guesses != authlib.DefaultCodeAttempts || limited == nil || limited.RetryAfter <= 0 {
		t.Fatalf("wrong reset codes accepted = %d, limited %+v; want %d, then a *RateLimitError", guesses, limited, authlib.DefaultCodeAttempts)
	}
	// The right code is refused too until the budget refills; other
	// addresses, and unknown ones, have their own budget.
	if err := f.svc.ResetPassword(ctx, "ada@example.com", f.emails.last(t, "reset").code, "a new long password"); !errors.As(err, &limited) {
		t.Errorf("ResetPassword(right code, spent budget) error = %v, want *RateLimitError", err)
	}
	f.clock.advance(authlib.CodeResendInterval)
	if err := f.svc.RequestPasswordReset(ctx, "bob@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.ResetPassword(ctx, "bob@example.com", f.emails.last(t, "reset").code, "a new long password"); err != nil {
		t.Errorf("ResetPassword(other address) error = %v", err)
	}
	for range authlib.DefaultCodeAttempts {
		if err := f.svc.VerifyEmail(ctx, "nobody@example.com", "000000"); !errors.Is(err, authdomain.ErrInvalidCode) {
			t.Fatalf("VerifyEmail(unknown address) error = %v", err)
		}
	}
	if err := f.svc.VerifyEmail(ctx, "nobody@example.com", "000000"); !errors.As(err, &limited) {
		t.Errorf("VerifyEmail(unknown address, spent budget) error = %v, want *RateLimitError like a known address", err)
	}
	f.clock.advance(authlib.DefaultCodeWindow)
	if err := f.svc.RequestPasswordReset(ctx, "ada@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.ResetPassword(ctx, "ada@example.com", f.emails.last(t, "reset").code, "a new long password"); err != nil {
		t.Errorf("ResetPassword() a day later error = %v", err)
	}
}

// TestAnonymousFlowsTakeTheSameTime: registering, resending a code and
// asking for a password reset take at least MinResponseTime, whether or not
// the address has an account (security review AUTH-S-4). The reviewers
// measured a median of 2.5 ms for an existing account against 1.0 ms for a
// missing one without it. Registering hashes the password once or twice,
// which the race detector slows down, so it gets a longer floor.
func TestAnonymousFlowsTakeTheSameTime(t *testing.T) {
	ctx := requestCtx()
	for name, tt := range map[string]struct {
		floor time.Duration
		runs  func(f *fixture) []func() error
	}{
		"forgot password": {200 * time.Millisecond, func(f *fixture) []func() error {
			return []func() error{
				func() error { return f.svc.RequestPasswordReset(ctx, "ada@example.com") },
				func() error { return f.svc.RequestPasswordReset(ctx, "nobody@example.com") },
			}
		}},
		"resend": {200 * time.Millisecond, func(f *fixture) []func() error {
			return []func() error{
				func() error { return f.svc.ResendVerification(ctx, "eve@example.com") },
				func() error { return f.svc.ResendVerification(ctx, "nobody@example.com") },
			}
		}},
		"register": {time.Second, func(f *fixture) []func() error {
			return []func() error{
				func() error { return f.svc.Register(ctx, "ada@example.com", password) },
				func() error { return f.svc.Register(ctx, "eve@example.com", password) },
				func() error { return f.svc.Register(ctx, "new-"+authlib.NewID("x")+"@example.com", password) },
			}
		}},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, func(c *authusecase.Config) { c.MinResponseTime = tt.floor })
			f.signUp(t, "ada@example.com")
			if err := f.svc.Register(ctx, "eve@example.com", password); err != nil {
				t.Fatal(err)
			}
			var medians []time.Duration
			for _, run := range tt.runs(f) {
				var d []time.Duration
				for range 3 {
					f.clock.advance(authlib.CodeResendInterval)
					start := time.Now()
					if err := run(); err != nil {
						t.Fatal(err)
					}
					d = append(d, time.Since(start))
				}
				slices.Sort(d)
				medians = append(medians, d[1])
			}
			// Every outcome waits for the floor; what remains is scheduling
			// noise.
			if lo, hi := slices.Min(medians), slices.Max(medians); lo < tt.floor || hi-lo > tt.floor/4 {
				t.Errorf("medians %v, want each at least %v and within %v of each other", medians, tt.floor, tt.floor/4)
			}
		})
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

// TestLoginLimitIsPerNetwork: wrong passwords from one network don't lock
// the owner out on another, while an address-wide limit still bounds
// guessing from many networks (security review AUTH-S-6).
func TestLoginLimitIsPerNetwork(t *testing.T) {
	f := newFixture(t, func(c *authusecase.Config) { c.LoginAttempts, c.LoginWindow = 3, time.Minute })
	f.signUp(t, "ada@example.com")
	from := func(ip string) context.Context {
		return authlib.WithClientInfo(context.Background(), authlib.ClientInfo{IP: ip, UserAgent: "test-agent/1"})
	}
	var limited *authdomain.RateLimitError
	var attacker context.Context
	for i := range 4 {
		// Every address of one IPv6 /64 is one network.
		attacker = from(fmt.Sprintf("2001:db8:bad:1::%x", i+1))
		if _, err := f.svc.Login(attacker, "ada@example.com", "wrong password here"); i < 3 && !errors.Is(err, authdomain.ErrInvalidCredentials) {
			t.Fatalf("wrong password %d error = %v", i, err)
		} else if i == 3 && !errors.As(err, &limited) {
			t.Fatalf("wrong password %d from the same /64 error = %v, want *RateLimitError", i, err)
		}
	}
	if _, err := f.svc.Login(from("198.51.100.7"), "ada@example.com", password); err != nil {
		t.Errorf("Login() by the owner from another network error = %v, want signed in", err)
	}

	// From many networks, the address's own limit applies to everyone.
	for i := range authlib.DefaultLoginAddressAttempts {
		if _, err := f.svc.Login(from(fmt.Sprintf("203.0.%d.%d", i/200, i%200+1)), "ada@example.com", "wrong password here"); errors.As(err, &limited) {
			break
		}
	}
	if _, err := f.svc.Login(from("192.0.2.200"), "ada@example.com", password); !errors.As(err, &limited) {
		t.Errorf("Login() after the address-wide limit error = %v, want *RateLimitError", err)
	}
}

// TestChecksBehindASessionAreLimited: a stolen session can't guess the
// password or a second factor through the changes that check them (security
// review AUTH-S-5, AUTH-M-2).
func TestChecksBehindASessionAreLimited(t *testing.T) {
	f := newFixture(t, withKeys, withPasskeys)
	f.signUp(t, "ada@example.com")
	ctx, res := f.login(t, "ada@example.com")
	var limited *authdomain.RateLimitError
	guesses := []func(i int) error{
		func(i int) error {
			return f.svc.ChangePassword(ctx, fmt.Sprintf("wrong password %d", i), "a new long password")
		},
		func(i int) error {
			_, err := f.svc.StartTOTPEnrollment(ctx, fmt.Sprintf("wrong password %d", i))
			return err
		},
		func(i int) error {
			_, err := f.svc.BeginPasskeyRegistration(ctx, fmt.Sprintf("wrong password %d", i))
			return err
		},
		func(i int) error {
			return f.svc.DeleteAccount(ctx, fmt.Sprintf("wrong password %d", i), authdomain.SecondFactor{})
		},
		func(i int) error {
			return f.svc.RemoveIdentity(ctx, "idn_unknown", fmt.Sprintf("wrong password %d", i))
		},
	}
	wrong := 0
	for i := 0; i < 100; i++ {
		err := guesses[i%len(guesses)](i)
		if errors.As(err, &limited) {
			break
		}
		if !errors.Is(err, authdomain.ErrInvalidCredentials) {
			t.Fatalf("guess %d error = %v", i, err)
		}
		wrong++
	}
	if wrong != authlib.DefaultLoginAttempts || limited == nil {
		t.Fatalf("wrong passwords checked = %d, limited %v; want %d, then a *RateLimitError", wrong, limited, authlib.DefaultLoginAttempts)
	}
	if err := f.svc.ChangePassword(ctx, password, "a new long password"); !errors.As(err, &limited) {
		t.Errorf("ChangePassword(right password, spent budget) error = %v, want *RateLimitError", err)
	}
	failed := 0
	for _, e := range f.audit.events {
		if e.Action == "auth.reauth.failed" {
			failed++
		}
	}
	if e, _ := f.audit.find("auth.reauth.failed"); failed != wrong+2 || e.Metadata["reason"] != "rate_limited" || e.ResourceID != res.User.ID {
		t.Errorf("auth.reauth.failed events = %d, last %+v; want one per wrong password and refusal", failed, e)
	}
	// Signing in isn't affected, and the budget refills.
	if _, err := f.svc.Login(requestCtx(), "ada@example.com", password); err != nil {
		t.Errorf("Login() after spending the re-authentication budget error = %v", err)
	}
	f.clock.advance(authlib.DefaultLoginWindow)
	if err := f.svc.ChangePassword(ctx, password, "a new long password"); err != nil {
		t.Errorf("ChangePassword() after the window error = %v", err)
	}
}

// TestResetChecksTheCodeBeforeHashing: a wrong reset code costs no password
// hashing, so anonymous requests can't keep the hasher busy with it
// (security review AUTH-S-8). Signing in keeps the only hashing slot busy;
// with a 1 ms wait, hashing first would fail with auth.ErrHasherBusy.
func TestResetChecksTheCodeBeforeHashing(t *testing.T) {
	f := newFixture(t, func(c *authusecase.Config) {
		c.HashConcurrency, c.HashMaxWait = 1, time.Millisecond
		c.LoginAttempts, c.LoginWindow = 1_000_000, time.Hour
	})
	f.signUp(t, "ada@example.com")
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
					_, _ = f.svc.Login(authlib.WithClientInfo(context.Background(), authlib.ClientInfo{IP: "192.0.2.1"}), "nobody@example.com", password)
				}
			}
		})
	}
	busy := 0
	for range 30 {
		if _, err := f.svc.Login(requestCtx(), "nobody@example.com", password); errors.Is(err, authlib.ErrHasherBusy) {
			busy++
		}
	}
	for i := range authlib.DefaultCodeAttempts - 1 {
		if err := f.svc.ResetPassword(requestCtx(), "ada@example.com", "000000", "a new long password"); !errors.Is(err, authdomain.ErrInvalidCode) {
			t.Errorf("ResetPassword(wrong code %d) with a busy hasher error = %v, want ErrInvalidCode", i, err)
		}
	}
	close(stop)
	wg.Wait()
	if busy == 0 {
		t.Skip("the hasher was never busy; the check proved nothing on this machine")
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

// TestCleanupExpiresUnverifiedAccounts: accounts still unverified after
// auth.unverified_account_ttl are deleted, freeing the address, with one
// audit event counting them; verified accounts, accounts with a Google or
// Apple identity and recent registrations stay.
func TestCleanupExpiresUnverifiedAccounts(t *testing.T) {
	f := newSocialFixture(t, func(c *authusecase.Config) { c.UnverifiedAccountTTL = config.Static(48 * time.Hour) })
	ctx := requestCtx()
	for _, email := range []string{"old1@example.com", "old2@example.com"} {
		if err := f.svc.Register(ctx, email, password); err != nil {
			t.Fatal(err)
		}
	}
	f.signUp(t, "verified@example.com")
	if _, err := f.webSignIn(t, social.Google, socialtest.Claims{Subject: "g-social", Email: "social@corp.example", EmailVerified: true}, ""); err != nil {
		t.Fatal(err)
	}
	f.clock.advance(47 * time.Hour)
	if err := f.svc.Register(ctx, "recent@example.com", password); err != nil {
		t.Fatal(err)
	}
	// old2's owner asked for a new code just now: still verifying.
	if err := f.svc.ResendVerification(ctx, "old2@example.com"); err != nil {
		t.Fatal(err)
	}
	f.clock.advance(2 * time.Hour)

	res, err := f.svc.Cleanup(context.Background())
	if err != nil || res.Unverified != 1 {
		t.Fatalf("Cleanup() = %+v, %v; want 1 unverified account deleted", res, err)
	}
	if _, err := f.svc.UserByEmail(operator(), "old1@example.com"); !errors.Is(err, authdomain.ErrUserNotFound) {
		t.Errorf("UserByEmail(expired) error = %v, want ErrUserNotFound", err)
	}
	for _, email := range []string{"old2@example.com", "verified@example.com", "social@corp.example", "recent@example.com"} {
		if _, err := f.svc.UserByEmail(operator(), email); err != nil {
			t.Errorf("UserByEmail(%s) error = %v, want the account kept", email, err)
		}
	}
	e, ok := f.audit.find("auth.accounts.unverified_expired")
	if !ok || e.Metadata["count"] != int64(1) || len(e.Metadata) != 1 || e.ResourceID != "" {
		t.Errorf("audit event = %+v, want only the count", e)
	}
	// The address is free again, and a second run finds nothing.
	if err := f.svc.Register(ctx, "old1@example.com", "someone else's password"); err != nil {
		t.Fatal(err)
	}
	if res, err := f.svc.Cleanup(context.Background()); err != nil || res.Unverified != 0 {
		t.Errorf("Cleanup() again = %+v, %v; want nothing", res, err)
	}
}
