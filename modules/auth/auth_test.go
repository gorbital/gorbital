package auth_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gorbital.dev/actor"
	"gorbital.dev/mail"
	"gorbital.dev/modules/auth"
)

func TestHasher(t *testing.T) {
	h, err := auth.NewHasher()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := h.Hash("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Errorf("Hash() = %q", encoded)
	}
	if ok, rehash := h.Verify("correct horse battery", encoded); !ok || rehash {
		t.Errorf("Verify(right) = %t, %t", ok, rehash)
	}
	if ok, _ := h.Verify("wrong horse battery", encoded); ok {
		t.Error("Verify(wrong) = true")
	}
	for _, bad := range []string{"", "$2a$10$bcrypt", "$argon2id$v=19$m=x$salt$key", "$argon2id$v=19$m=0,t=0,p=0$c2FsdA$a2V5"} {
		if ok, _ := h.Verify("x", bad); ok {
			t.Errorf("Verify(%q) = true", bad)
		}
	}
	h.VerifyDummy("anything")
}

func TestValidatePassword(t *testing.T) {
	ctx := context.Background()
	checker := func(_ context.Context, pw string) error {
		if strings.Contains(pw, "password") {
			return errors.New("appears in a list of breached passwords")
		}
		return nil
	}
	if err := auth.ValidatePassword(ctx, "correct horse battery", checker); err != nil {
		t.Errorf("ValidatePassword(good) = %v", err)
	}
	var pwErr *auth.PasswordError
	for pw, reason := range map[string]string{
		"short":                  "at least 12",
		strings.Repeat("x", 129): "at most 128",
		"             ":          "blank",
		"my password is long":    "breached",
	} {
		err := auth.ValidatePassword(ctx, pw, checker)
		if !errors.As(err, &pwErr) || !errors.Is(err, auth.ErrWeakPassword) || !strings.Contains(pwErr.Reason, reason) {
			t.Errorf("ValidatePassword(%q) = %v, want a reason with %q", pw, err, reason)
		}
	}
}

func TestTokensCodesAndEmails(t *testing.T) {
	token, hash := auth.NewToken()
	if len(token) != 43 || string(auth.HashToken(token)) != string(hash) {
		t.Errorf("NewToken() = %q", token)
	}
	if id := auth.NewID("usr"); !strings.HasPrefix(id, "usr_") || len(id) != 30 {
		t.Errorf("NewID() = %q", id)
	}
	code := auth.NewCode()
	if len(code) != 6 || strings.Trim(code, "0123456789") != "" {
		t.Errorf("NewCode() = %q", code)
	}
	stored := auth.HashCode("cod_a", code)
	if !auth.CodeMatches("cod_a", " "+code+" ", stored) || auth.CodeMatches("cod_b", code, stored) || auth.CodeMatches("cod_a", "000000x", stored) {
		t.Error("CodeMatches() doesn't bind codes to their row")
	}

	email, normalized, err := auth.NormalizeEmail("  Ada@Example.com ")
	if err != nil || email != "Ada@Example.com" || normalized != "ada@example.com" {
		t.Errorf("NormalizeEmail() = %q, %q, %v", email, normalized, err)
	}
	if _, normalized, err := auth.NormalizeEmail("jürgen@bücher.example"); err != nil || normalized != "jürgen@bücher.example" {
		t.Errorf("NormalizeEmail(lowercase non-ASCII) = %q, %v", normalized, err)
	}
	// Characters that lowercase to another address's characters would
	// collide with it (security review AUTH-S-3): the Kelvin sign, the
	// Angstrom sign, the Ohm sign and uppercase non-ASCII letters.
	for _, bad := range []string{"", "not an email", "Ada <ada@example.com>", strings.Repeat("a", 250) + "@x.io",
		"\u212Aevin@example.com", "\u212Bsa@example.com", "\u2126mega@example.com", "\u00C4da@example.com", "ada@\u00C4xample.com"} {
		if _, _, err := auth.NormalizeEmail(bad); !errors.Is(err, auth.ErrInvalidEmail) {
			t.Errorf("NormalizeEmail(%q) error = %v", bad, err)
		}
	}

	if c := (auth.ClientInfo{IP: "fe80::1%en0", UserAgent: strings.Repeat("a", 600) + "\x00"}).Clean(); c.IP != "fe80::1" || len(c.UserAgent) != 512 {
		t.Errorf("Clean() = %q, %d", c.IP, len(c.UserAgent))
	}
	if l := auth.SessionIdleLimits; l.Clamp(time.Second) != 5*time.Minute || l.Clamp(time.Hour) != time.Hour || l.Clamp(1000*24*time.Hour) != 90*24*time.Hour {
		t.Error("Limits.Clamp() doesn't bound durations")
	}
}

