package usecase_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"gorbital.dev/actor"
	"gorbital.dev/config"
	authlib "gorbital.dev/modules/auth"
	"gorbital.dev/ratelimit"

	authdomain "example.com/plateful/internal/modules/auth/domain"
	authrepository "example.com/plateful/internal/modules/auth/repository"
	authusecase "example.com/plateful/internal/modules/auth/usecase"
)

// keyCatalog has the user role every user holds (their notes), a role
// without two-factor authentication (reporter), ops roles that require it,
// and a role mixing both kinds of permissions.
func keyCatalog() *authlib.Catalog {
	c := authlib.NewCatalog()
	c.Permission("notes.note.read", "See your notes")
	c.Permission("notes.note.write", "Change your notes")
	c.Role(authusecase.RoleUser, "Every user", "notes.note.read", "notes.note.write")
	c.Permission("reports.report.read", "Read reports")
	c.Permission("reports.report.write", "Write reports")
	c.Permission("ops.settings.read", "Read runtime settings")
	c.Permission(authusecase.PermServiceAccountsRead, "See service accounts")
	c.Permission(authusecase.PermServiceAccountsWrite, "Change service accounts")
	c.Role("reporter", "Works with reports", "reports.report.read", "reports.report.write")
	c.Role("reader", "Reads reports", "reports.report.read")
	c.Role("platform_admin", "Operates the platform", "ops.settings.read", authusecase.PermServiceAccountsRead, authusecase.PermServiceAccountsWrite)
	c.Role("auditor", "Reads reports and settings", "reports.report.read", "ops.settings.read")
	c.RequireMFA("platform_admin", "auditor")
	return c
}

func newKeyFixture(t *testing.T, configure ...func(*authusecase.Config)) *fixture {
	t.Helper()
	return newFixture(t, append([]func(*authusecase.Config){func(c *authusecase.Config) { c.Catalog = keyCatalog() }}, configure...)...)
}

// user signs up email, grants roles and returns a signed-in request context
// and the user ID.
func (f *fixture) user(t *testing.T, email string, roles ...string) (context.Context, string) {
	t.Helper()
	f.signUp(t, email)
	u, err := f.svc.UserByEmail(context.Background(), email)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range roles {
		if err := f.svc.GrantRole(operator(), u.ID, r); err != nil {
			t.Fatalf("GrantRole(%s) error = %v", r, err)
		}
	}
	ctx, _ := f.login(t, email)
	return ctx, u.ID
}

// operatorSession is a request of a platform_admin whose session verified a
// second factor a minute ago.
func (f *fixture) operatorSession(t *testing.T) context.Context {
	t.Helper()
	_, id := f.user(t, "operator@example.com", "platform_admin")
	now := f.clock.now()
	p := authlib.Principal{UserID: id, SessionID: "ses_operator", MFAVerified: true, MFAVerifiedAt: now.Add(-time.Minute), SignedInAt: now.Add(-time.Hour)}
	p.Permissions, p.StepUp = keyCatalog().PermissionsFor([]string{"platform_admin"}, true)
	return authlib.WithPrincipal(requestCtx(), p)
}

func (f *fixture) newKey(t *testing.T, ctx context.Context, in authusecase.APIKeyInput) authusecase.CreatedAPIKey {
	t.Helper()
	if in.Name == "" {
		in.Name = "test key"
	}
	if in.ExpiresAt.IsZero() {
		in.ExpiresAt = f.clock.now().Add(30 * 24 * time.Hour)
	}
	created, err := f.svc.CreateAPIKey(ctx, password, in)
	if err != nil {
		t.Fatalf("CreateAPIKey(%+v) error = %v", in, err)
	}
	return created
}

// keyCtx is a request authenticated with key, as the middleware prepares
// it.
func (f *fixture) keyCtx(t *testing.T, key string) context.Context {
	t.Helper()
	p, err := f.svc.AuthenticateAPIKey(requestCtx(), key)
	if err != nil {
		t.Fatalf("AuthenticateAPIKey() error = %v", err)
	}
	return authlib.WithPrincipal(requestCtx(), p)
}

