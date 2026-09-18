package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorbital.dev/config"
	authlib "gorbital.dev/modules/auth"

	authdomain "example.com/invoicing/internal/modules/auth/domain"
	authusecase "example.com/invoicing/internal/modules/auth/usecase"
)

func TestSessionExpiry(t *testing.T) {
	f := newFixture(t, func(c *authusecase.Config) {
		c.SessionIdleTTL, c.SessionAbsoluteTTL = config.Static(time.Hour), config.Static(3*time.Hour)
	})
	ctx := context.Background()
	f.signUp(t, "ada@example.com")
	_, res := f.login(t, "ada@example.com")

	// Each use more than a minute after the last extends the idle expiry, up
	// to the absolute expiry: at 50m, 1h40m and 2h30m.
	for range 3 {
		f.clock.advance(50 * time.Minute)
		p, err := f.svc.Authenticate(ctx, res.Token)
		if err != nil {
			t.Fatalf("Authenticate() at %v error = %v", f.clock.now(), err)
		}
		views, err := f.svc.Sessions(authlib.WithPrincipal(ctx, p))
		if err != nil || len(views) != 1 {
			t.Fatalf("Sessions() = %+v, %v", views, err)
		}
		want := f.clock.now().Add(time.Hour)
		if absolute := res.Session.CreatedAt.Add(3 * time.Hour); absolute.Before(want) {
			want = absolute
		}
		if !views[0].ExpiresAt().Equal(want) {
			t.Errorf("ExpiresAt = %v, want %v", views[0].ExpiresAt(), want)
		}
	}
	f.clock.advance(31 * time.Minute) // idle-valid, but past the absolute expiry
	if _, err := f.svc.Authenticate(ctx, res.Token); !errors.Is(err, authlib.ErrUnauthenticated) {
		t.Errorf("Authenticate() past the absolute expiry error = %v", err)
	}

	_, idle := f.login(t, "ada@example.com")
	f.clock.advance(61 * time.Minute)
	if _, err := f.svc.Authenticate(ctx, idle.Token); !errors.Is(err, authlib.ErrUnauthenticated) {
		t.Errorf("Authenticate() after an idle hour error = %v", err)
	}
	for _, token := range []string{"", "not-a-token", string(make([]byte, 300))} {
		if _, err := f.svc.Authenticate(ctx, token); !errors.Is(err, authlib.ErrUnauthenticated) {
			t.Errorf("Authenticate(%q) error = %v", token, err)
		}
	}
}

func TestSettingsAreClampedToHardLimits(t *testing.T) {
	f := newFixture(t, func(c *authusecase.Config) {
		c.SessionIdleTTL, c.VerificationCodeTTL = config.Static(time.Second), config.Static(24*time.Hour)
	})
	ctx := requestCtx()
	if err := f.svc.Register(ctx, "ada@example.com", password); err != nil {
		t.Fatal(err)
	}
	if ttl := f.emails.last(t, "verify").ttl; ttl != time.Hour {
		t.Errorf("verification code TTL = %v, want clamped to 1h", ttl)
	}
	if err := f.svc.VerifyEmail(ctx, "ada@example.com", f.emails.last(t, "verify").code); err != nil {
		t.Fatal(err)
	}
	_, res := f.login(t, "ada@example.com")
	if want := f.clock.now().Add(5 * time.Minute); !res.Session.ExpiresAt().Equal(want) {
		t.Errorf("session ExpiresAt = %v, want the 5-minute minimum idle TTL", res.Session.ExpiresAt())
	}
}