func TestCatalog(t *testing.T) {
	mustPanic := func(name string, fn func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Errorf("%s did not panic", name)
			}
		}()
		fn()
	}
	c := auth.NewCatalog()
	c.Permission("ops.jobs.run", "Run jobs")
	c.Permission("ops.jobs.read", "Read jobs")
	mustPanic("duplicate permission", func() { c.Permission("ops.jobs.run", "again") })
	mustPanic("bad permission name", func() { c.Permission("Ops Jobs", "") })
	mustPanic("undeclared permission", func() { c.Role("admin", "", "ops.jobs.write") })
	c.Role("admin", "Admin", "ops.jobs.run", "ops.jobs.read", "ops.jobs.run")
	c.Role("viewer", "Viewer", "ops.jobs.read")
	mustPanic("duplicate role", func() { c.Role("admin", "", "ops.jobs.run") })

	if got := c.Permissions("viewer", "admin", "retired"); strings.Join(got, ",") != "ops.jobs.read,ops.jobs.run" {
		t.Errorf("Permissions() = %v", got)
	}
	if len(c.Permissions("retired")) != 0 || !c.HasRole("viewer") || c.HasRole("retired") {
		t.Error("unknown roles must grant nothing")
	}
	if roles := c.Roles(); len(roles) != 2 || len(roles[0].Permissions) != 2 || len(c.AllPermissions()) != 2 {
		t.Errorf("Roles() = %+v", roles)
	}
	c.Freeze()
	mustPanic("declaring after Freeze", func() { c.Permission("late.permission", "") })
}

type fakeAuthenticator struct {
	token string
	err   error
}

func (f fakeAuthenticator) Authenticate(_ context.Context, token string) (auth.Principal, error) {
	switch {
	case f.err != nil:
		return auth.Principal{}, f.err
	case token == f.token:
		return auth.Principal{UserID: "usr_1", SessionID: "ses_1", Permissions: []string{"ops.jobs.read"}}, nil
	default:
		return auth.Principal{}, auth.ErrUnauthenticated
	}
}

func TestMiddleware(t *testing.T) {
	var (
		seen      actor.Actor
		principal auth.Principal
		signedIn  bool
		info      auth.ClientInfo
		status    int
	)
	serve := func(a auth.Authenticator, mutate func(*http.Request)) {
		seen, principal, signedIn, info = actor.Anonymous, auth.Principal{}, false, auth.ClientInfo{}
		h := auth.Middleware(a)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen = actor.FromOrAnonymous(r.Context())
			principal, signedIn = auth.PrincipalFrom(r.Context())
			info = auth.ClientInfoFromContext(r.Context())
		}))
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("User-Agent", "agent/2")
		mutate(req)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		status = rec.Code
	}
	valid := fakeAuthenticator{token: "good-token"}

	serve(valid, func(*http.Request) {})
	if signedIn || info.IP != "192.0.2.1" || info.UserAgent != "agent/2" {
		t.Errorf("anonymous request: signed in %t, client %+v", signedIn, info)
	}
	serve(valid, func(r *http.Request) { r.Header.Set("Authorization", "Bearer good-token") })
	if !signedIn || principal.SessionID != "ses_1" || seen.Kind != actor.KindUser || seen.ID != "usr_1" || !seen.Can("ops.jobs.read") {
		t.Errorf("bearer token: actor %+v, principal %+v", seen, principal)
	}
	serve(valid, func(r *http.Request) {
		r.AddCookie(auth.SessionCookie(auth.DefaultCookieName, "good-token", time.Now().Add(time.Hour)))
	})
	if seen.ID != "usr_1" {
		t.Errorf("session cookie: actor %+v", seen)
	}
	serve(valid, func(r *http.Request) { r.Header.Set("Authorization", "Bearer wrong") })
	if signedIn || seen.Kind != actor.KindAnonymous || status != http.StatusOK {
		t.Errorf("invalid token: signed in %t, status %d; want anonymous", signedIn, status)
	}
	serve(fakeAuthenticator{err: errors.New("database is down")}, func(r *http.Request) { r.Header.Set("Authorization", "Bearer good-token") })
	if status != http.StatusServiceUnavailable {
		t.Errorf("authenticator failure: status %d, want 503", status)
	}

	cookie := auth.SessionCookie("__Host-session", "t", time.Now().Add(time.Hour))
	if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.MaxAge < 3500 {
		t.Errorf("SessionCookie() = %+v", cookie)
	}
	if clear := auth.ClearSessionCookie("__Host-session"); clear.MaxAge != -1 || clear.Value != "" {
		t.Errorf("ClearSessionCookie() = %+v", clear)
	}
}

func TestMailEmails(t *testing.T) {
	var sent []mail.Message
	emails := auth.NewMailEmails(mail.SenderFunc(func(_ context.Context, m mail.Message) error {
		sent = append(sent, m)
		return nil
	}), "Acme <Shop>")
	ctx := context.Background()
	_ = emails.SendVerificationCode(ctx, "ada@example.com", "123456", 15*time.Minute)
	_ = emails.SendPasswordResetCode(ctx, "ada@example.com", "654321", 2*time.Hour)
	_ = emails.SendAccountExists(ctx, "ada@example.com")
	_ = emails.SendPasswordChanged(ctx, "ada@example.com")
	if len(sent) != 4 {
		t.Fatalf("sent %d emails, want 4", len(sent))
	}
	if m := sent[0]; !strings.Contains(m.Text, "123456") || !strings.Contains(m.Text, "15 minutes") || m.To[0].Email != "ada@example.com" ||
		!strings.Contains(m.HTML, "Acme &lt;Shop&gt;") || m.Tags["category"] != "auth_verification" {
		t.Errorf("verification email = %+v", m)
	}
	if m := sent[1]; !strings.Contains(m.Text, "654321") || !strings.Contains(m.Text, "2 hours") {
		t.Errorf("reset email = %+v", m)
	}
}