func TestAPIKeyAuthentication(t *testing.T) {
	f := newKeyFixture(t)
	ctx, userID := f.user(t, "ada@example.com", "reporter")
	created := f.newKey(t, ctx, authusecase.APIKeyInput{Name: " CI deploys ", Scopes: []string{"reports.report.read"}})
	k := created.APIKey
	if !strings.HasPrefix(created.Key, authlib.APIKeyPrefix+k.LookupID+"_") || k.Name != "CI deploys" || k.UserID != userID || k.ServiceAccountID != "" {
		t.Fatalf("CreateAPIKey() = %+v, key %q", k, created.Key)
	}

	p, err := f.svc.AuthenticateAPIKey(requestCtx(), created.Key)
	if err != nil || p.UserID != userID || p.APIKeyID != k.ID || p.SessionID != "" || !slices.Equal(p.Permissions, []string{"reports.report.read"}) || len(p.StepUp) != 0 {
		t.Fatalf("AuthenticateAPIKey() = %+v, %v; want the user limited to the key's scope", p, err)
	}
	if a := actor.FromOrAnonymous(authlib.WithPrincipal(context.Background(), p)); a.Kind != actor.KindUser || a.Can("reports.report.write") {
		t.Errorf("actor = %+v, want a user without reports.report.write", a)
	}

	// Parsing edge cases, a wrong secret and an unknown lookup ID all fail
	// the same way.
	wrongSecret := created.Key[:len(created.Key)-1] + map[bool]string{true: "b", false: "a"}[strings.HasSuffix(created.Key, "a")]
	other, _, _ := authlib.NewAPIKey()
	for name, key := range map[string]string{
		"wrong secret":     wrongSecret,
		"unknown lookup":   other,
		"lookup of a key":  authlib.APIKeyPrefix + k.LookupID + "_" + strings.Repeat("a", 52),
		"uppercase":        strings.ToUpper(created.Key),
		"truncated":        created.Key[:40],
		"with whitespace":  created.Key + " ",
		"empty":            "",
		"session-shaped":   "c2Vzc2lvbi10b2tlbg",
		"another prefix":   "gbx_" + created.Key[4:],
		"lookup ID alone":  k.LookupID,
		"hash of the key":  fmt.Sprintf("%x", authlib.HashAPIKey(created.Key)),
		"key twice joined": created.Key + created.Key,
	} {
		if _, err := f.svc.AuthenticateAPIKey(requestCtx(), key); !errors.Is(err, authlib.ErrUnauthenticated) {
			t.Errorf("%s: AuthenticateAPIKey() error = %v, want ErrUnauthenticated", name, err)
		}
	}

	// Last use is recorded at most once a minute.
	lastUsed := func() *time.Time {
		keys, err := f.svc.ListAPIKeys(ctx)
		if err != nil || len(keys) != 1 {
			t.Fatalf("ListAPIKeys() = %+v, %v", keys, err)
		}
		return keys[0].LastUsedAt
	}
	first := lastUsed()
	if first == nil || !first.Equal(f.clock.now()) {
		t.Fatalf("LastUsedAt = %v, want %v", first, f.clock.now())
	}
	f.clock.advance(30 * time.Second)
	f.keyCtx(t, created.Key)
	if got := lastUsed(); !got.Equal(*first) {
		t.Errorf("LastUsedAt after 30 seconds = %v, want unchanged %v", got, first)
	}
	f.clock.advance(31 * time.Second)
	f.keyCtx(t, created.Key)
	if got := lastUsed(); !got.Equal(f.clock.now()) {
		t.Errorf("LastUsedAt after a minute = %v, want %v", got, f.clock.now())
	}

	// Expired keys fail from their expiry on.
	short := f.newKey(t, ctx, authusecase.APIKeyInput{ExpiresAt: f.clock.now().Add(2 * time.Hour)})
	f.clock.advance(2*time.Hour - time.Second)
	f.keyCtx(t, short.Key)
	f.clock.advance(time.Second)
	if _, err := f.svc.AuthenticateAPIKey(requestCtx(), short.Key); !errors.Is(err, authlib.ErrUnauthenticated) {
		t.Errorf("AuthenticateAPIKey() at expiry error = %v, want ErrUnauthenticated", err)
	}

	// Revoked keys fail at once; revoking twice changes nothing, and nobody
	// else can revoke a key.
	otherCtx, _ := f.user(t, "bob@example.com")
	if err := f.svc.RevokeAPIKey(otherCtx, k.ID); !errors.Is(err, authdomain.ErrAPIKeyNotFound) {
		t.Errorf("RevokeAPIKey() by another user error = %v, want ErrAPIKeyNotFound", err)
	}
	f.keyCtx(t, created.Key)
	for range 2 {
		if err := f.svc.RevokeAPIKey(ctx, k.ID); err != nil {
			t.Fatalf("RevokeAPIKey() error = %v", err)
		}
	}
	if _, err := f.svc.AuthenticateAPIKey(requestCtx(), created.Key); !errors.Is(err, authlib.ErrUnauthenticated) {
		t.Errorf("AuthenticateAPIKey() after revoking error = %v, want ErrUnauthenticated", err)
	}
	revoked := 0
	for _, a := range f.audit.actions() {
		if a == "auth.api_key.revoked" {
			revoked++
		}
	}
	if revoked != 1 {
		t.Errorf("auth.api_key.revoked recorded %d times, want once", revoked)
	}
	if keys, _ := f.svc.ListAPIKeys(ctx); len(keys) != 2 || keys[0].StatusAt(f.clock.now()) != authdomain.APIKeyStatusExpired || keys[1].StatusAt(f.clock.now()) != authdomain.APIKeyStatusRevoked {
		t.Errorf("ListAPIKeys() = %+v, want the expired key then the revoked one", keys)
	}
}