func TestLogoutAndSessions(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.signUp(t, "ada@example.com")
	f.signUp(t, "bob@example.com")
	laptop, laptopRes := f.login(t, "ada@example.com")
	f.clock.advance(2 * time.Minute)
	phone, phoneRes := f.login(t, "ada@example.com")
	bob, bobRes := f.login(t, "bob@example.com")

	views, err := f.svc.Sessions(phone)
	if err != nil || len(views) != 2 || views[0].ID != phoneRes.Session.ID || !views[0].Current || views[1].Current {
		t.Fatalf("Sessions() = %+v, %v", views, err)
	}
	me, err := f.svc.Me(phone)
	if err != nil || me.User.Email != "ada@example.com" || me.Session.ID != phoneRes.Session.ID {
		t.Errorf("Me() = %+v, %v", me, err)
	}
	if err := f.svc.RevokeSession(bob, laptopRes.Session.ID); !errors.Is(err, authdomain.ErrSessionNotFound) {
		t.Errorf("RevokeSession(another user's session) error = %v", err)
	}
	if err := f.svc.RevokeSession(phone, laptopRes.Session.ID); err != nil {
		t.Fatalf("RevokeSession() error = %v", err)
	}
	if _, err := f.svc.Authenticate(ctx, laptopRes.Token); !errors.Is(err, authlib.ErrUnauthenticated) {
		t.Errorf("revoked session error = %v", err)
	}
	if err := f.svc.Logout(laptop); err != nil {
		t.Errorf("Logout() of an already revoked session error = %v", err)
	}
	if err := f.svc.Logout(phone); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Authenticate(ctx, phoneRes.Token); !errors.Is(err, authlib.ErrUnauthenticated) {
		t.Errorf("session after Logout() error = %v", err)
	}

	_, second := f.login(t, "bob@example.com")
	if n, err := f.svc.LogoutAll(bob); err != nil || n != 2 {
		t.Errorf("LogoutAll() = %d, %v; want 2", n, err)
	}
	for _, token := range []string{bobRes.Token, second.Token} {
		if _, err := f.svc.Authenticate(ctx, token); !errors.Is(err, authlib.ErrUnauthenticated) {
			t.Errorf("session after LogoutAll() error = %v", err)
		}
	}
	if _, err := f.svc.Sessions(ctx); !errors.Is(err, authlib.ErrUnauthenticated) {
		t.Errorf("Sessions() without a principal error = %v", err)
	}
}

func TestRolesGrantPermissions(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.signUp(t, "ada@example.com")
	_, res := f.login(t, "ada@example.com")
	userID := res.User.ID

	if err := f.svc.GrantRole(ctx, userID, "platform_admin"); !errors.Is(err, authdomain.ErrActorRequired) {
		t.Errorf("GrantRole() without actor error = %v", err)
	}
	if err := f.svc.GrantRole(operator(), userID, "superuser"); !errors.Is(err, authdomain.ErrUnknownRole) {
		t.Errorf("GrantRole(unknown) error = %v", err)
	}
	if err := f.svc.GrantRole(operator(), "usr_missing", "viewer"); !errors.Is(err, authdomain.ErrUserNotFound) {
		t.Errorf("GrantRole(missing user) error = %v", err)
	}
	// An unverified account gets no role: whoever verifies the address later
	// may not be who registered it.
	if err := f.svc.Register(requestCtx(), "pending@example.com", password); err != nil {
		t.Fatal(err)
	}
	pending, _ := f.svc.UserByEmail(operator(), "pending@example.com")
	if err := f.svc.GrantRole(operator(), pending.ID, "viewer"); !errors.Is(err, authdomain.ErrEmailNotVerified) {
		t.Errorf("GrantRole(unverified account) error = %v, want ErrEmailNotVerified", err)
	}
	if u, _ := f.svc.User(operator(), pending.ID); len(u.Roles) != 0 {
		t.Errorf("unverified account roles = %v, want none", u.Roles)
	}
	for range 2 {
		if err := f.svc.GrantRole(operator(), userID, "viewer"); err != nil {
			t.Fatalf("GrantRole() error = %v", err)
		}
	}
	granted := 0
	for _, a := range f.audit.actions() {
		if a == "auth.role.granted" {
			granted++
		}
	}
	if granted != 1 {
		t.Errorf("role granted events = %d, want 1 (the second grant changes nothing)", granted)
	}

	p, err := f.svc.Authenticate(ctx, res.Token)
	if err != nil || !hasAll(p.Permissions, "ops.settings.read") || hasAll(p.Permissions, "ops.settings.write") {
		t.Fatalf("permissions as viewer = %v, %v", p.Permissions, err)
	}
	if err := f.svc.GrantRole(operator(), userID, "platform_admin"); err != nil {
		t.Fatal(err)
	}
	if p, _ := f.svc.Authenticate(ctx, res.Token); !hasAll(p.Permissions, "ops.settings.read", "ops.settings.write") || len(p.Permissions) != 4 {
		t.Errorf("permissions as admin = %v, want both and the user role's, once each", p.Permissions)
	}
	if err := f.svc.RevokeRole(operator(), userID, "platform_admin"); err != nil {
		t.Fatal(err)
	}
	if u, _ := f.svc.User(ctx, userID); len(u.Roles) != 1 || u.Roles[0] != "viewer" {
		t.Errorf("roles after revoking = %v", u.Roles)
	}

	// A role stored for a user but no longer in the catalog grants nothing.
	if _, err := f.pool.Exec(ctx, "INSERT INTO auth_user_roles (user_id, role, granted_at, granted_by) VALUES ($1, 'retired_role', now(), 'test')", userID); err != nil {
		t.Fatal(err)
	}
	if p, _ := f.svc.Authenticate(ctx, res.Token); len(p.Permissions) != 3 {
		t.Errorf("permissions with an unknown stored role = %v", p.Permissions)
	}
}