func TestDeletedOrDisabledOwnersKeysFail(t *testing.T) {
	f := newKeyFixture(t)
	ctx, userID := f.user(t, "ada@example.com", "reporter")
	created := f.newKey(t, ctx, authusecase.APIKeyInput{})

	// A deleted account's key fails even before cleanup revokes it: the
	// account is looked up with the key.
	if _, err := f.pool.Exec(context.Background(), "UPDATE auth_users SET deleted_at = now() WHERE id = $1", userID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.AuthenticateAPIKey(requestCtx(), created.Key); !errors.Is(err, authlib.ErrUnauthenticated) {
		t.Errorf("AuthenticateAPIKey() for a deleted account error = %v, want ErrUnauthenticated", err)
	}
	if _, err := f.pool.Exec(context.Background(), "UPDATE auth_users SET deleted_at = NULL WHERE id = $1", userID); err != nil {
		t.Fatal(err)
	}
	f.keyCtx(t, created.Key)

	// Deleting the account revokes its keys.
	if err := f.svc.DeleteAccount(ctx, password, authdomain.SecondFactor{}); err != nil {
		t.Fatal(err)
	}
	var reason string
	if err := f.pool.QueryRow(context.Background(), "SELECT revoked_reason FROM auth_api_keys WHERE id = $1", created.APIKey.ID).Scan(&reason); err != nil || reason != authdomain.RevokedAccountDeleted {
		t.Errorf("revoked_reason after account deletion = %q, %v", reason, err)
	}

	// Resetting the password revokes them too.
	ctx, _ = f.user(t, "bob@example.com")
	bobKey := f.newKey(t, ctx, authusecase.APIKeyInput{})
	if err := f.svc.RequestPasswordReset(requestCtx(), "bob@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.ResetPassword(requestCtx(), "bob@example.com", f.emails.last(t, "reset").code, "another long password"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.AuthenticateAPIKey(requestCtx(), bobKey.Key); !errors.Is(err, authlib.ErrUnauthenticated) {
		t.Errorf("AuthenticateAPIKey() after a password reset error = %v, want ErrUnauthenticated", err)
	}
	if e, ok := f.audit.find("auth.api_key.revoked"); !ok || e.Metadata["reason"] != authdomain.RevokedPasswordReset || e.Metadata["count"] != int64(1) {
		t.Errorf("auth.api_key.revoked after a reset = %+v, want one key with the reason", e)
	}

	// A disabled service account's keys fail, and enabling it doesn't bring
	// them back.
	op := f.operatorSession(t)
	account, err := f.svc.CreateServiceAccount(op, "", authusecase.ServiceAccountInput{Name: "Billing sync", Roles: []string{"reporter"}})
	if err != nil {
		t.Fatal(err)
	}
	saKey, err := f.svc.CreateServiceAccountKey(op, "", account.ID, "", authusecase.APIKeyInput{Name: "sync", ExpiresAt: f.clock.now().Add(24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	f.keyCtx(t, saKey.Key)
	yes, no := true, false
	if _, err := f.svc.UpdateServiceAccount(op, "", account.ID, authusecase.ServiceAccountPatch{Disabled: &yes}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.AuthenticateAPIKey(requestCtx(), saKey.Key); !errors.Is(err, authlib.ErrUnauthenticated) {
		t.Errorf("AuthenticateAPIKey() of a disabled service account error = %v, want ErrUnauthenticated", err)
	}
	if _, err := f.svc.CreateServiceAccountKey(op, "", account.ID, "", authusecase.APIKeyInput{Name: "again", ExpiresAt: f.clock.now().Add(24 * time.Hour)}); !errors.Is(err, authdomain.ErrServiceAccountDisabled) {
		t.Errorf("CreateServiceAccountKey() for a disabled service account error = %v, want ErrServiceAccountDisabled", err)
	}
	if _, err := f.svc.UpdateServiceAccount(op, "", account.ID, authusecase.ServiceAccountPatch{Disabled: &no}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.AuthenticateAPIKey(requestCtx(), saKey.Key); !errors.Is(err, authlib.ErrUnauthenticated) {
		t.Errorf("AuthenticateAPIKey() after enabling again error = %v, want the revoked key to stay revoked", err)
	}
	if _, ok := f.audit.find("auth.service_account.disabled"); !ok {
		t.Error("no auth.service_account.disabled event")
	}

	// Deleting a service account deletes its keys.
	again, err := f.svc.CreateServiceAccountKey(op, "", account.ID, "", authusecase.APIKeyInput{Name: "again", ExpiresAt: f.clock.now().Add(24 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.DeleteServiceAccount(op, "", account.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.AuthenticateAPIKey(requestCtx(), again.Key); !errors.Is(err, authlib.ErrUnauthenticated) {
		t.Errorf("AuthenticateAPIKey() of a deleted service account error = %v, want ErrUnauthenticated", err)
	}
	if _, err := f.svc.GetServiceAccount(op, "", account.ID); !errors.Is(err, authdomain.ErrServiceAccountNotFound) {
		t.Errorf("GetServiceAccount() after deleting error = %v", err)
	}
}

// TestAPIKeysNeverCarryTwoFactorPermissions checks that no path gives an API
// key a permission only a role requiring two-factor authentication grants.
func TestAPIKeysNeverCarryTwoFactorPermissions(t *testing.T) {
	f := newKeyFixture(t)
	ctx, userID := f.user(t, "ada@example.com", "reporter", "auditor", "platform_admin")

	for _, scopes := range [][]string{{"ops.settings.read"}, {authusecase.PermServiceAccountsWrite}, {"reports.report.read", "ops.settings.read"}} {
		if _, err := f.svc.CreateAPIKey(ctx, password, authusecase.APIKeyInput{Name: "ops", Scopes: scopes, ExpiresAt: f.clock.now().Add(time.Hour * 2)}); !errors.Is(err, authdomain.ErrInvalidAPIKeyScopes) {
			t.Errorf("CreateAPIKey(scopes %v) error = %v, want ErrInvalidAPIKeyScopes", scopes, err)
		}
	}
	unscoped := f.newKey(t, ctx, authusecase.APIKeyInput{})
	p, err := f.svc.AuthenticateAPIKey(requestCtx(), unscoped.Key)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"notes.note.read", "notes.note.write", "reports.report.read", "reports.report.write"}; !slices.Equal(p.Permissions, want) || len(p.StepUp) != 0 {
		t.Errorf("unscoped key of a platform admin: permissions %v, step-up %v; want %v and none", p.Permissions, p.StepUp, want)
	}
	keyCtx := authlib.WithPrincipal(requestCtx(), p)
	if err := actor.Require(keyCtx, "ops.settings.read"); !errors.Is(err, actor.ErrForbidden) {
		t.Errorf("actor.Require(ops.settings.read) with a key error = %v, want ErrForbidden (never step-up)", err)
	}
	if _, err := f.svc.ListServiceAccounts(keyCtx, ""); !errors.Is(err, authdomain.ErrSessionRequired) {
		t.Errorf("ListServiceAccounts() with a key error = %v, want ErrSessionRequired", err)
	}

	// A key scoped before a role started requiring 2FA gets nothing from it:
	// the stored scope names a permission the key's roles now withhold.
	if _, err := f.pool.Exec(context.Background(), "UPDATE auth_api_keys SET scopes = '{ops.settings.read}' WHERE id = $1", unscoped.APIKey.ID); err != nil {
		t.Fatal(err)
	}
	if p, err := f.svc.AuthenticateAPIKey(requestCtx(), unscoped.Key); err != nil || len(p.Permissions) != 0 {
		t.Errorf("key scoped to ops.settings.read: permissions %v, %v; want none", p.Permissions, err)
	}

	// Service accounts can't be given such roles, even a mixed one.
	op := f.operatorSession(t)
	for _, roles := range [][]string{{"platform_admin"}, {"auditor"}, {"reporter", "auditor"}, {"undeclared"}} {
		if _, err := f.svc.CreateServiceAccount(op, "", authusecase.ServiceAccountInput{Name: "robot", Roles: roles}); !errors.Is(err, authdomain.ErrInvalidServiceAccountRole) {
			t.Errorf("CreateServiceAccount(roles %v) error = %v, want ErrInvalidServiceAccountRole", roles, err)
		}
	}
	account, err := f.svc.CreateServiceAccount(op, "", authusecase.ServiceAccountInput{Name: "robot", Roles: []string{"reporter"}})
	if err != nil {
		t.Fatal(err)
	}
	roles := []string{"platform_admin"}
	if _, err := f.svc.UpdateServiceAccount(op, "", account.ID, authusecase.ServiceAccountPatch{Roles: &roles}); !errors.Is(err, authdomain.ErrInvalidServiceAccountRole) {
		t.Errorf("UpdateServiceAccount(roles platform_admin) error = %v, want ErrInvalidServiceAccountRole", err)
	}
	if _, err := f.svc.CreateServiceAccountKey(op, "", account.ID, "", authusecase.APIKeyInput{Name: "k", Scopes: []string{"ops.settings.read"}, ExpiresAt: f.clock.now().Add(2 * time.Hour)}); !errors.Is(err, authdomain.ErrInvalidAPIKeyScopes) {
		t.Errorf("CreateServiceAccountKey(scope ops.settings.read) error = %v, want ErrInvalidAPIKeyScopes", err)
	}
	saKey, err := f.svc.CreateServiceAccountKey(op, "", account.ID, "", authusecase.APIKeyInput{Name: "k", ExpiresAt: f.clock.now().Add(2 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	// A role requiring 2FA written into the table anyway grants nothing.
	if _, err := f.pool.Exec(context.Background(), "UPDATE auth_service_accounts SET roles = '{platform_admin,auditor}' WHERE id = $1", account.ID); err != nil {
		t.Fatal(err)
	}
	if p, err := f.svc.AuthenticateAPIKey(requestCtx(), saKey.Key); err != nil || len(p.Permissions) != 0 || len(p.StepUp) != 0 || p.ServiceAccountID != account.ID {
		t.Errorf("service account with ops roles in the table: %+v, %v; want no permissions", p, err)
	}
	_ = userID
}

// TestAPIKeyScopesCantEscalate checks that a key never exceeds its owner's
// current permissions or its own scopes.
func TestAPIKeyScopesCantEscalate(t *testing.T) {
	f := newKeyFixture(t)
	ctx, userID := f.user(t, "ada@example.com", "reader")
	if _, err := f.svc.CreateAPIKey(ctx, password, authusecase.APIKeyInput{Name: "write", Scopes: []string{"reports.report.write"}, ExpiresAt: f.clock.now().Add(2 * time.Hour)}); !errors.Is(err, authdomain.ErrInvalidAPIKeyScopes) {
		t.Errorf("CreateAPIKey(scope the user lacks) error = %v, want ErrInvalidAPIKeyScopes", err)
	}
	for _, scopes := range [][]string{{"Reports.Report.Read"}, {"reports report read"}, {""}, {strings.Repeat("a", 201)}} {
		if _, err := f.svc.CreateAPIKey(ctx, password, authusecase.APIKeyInput{Name: "bad", Scopes: scopes, ExpiresAt: f.clock.now().Add(2 * time.Hour)}); !errors.Is(err, authdomain.ErrInvalidAPIKeyScopes) {
			t.Errorf("CreateAPIKey(scopes %q) error = %v, want ErrInvalidAPIKeyScopes", scopes, err)
		}
	}
	many := make([]string, authdomain.MaxAPIKeyScopes+1)
	for i := range many {
		many[i] = fmt.Sprintf("reports.report.read%d", i)
	}
	if _, err := f.svc.CreateAPIKey(ctx, password, authusecase.APIKeyInput{Name: "many", Scopes: many, ExpiresAt: f.clock.now().Add(2 * time.Hour)}); !errors.Is(err, authdomain.ErrInvalidAPIKeyScopes) {
		t.Errorf("CreateAPIKey(51 scopes) error = %v, want ErrInvalidAPIKeyScopes", err)
	}

	scoped := f.newKey(t, ctx, authusecase.APIKeyInput{Scopes: []string{"reports.report.read", "reports.report.read"}})
	unscoped := f.newKey(t, ctx, authusecase.APIKeyInput{})
	if !slices.Equal(scoped.APIKey.Scopes, []string{"reports.report.read"}) {
		t.Errorf("scopes = %v, want duplicates removed", scoped.APIKey.Scopes)
	}
	perms := func(key string) []string {
		p, err := f.svc.AuthenticateAPIKey(requestCtx(), key)
		if err != nil {
			t.Fatal(err)
		}
		return p.Permissions
	}

	// Gaining a role widens an unscoped key, never a scoped one.
	if err := f.svc.GrantRole(operator(), userID, "reporter"); err != nil {
		t.Fatal(err)
	}
	if got := perms(scoped.Key); !slices.Equal(got, []string{"reports.report.read"}) {
		t.Errorf("scoped key after a new role = %v, want its scope only", got)
	}
	if got := perms(unscoped.Key); !slices.Equal(got, []string{"notes.note.read", "notes.note.write", "reports.report.read", "reports.report.write"}) {
		t.Errorf("unscoped key after a new role = %v", got)
	}
	// Losing roles narrows both at once; the user role stays.
	for _, r := range []string{"reader", "reporter"} {
		if err := f.svc.RevokeRole(operator(), userID, r); err != nil {
			t.Fatal(err)
		}
	}
	if got, got2 := perms(scoped.Key), perms(unscoped.Key); len(got) != 0 || !slices.Equal(got2, []string{"notes.note.read", "notes.note.write"}) {
		t.Errorf("keys after losing every granted role = %v, %v; want none and the user role's", got, got2)
	}
}

// TestUserRoleIsScopedLikeAnyRole checks the user role every user holds
// (ADR-0058): sessions get its permissions, API keys only within their
// scopes, and nobody grants it or gives it to a service account.
func TestUserRoleIsScopedLikeAnyRole(t *testing.T) {
	f := newKeyFixture(t)
	ctx, userID := f.user(t, "ada@example.com")
	p, _ := authlib.PrincipalFrom(ctx)
	if !slices.Equal(p.Permissions, []string{"notes.note.read", "notes.note.write"}) {
		t.Errorf("session without granted roles has permissions %v, want the user role's", p.Permissions)
	}
	readOnly := f.newKey(t, ctx, authusecase.APIKeyInput{Scopes: []string{"notes.note.read"}})
	keyCtx := f.keyCtx(t, readOnly.Key)
	if err := actor.Require(keyCtx, "notes.note.read"); err != nil {
		t.Errorf("read-scoped key: Require(notes.note.read) error = %v", err)
	}
	if err := actor.Require(keyCtx, "notes.note.write"); !errors.Is(err, actor.ErrForbidden) {
		t.Errorf("read-scoped key: Require(notes.note.write) error = %v, want ErrForbidden", err)
	}
	unscoped := f.keyCtx(t, f.newKey(t, ctx, authusecase.APIKeyInput{}).Key)
	if err := actor.Require(unscoped, "notes.note.write"); err != nil {
		t.Errorf("unscoped key: Require(notes.note.write) error = %v", err)
	}

	if err := f.svc.GrantRole(operator(), userID, authusecase.RoleUser); !errors.Is(err, authdomain.ErrUnknownRole) {
		t.Errorf("GrantRole(user) error = %v, want ErrUnknownRole", err)
	}
	admin := f.operatorSession(t)
	if _, err := f.svc.CreateServiceAccount(admin, "", authusecase.ServiceAccountInput{Name: "robot", Roles: []string{authusecase.RoleUser}}); !errors.Is(err, authdomain.ErrInvalidServiceAccountRole) {
		t.Errorf("CreateServiceAccount(user role) error = %v, want ErrInvalidServiceAccountRole", err)
	}

	for name, c := range map[string]*authlib.Catalog{
		"no user role":            catalogWithUserRole(false, false),
		"user role requiring 2FA": catalogWithUserRole(true, true),
	} {
		if _, err := authusecase.NewService(authusecase.Config{Store: authrepository.NewStore(f.pool), Catalog: c, Recorder: f.audit, Emails: f.emails}); err == nil ||
			!strings.Contains(err.Error(), "user role") {
			t.Errorf("NewService(%s) error = %v, want the user role required", name, err)
		}
	}
}

// catalogWithUserRole returns a catalog with or without the user role, which
// may require two-factor authentication.
func catalogWithUserRole(declare, mfa bool) *authlib.Catalog {
	c := authlib.NewCatalog()
	c.Permission("notes.note.read", "See your notes")
	if declare {
		c.Role(authusecase.RoleUser, "Every user", "notes.note.read")
	}
	if mfa {
		c.RequireMFA(authusecase.RoleUser)
	}
	return c
}

// TestAPIKeysNeedASession checks that a key can't manage the account, its
// sessions, or keys and service accounts.
func TestAPIKeysNeedASession(t *testing.T) {
	f := newKeyFixture(t)
	ctx, _ := f.user(t, "ada@example.com", "reporter")
	created := f.newKey(t, ctx, authusecase.APIKeyInput{})
	keyCtx := f.keyCtx(t, created.Key)
	in := authusecase.APIKeyInput{Name: "more", ExpiresAt: f.clock.now().Add(2 * time.Hour)}

	checks := map[string]error{}
	_, checks["CreateAPIKey"] = f.svc.CreateAPIKey(keyCtx, password, in)
	_, checks["ListAPIKeys"] = f.svc.ListAPIKeys(keyCtx)
	checks["RevokeAPIKey"] = f.svc.RevokeAPIKey(keyCtx, created.APIKey.ID)
	_, checks["Me"] = f.svc.Me(keyCtx)
	_, checks["Sessions"] = f.svc.Sessions(keyCtx)
	checks["Logout"] = f.svc.Logout(keyCtx)
	checks["ChangePassword"] = f.svc.ChangePassword(keyCtx, password, "a brand new password")
	checks["DeleteAccount"] = f.svc.DeleteAccount(keyCtx, password, authdomain.SecondFactor{})
	_, checks["StartTOTPEnrollment"] = f.svc.StartTOTPEnrollment(keyCtx, password)
	_, checks["CreateServiceAccount"] = f.svc.CreateServiceAccount(keyCtx, "", authusecase.ServiceAccountInput{Name: "robot"})
	_, checks["CreateServiceAccountKey"] = f.svc.CreateServiceAccountKey(keyCtx, "", "svc_x", password, in)
	for name, err := range checks {
		if !errors.Is(err, authdomain.ErrSessionRequired) {
			t.Errorf("%s with an API key error = %v, want ErrSessionRequired", name, err)
		}
	}
	f.keyCtx(t, created.Key) // still usable: nothing above changed it
}

func TestCreateAPIKeyChecksTheUserAndInput(t *testing.T) {
	f := newKeyFixture(t, func(c *authusecase.Config) {
		c.APIKeyMaxTTL = config.Static(10 * 365 * 24 * time.Hour)
		c.ReauthLimiter = ratelimit.New(100, 100) // creating keys spends the budget; this test creates many
	})
	ctx, userID := f.user(t, "ada@example.com")
	now := f.clock.now()
	valid := authusecase.APIKeyInput{Name: "ok", ExpiresAt: now.Add(24 * time.Hour)}

	if _, err := f.svc.CreateAPIKey(ctx, "wrong password!", valid); !errors.Is(err, authdomain.ErrInvalidCredentials) {
		t.Errorf("CreateAPIKey(wrong password) error = %v, want ErrInvalidCredentials", err)
	}
	if e, ok := f.audit.find("auth.reauth.failed"); !ok || e.ResourceID != userID {
		t.Errorf("auth.reauth.failed = %+v, %v", e, ok)
	}
	for name, tt := range map[string]struct {
		in   authusecase.APIKeyInput
		want error
	}{
		"no name":                   {authusecase.APIKeyInput{ExpiresAt: now.Add(24 * time.Hour)}, authdomain.ErrInvalidAPIKeyName},
		"name on two lines":         {authusecase.APIKeyInput{Name: "a\nb", ExpiresAt: now.Add(24 * time.Hour)}, authdomain.ErrInvalidAPIKeyName},
		"long name":                 {authusecase.APIKeyInput{Name: strings.Repeat("é", 101), ExpiresAt: now.Add(24 * time.Hour)}, authdomain.ErrInvalidAPIKeyName},
		"no expiry":                 {authusecase.APIKeyInput{Name: "x"}, authdomain.ErrInvalidAPIKeyExpiry},
		"in the past":               {authusecase.APIKeyInput{Name: "x", ExpiresAt: now.Add(-time.Hour)}, authdomain.ErrInvalidAPIKeyExpiry},
		"in less than an hour":      {authusecase.APIKeyInput{Name: "x", ExpiresAt: now.Add(59 * time.Minute)}, authdomain.ErrInvalidAPIKeyExpiry},
		"beyond the one-year limit": {authusecase.APIKeyInput{Name: "x", ExpiresAt: now.Add(366 * 24 * time.Hour)}, authdomain.ErrInvalidAPIKeyExpiry},
	} {
		if _, err := f.svc.CreateAPIKey(ctx, password, tt.in); !errors.Is(err, tt.want) {
			t.Errorf("%s: CreateAPIKey() error = %v, want %v", name, err, tt.want)
		}
	}
	// The setting is clamped to a year, however it is set.
	if _, err := f.svc.CreateAPIKey(ctx, password, authusecase.APIKeyInput{Name: "year", ExpiresAt: now.Add(365 * 24 * time.Hour)}); err != nil {
		t.Errorf("CreateAPIKey(a year) error = %v", err)
	}

	// An unverified account can't create keys.
	if err := f.svc.Register(requestCtx(), "unverified@example.com", password); err != nil {
		t.Fatal(err)
	}
	u, _ := f.svc.UserByEmail(context.Background(), "unverified@example.com")
	unverified := authlib.WithPrincipal(requestCtx(), authlib.Principal{UserID: u.ID, SessionID: "ses_x", SignedInAt: now})
	if _, err := f.svc.CreateAPIKey(unverified, password, valid); !errors.Is(err, authdomain.ErrEmailNotVerified) {
		t.Errorf("CreateAPIKey() for an unverified account error = %v, want ErrEmailNotVerified", err)
	}

	// At most MaxAPIKeys usable keys; a revoked one frees a place.
	var last authusecase.CreatedAPIKey
	for range authdomain.MaxAPIKeys - 1 {
		last = f.newKey(t, ctx, valid)
	}
	if _, err := f.svc.CreateAPIKey(ctx, password, valid); !errors.Is(err, authdomain.ErrAPIKeyLimitReached) {
		t.Errorf("CreateAPIKey() past the limit error = %v, want ErrAPIKeyLimitReached", err)
	}
	if err := f.svc.RevokeAPIKey(ctx, last.APIKey.ID); err != nil {
		t.Fatal(err)
	}
	f.newKey(t, ctx, valid)
}

func TestMaxTTLSetting(t *testing.T) {
	f := newKeyFixture(t, func(c *authusecase.Config) { c.APIKeyMaxTTL = config.Static(7 * 24 * time.Hour) })
	ctx, _ := f.user(t, "ada@example.com")
	if _, err := f.svc.CreateAPIKey(ctx, password, authusecase.APIKeyInput{Name: "x", ExpiresAt: f.clock.now().Add(8 * 24 * time.Hour)}); !errors.Is(err, authdomain.ErrInvalidAPIKeyExpiry) {
		t.Errorf("CreateAPIKey(beyond auth.api_key_max_ttl) error = %v, want ErrInvalidAPIKeyExpiry", err)
	}
	f.newKey(t, ctx, authusecase.APIKeyInput{ExpiresAt: f.clock.now().Add(7 * 24 * time.Hour)})
}

func TestServiceAccountsNeedOpsPermissions(t *testing.T) {
	f := newKeyFixture(t)
	noRole, _ := f.user(t, "ada@example.com", "reporter")
	if _, err := f.svc.ListServiceAccounts(noRole, ""); !errors.Is(err, authdomain.ErrForbidden) {
		t.Errorf("ListServiceAccounts() without permission error = %v, want ErrForbidden", err)
	}
	admin, _ := f.user(t, "admin@example.com", "platform_admin")
	if _, err := f.svc.CreateServiceAccount(admin, "", authusecase.ServiceAccountInput{Name: "robot"}); !errors.Is(err, authdomain.ErrStepUpRequired) {
		t.Errorf("CreateServiceAccount() without 2FA error = %v, want ErrStepUpRequired", err)
	}
	if _, err := f.svc.ListServiceAccounts(requestCtx(), ""); !errors.Is(err, authlib.ErrUnauthenticated) {
		t.Errorf("ListServiceAccounts() signed out error = %v, want ErrUnauthenticated", err)
	}
	// An organisation's service accounts don't exist without organisations.
	op := f.operatorSession(t)
	if _, err := f.svc.ListServiceAccounts(op, "org_x"); !errors.Is(err, authdomain.ErrServiceAccountNotFound) {
		t.Errorf("ListServiceAccounts(org) without OrgAccess error = %v", err)
	}

	for name, in := range map[string]authusecase.ServiceAccountInput{
		"no name":          {},
		"long description": {Name: "x", Description: strings.Repeat("d", 501)},
		"name with NUL":    {Name: "x\x00y"},
	} {
		if _, err := f.svc.CreateServiceAccount(op, "", in); !errors.Is(err, authdomain.ErrInvalidServiceAccount) {
			t.Errorf("%s: CreateServiceAccount() error = %v, want ErrInvalidServiceAccount", name, err)
		}
	}
	a, err := f.svc.CreateServiceAccount(op, "", authusecase.ServiceAccountInput{Name: "robot", Description: "Syncs", Roles: []string{"reader", "reporter", "reader"}})
	if err != nil || !slices.Equal(a.Roles, []string{"reader", "reporter"}) || a.OrgID != "" || !strings.HasPrefix(a.ID, "svc_") {
		t.Fatalf("CreateServiceAccount() = %+v, %v", a, err)
	}
	if e, ok := f.audit.find("auth.service_account.created"); !ok || e.ActorID == "" || e.ResourceID != a.ID {
		t.Errorf("auth.service_account.created = %+v", e)
	}
	key, err := f.svc.CreateServiceAccountKey(op, "", a.ID, "", authusecase.APIKeyInput{Name: "k", Scopes: []string{"reports.report.read"}, ExpiresAt: f.clock.now().Add(2 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.svc.AuthenticateAPIKey(requestCtx(), key.Key)
	if err != nil || p.ServiceAccountID != a.ID || p.UserID != "" || p.OrgID != "" || !slices.Equal(p.Permissions, []string{"reports.report.read"}) {
		t.Fatalf("AuthenticateAPIKey() = %+v, %v", p, err)
	}
	if got := actor.FromOrAnonymous(authlib.WithPrincipal(context.Background(), p)); got.Kind != actor.KindService || got.ID != a.ID {
		t.Errorf("service account's actor = %+v", got)
	}
	// A service account's key has no account to manage.
	if _, err := f.svc.Me(authlib.WithPrincipal(requestCtx(), p)); !errors.Is(err, authdomain.ErrSessionRequired) {
		t.Errorf("Me() with a service account's key error = %v, want ErrSessionRequired", err)
	}
	if err := f.svc.RevokeServiceAccountKey(op, "", "svc_other", key.APIKey.ID); !errors.Is(err, authdomain.ErrServiceAccountNotFound) {
		t.Errorf("RevokeServiceAccountKey(another service account) error = %v", err)
	}
	if err := f.svc.RevokeServiceAccountKey(op, "", a.ID, "key_other"); !errors.Is(err, authdomain.ErrAPIKeyNotFound) {
		t.Errorf("RevokeServiceAccountKey(unknown key) error = %v", err)
	}
	if keys, err := f.svc.ListServiceAccountKeys(op, "", a.ID); err != nil || len(keys) != 1 {
		t.Errorf("ListServiceAccountKeys() = %d, %v", len(keys), err)
	}
	if err := f.svc.RevokeServiceAccountKey(op, "", a.ID, key.APIKey.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.AuthenticateAPIKey(requestCtx(), key.Key); !errors.Is(err, authlib.ErrUnauthenticated) {
		t.Errorf("AuthenticateAPIKey() after revoking error = %v", err)
	}
}

// fakeOrgs is an OrgAccess: user IDs are members of orgs with roles.
type fakeOrgs struct {
	catalog *authlib.Catalog
	members map[string]string // "org/user" -> role
}

func (o fakeOrgs) AuthorizeServiceAccounts(ctx context.Context, orgID string) (context.Context, string, error) {
	a := actor.FromOrAnonymous(ctx)
	role, ok := o.members[orgID+"/"+a.ID]
	if !ok {
		return ctx, "", authdomain.ErrServiceAccountNotFound
	}
	if role == "member" {
		return ctx, "", authdomain.ErrForbidden
	}
	a.OrgID = orgID
	return actor.With(ctx, a), role, nil
}

func (o fakeOrgs) CanAssign(callerRole, role string) bool {
	if role == "owner" {
		return false
	}
	mine := o.catalog.Permissions(callerRole)
	for _, p := range o.catalog.Permissions(role) {
		if !slices.Contains(mine, p) {
			return false
		}
	}
	return true
}

func (o fakeOrgs) Catalog() *authlib.Catalog { return o.catalog }

func orgCatalog() *authlib.Catalog {
	c := authlib.NewCatalog()
	c.Permission("projects.project.read", "Read projects")
	c.Permission("projects.project.write", "Write projects")
	c.Permission("orgs.org.delete", "Delete the organisation")
	c.Permission("billing.invoice.read", "Read invoices")
	c.Role("owner", "Everything", "projects.project.read", "projects.project.write", "orgs.org.delete", "billing.invoice.read")
	c.Role("admin", "Manages", "projects.project.read", "projects.project.write")
	c.Role("member", "Works", "projects.project.read")
	c.Role("billing", "Reads invoices with 2FA", "billing.invoice.read")
	c.RequireMFA("billing")
	c.Freeze()
	return c
}

func TestOrganisationServiceAccounts(t *testing.T) {
	orgs := fakeOrgs{catalog: orgCatalog(), members: map[string]string{}}
	f := newKeyFixture(t, func(c *authusecase.Config) { c.Orgs = orgs })
	owner, ownerID := f.user(t, "owner@example.com")
	admin, adminID := f.user(t, "admin@example.com")
	outsider, outsiderID := f.user(t, "outsider@example.com")
	orgs.members["org_a/"+ownerID], orgs.members["org_a/"+adminID], orgs.members["org_b/"+outsiderID] = "owner", "admin", "owner"

	for _, role := range []string{"owner", "billing", "undeclared", ""} {
		if _, err := f.svc.CreateServiceAccount(owner, "org_a", authusecase.ServiceAccountInput{Name: "robot", Roles: []string{role}}); !errors.Is(err, authdomain.ErrInvalidServiceAccountRole) {
			t.Errorf("CreateServiceAccount(role %q) error = %v, want ErrInvalidServiceAccountRole", role, err)
		}
	}
	if _, err := f.svc.CreateServiceAccount(owner, "org_a", authusecase.ServiceAccountInput{Name: "robot", Roles: []string{"member", "admin"}}); !errors.Is(err, authdomain.ErrInvalidServiceAccountRole) {
		t.Errorf("CreateServiceAccount(two roles) error = %v, want ErrInvalidServiceAccountRole", err)
	}
	a, err := f.svc.CreateServiceAccount(admin, "org_a", authusecase.ServiceAccountInput{Name: "robot", Roles: []string{"admin"}})
	if err != nil || a.OrgID != "org_a" {
		t.Fatalf("CreateServiceAccount(org) = %+v, %v", a, err)
	}
	if e, ok := f.audit.find("auth.service_account.created"); !ok || e.OrgID != "org_a" {
		t.Errorf("auth.service_account.created = %+v, want org_a", e)
	}
	key, err := f.svc.CreateServiceAccountKey(admin, "org_a", a.ID, password, authusecase.APIKeyInput{Name: "k", ExpiresAt: f.clock.now().Add(2 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CreateServiceAccountKey(admin, "org_a", a.ID, password, authusecase.APIKeyInput{Name: "k", Scopes: []string{"orgs.org.delete"}, ExpiresAt: f.clock.now().Add(2 * time.Hour)}); !errors.Is(err, authdomain.ErrInvalidAPIKeyScopes) {
		t.Errorf("CreateServiceAccountKey(scope above the role) error = %v, want ErrInvalidAPIKeyScopes", err)
	}
	p, err := f.svc.AuthenticateAPIKey(requestCtx(), key.Key)
	if err != nil || p.OrgID != "org_a" || p.ServiceAccountID != a.ID || len(p.Permissions) != 0 {
		t.Errorf("AuthenticateAPIKey() = %+v, %v; want org_a and no platform permissions", p, err)
	}

	// Another organisation's members, and the platform, don't see it.
	if _, err := f.svc.GetServiceAccount(outsider, "org_b", a.ID); !errors.Is(err, authdomain.ErrServiceAccountNotFound) {
		t.Errorf("GetServiceAccount() from org_b error = %v, want ErrServiceAccountNotFound", err)
	}
	if _, err := f.svc.GetServiceAccount(outsider, "org_a", a.ID); !errors.Is(err, authdomain.ErrServiceAccountNotFound) {
		t.Errorf("GetServiceAccount() by a non-member error = %v", err)
	}
	if err := f.svc.RevokeServiceAccountKey(outsider, "org_b", a.ID, key.APIKey.ID); !errors.Is(err, authdomain.ErrServiceAccountNotFound) {
		t.Errorf("RevokeServiceAccountKey() from org_b error = %v", err)
	}
	if list, err := f.svc.ListServiceAccounts(outsider, "org_b"); err != nil || len(list) != 0 {
		t.Errorf("ListServiceAccounts(org_b) = %+v, %v; want none", list, err)
	}
	op := f.operatorSession(t)
	if _, err := f.svc.GetServiceAccount(op, "", a.ID); !errors.Is(err, authdomain.ErrServiceAccountNotFound) {
		t.Errorf("GetServiceAccount() on the platform error = %v, want ErrServiceAccountNotFound", err)
	}

	// An admin can't manage a service account above their role; the owner
	// can.
	orgs.members["org_a/"+adminID] = "member_manager"
	top, err := f.svc.CreateServiceAccount(owner, "org_a", authusecase.ServiceAccountInput{Name: "top", Roles: []string{"admin"}})
	if err != nil {
		t.Fatal(err)
	}
	orgs.members["org_a/"+adminID] = "admin"
	if err := f.svc.DeleteServiceAccount(admin, "org_a", top.ID); err != nil {
		t.Errorf("DeleteServiceAccount() by an admin of an admin service account error = %v", err)
	}
	orgs.members["org_a/"+adminID] = "member"
	if err := f.svc.DeleteServiceAccount(admin, "org_a", a.ID); !errors.Is(err, authdomain.ErrForbidden) {
		t.Errorf("DeleteServiceAccount() by a member error = %v, want the OrgAccess error", err)
	}
}

func TestAPIKeyFailuresAreLimited(t *testing.T) {
	f := newKeyFixture(t, func(c *authusecase.Config) {
		c.APIKeyLimiter = ratelimit.New(3.0/60, 3)
	})
	ctx, _ := f.user(t, "ada@example.com")
	created := f.newKey(t, ctx, authusecase.APIKeyInput{})
	wrong := created.Key[:len(created.Key)-2] + "zz"
	if wrong == created.Key {
		wrong = created.Key[:len(created.Key)-2] + "yy"
	}
	from := func(ip string) context.Context {
		return authlib.WithClientInfo(context.Background(), authlib.ClientInfo{IP: ip})
	}
	for i, key := range []string{"gbk_malformed", wrong, "gbk_" + strings.Repeat("a", 26) + "_" + strings.Repeat("b", 52)} {
		if _, err := f.svc.AuthenticateAPIKey(from("198.51.100.7"), key); !errors.Is(err, authlib.ErrUnauthenticated) {
			t.Fatalf("failure %d error = %v, want ErrUnauthenticated", i+1, err)
		}
	}
	var limited *authlib.RateLimitError
	if _, err := f.svc.AuthenticateAPIKey(from("198.51.100.7"), wrong); !errors.As(err, &limited) || limited.RetryAfter <= 0 {
		t.Fatalf("fourth failure error = %v, want a RateLimitError", err)
	}
	// A valid key from the same network still works, and another network
	// has its own budget; the same /64 shares one.
	if _, err := f.svc.AuthenticateAPIKey(from("198.51.100.7"), created.Key); err != nil {
		t.Errorf("valid key from a limited network error = %v", err)
	}
	if _, err := f.svc.AuthenticateAPIKey(from("198.51.100.8"), wrong); !errors.Is(err, authlib.ErrUnauthenticated) {
		t.Errorf("another network error = %v, want ErrUnauthenticated", err)
	}
	for range 3 {
		_, _ = f.svc.AuthenticateAPIKey(from("2001:db8::1"), wrong)
	}
	if _, err := f.svc.AuthenticateAPIKey(from("2001:db8::ffff"), wrong); !errors.As(err, &limited) {
		t.Errorf("same IPv6 /64 error = %v, want a RateLimitError", err)
	}
	// Revoked and expired keys aren't guesses: they don't use the budget.
	if err := f.svc.RevokeAPIKey(ctx, created.APIKey.ID); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		if _, err := f.svc.AuthenticateAPIKey(from("192.0.2.50"), created.Key); !errors.Is(err, authlib.ErrUnauthenticated) {
			t.Fatalf("revoked key error = %v, want ErrUnauthenticated", err)
		}
	}
}

func TestCleanupRecordsExpiredAPIKeys(t *testing.T) {
	f := newKeyFixture(t)
	ctx, userID := f.user(t, "ada@example.com")
	expiring := f.newKey(t, ctx, authusecase.APIKeyInput{Name: "expiring", ExpiresAt: f.clock.now().Add(2 * time.Hour)})
	revoked := f.newKey(t, ctx, authusecase.APIKeyInput{Name: "revoked"})
	lasting := f.newKey(t, ctx, authusecase.APIKeyInput{Name: "lasting", ExpiresAt: f.clock.now().Add(89 * 24 * time.Hour)})
	if err := f.svc.RevokeAPIKey(ctx, revoked.APIKey.ID); err != nil {
		t.Fatal(err)
	}

	f.clock.advance(3 * time.Hour)
	for run := range 2 {
		res, err := f.svc.Cleanup(context.Background())
		if err != nil || res.ExpiredAPIKeys != int64(1-run) || res.APIKeys != 0 {
			t.Fatalf("Cleanup() run %d = %+v, %v; want the expiry recorded once and nothing deleted", run+1, res, err)
		}
	}
	e, ok := f.audit.find("auth.api_key.expired")
	if !ok || e.ResourceID != expiring.APIKey.ID || e.ActorKind != actor.KindSystem || e.Metadata["owner_id"] != userID {
		t.Errorf("auth.api_key.expired = %+v", e)
	}

	f.clock.advance(authlib.APIKeyRetention)
	res, err := f.svc.Cleanup(context.Background())
	if err != nil || res.APIKeys != 2 {
		t.Errorf("Cleanup() after the retention = %+v, %v; want the expired and revoked keys deleted", res, err)
	}
	if keys, _ := f.svc.ListAPIKeys(ctx); len(keys) != 1 || keys[0].ID != lasting.APIKey.ID {
		t.Errorf("ListAPIKeys() after cleanup = %+v, want only the lasting key", keys)
	}
}

// TestAPIKeysNeverLogged runs every API key path with debug logging and
// checks that no log line or audit event holds a key or its secret.
func TestAPIKeysNeverLogged(t *testing.T) {
	var logs bytes.Buffer
	orgs := fakeOrgs{catalog: orgCatalog(), members: map[string]string{}}
	f := newKeyFixture(t, func(c *authusecase.Config) {
		c.Logger = slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
		c.APIKeyLimiter = ratelimit.New(1.0/60, 1)
		c.Orgs = orgs
	})
	ctx, userID := f.user(t, "ada@example.com", "reporter")
	orgs.members["org_a/"+userID] = "owner"
	var keys []string
	created := f.newKey(t, ctx, authusecase.APIKeyInput{ExpiresAt: f.clock.now().Add(2 * time.Hour)})
	keys = append(keys, created.Key)
	op := f.operatorSession(t)
	a, err := f.svc.CreateServiceAccount(op, "", authusecase.ServiceAccountInput{Name: "robot", Roles: []string{"reporter"}})
	if err != nil {
		t.Fatal(err)
	}
	saKey, err := f.svc.CreateServiceAccountKey(op, "", a.ID, "", authusecase.APIKeyInput{Name: "k", ExpiresAt: f.clock.now().Add(2 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	keys = append(keys, saKey.Key)
	orgAccount, err := f.svc.CreateServiceAccount(ctx, "org_a", authusecase.ServiceAccountInput{Name: "org robot", Roles: []string{"member"}})
	if err != nil {
		t.Fatal(err)
	}
	orgKey, err := f.svc.CreateServiceAccountKey(ctx, "org_a", orgAccount.ID, password, authusecase.APIKeyInput{Name: "k", ExpiresAt: f.clock.now().Add(2 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	keys = append(keys, orgKey.Key)

	for _, k := range keys {
		f.keyCtx(t, k)
		wrong := k[:len(k)-1] + "q"
		if wrong == k {
			wrong = k[:len(k)-1] + "r"
		}
		_, _ = f.svc.AuthenticateAPIKey(requestCtx(), wrong) // failures, then the limit
		_, _ = f.svc.AuthenticateAPIKey(requestCtx(), k[:50])
	}
	_ = f.svc.RevokeAPIKey(ctx, created.APIKey.ID)
	_, _ = f.svc.AuthenticateAPIKey(requestCtx(), created.Key) // revoked: logged with its lookup ID
	_, _ = f.svc.UpdateServiceAccount(op, "", a.ID, authusecase.ServiceAccountPatch{Disabled: ptr(true)})
	_, _ = f.svc.AuthenticateAPIKey(requestCtx(), saKey.Key)
	f.clock.advance(3 * time.Hour)
	_, _ = f.svc.AuthenticateAPIKey(requestCtx(), orgKey.Key) // expired
	if _, err := f.svc.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}

	events, err := json.Marshal(f.audit.events)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "lookup_id") || !strings.Contains(string(events), "auth.api_key.expired") {
		t.Fatalf("the paths weren't exercised: logs %s", logs.String())
	}
	for _, k := range keys {
		secret := k[len(authlib.APIKeyPrefix)+27:]
		for where, text := range map[string]string{"logs": logs.String(), "audit events": string(events)} {
			if strings.Contains(text, secret) || strings.Contains(text, k[:40]+k[41:60]) {
				t.Errorf("%s contain an API key's secret", where)
			}
			if strings.Contains(text, fmt.Sprintf("%x", authlib.HashAPIKey(k))) {
				t.Errorf("%s contain an API key's hash", where)
			}
		}
	}
}

func ptr[T any](v T) *T { return &v }
