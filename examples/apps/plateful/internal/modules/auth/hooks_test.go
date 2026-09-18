package authhttp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/actor"
	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"
	authlib "gorbital.dev/modules/auth"
	"gorbital.dev/modules/auth/social/socialtest"
	"gorbital.dev/modules/postgres/pgtest"

	authdomain "example.com/plateful/internal/modules/auth/domain"
)

// hookApp is an app built with sign-in's options, the modules extra returns
// for its authenticator, and a table hooks write to.
type hookApp struct {
	*testApp
	pool *pgxpool.Pool
}

// newHookApp builds an app like newApp, with opts and the modules of extra,
// and creates hook_rows (user_id, note) for the hooks to write. extra's
// functions receive the authenticator before the app is built.
func newHookApp(t *testing.T, env map[string]string, opts []Option, extra ...func(*Authenticator) gorbital.Module) *hookApp {
	t.Helper()
	full := map[string]string{"DATABASE_URL": pgtest.NewDatabase(t)}
	maps.Copy(full, env)
	cfg := testConfig(t, full)
	ctx := context.Background()
	auth := New(opts...)
	if err := auth.CheckConfig(cfg); err != nil {
		t.Fatalf("CheckConfig() error = %v", err)
	}
	gopts := appOptions(auth)
	for _, m := range extra {
		if module := m(auth); module.Name != "" {
			gopts = append(gopts, gorbital.WithModules(module))
		}
	}
	if err := gorbital.Migrate(ctx, cfg, io.Discard, gopts...); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL.Reveal())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `CREATE TABLE hook_rows (user_id text NOT NULL, note text NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	a, err := gorbital.New(ctx, cfg, gopts...)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := a.Close(context.WithoutCancel(ctx)); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return &hookApp{testApp: &testApp{App: a, auth: auth, cfg: cfg, url: cfg.DatabaseURL.Reveal()}, pool: pool}
}

// notes returns hook_rows' notes for a user, or every note for "".
func (a *hookApp) notes(t *testing.T, userID string) []string {
	t.Helper()
	rows, err := a.pool.Query(context.Background(), `SELECT note FROM hook_rows WHERE $1 = '' OR user_id = $1 ORDER BY note`, userID)
	if err != nil {
		t.Fatal(err)
	}
	notes, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return notes
}

// countUsers returns how many accounts have an address.
func (a *hookApp) countUsers(t *testing.T, email string) int {
	t.Helper()
	var n int
	if err := a.pool.QueryRow(context.Background(), `SELECT count(*) FROM auth_users WHERE email_normalized = lower($1)`, email).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// verifiedUser creates a verified account with the test password.
func (a *hookApp) verifiedUser(t *testing.T, email string) string {
	t.Helper()
	u, err := a.Auth().CreateUser(actor.With(context.Background(), actor.System("test")), email, testPassword, true)
	if err != nil {
		t.Fatalf("CreateUser(%s) error = %v", email, err)
	}
	return u.ID
}

func login(email, password string) string {
	return fmt.Sprintf(`{"email":%q,"password":%q,"transport":"bearer"}`, email, password)
}

// withoutRequestID drops the request ID, which differs between responses.
func withoutRequestID(r response) string {
	delete(r.json, "request_id")
	return fmt.Sprint(r.code, r.json)
}

// hookCalls records what a hook received.
type hookCalls[T any] struct {
	mu    sync.Mutex
	calls []T
}

func (c *hookCalls[T]) add(v T) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, v)
}

func (c *hookCalls[T]) all() []T {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.calls)
}

var errSuspended = Refuse("test_suspended", "this account is suspended")

// TestBeforeLogin: the hook runs once every factor is verified, can refuse
// with its own code or fail closed, and never runs, so never shows, for
// unknown addresses, wrong passwords, unverified addresses or banned
// accounts.
func TestBeforeLogin(t *testing.T) {
	var attempts hookCalls[LoginAttempt]
	refuse := map[string]error{}
	var mu sync.Mutex
	a := newHookApp(t, nil, []Option{BeforeLogin(func(ctx context.Context, tx pgx.Tx, at LoginAttempt) error {
		attempts.add(at)
		if _, err := tx.Exec(ctx, `INSERT INTO hook_rows VALUES ($1, 'before_login')`, at.User.ID); err != nil {
			return err
		}
		mu.Lock()
		defer mu.Unlock()
		return refuse[at.User.Email]
	})})
	h := a.Handler()

	ada := a.verifiedUser(t, "ada@example.com")
	r := do(t, h, "POST", "/v1/auth/login", login("ada@example.com", testPassword))
	if r.code != http.StatusOK {
		t.Fatalf("POST /v1/auth/login = %d %s", r.code, r.body)
	}
	got := attempts.all()
	if len(got) != 1 || got[0].User.ID != ada || got[0].Method != "password" || got[0].SecondFactor != "" || got[0].User.Email != "ada@example.com" || !got[0].User.EmailVerified {
		t.Errorf("BeforeLogin received %+v", got)
	}
	if notes := a.notes(t, ada); !slices.Equal(notes, []string{"before_login"}) {
		t.Errorf("the hook's write in the sign-in's transaction = %v, want committed", notes)
	}

	// A refusal: 403 with the app's code, no session, the write rolled back,
	// and an audit event.
	mu.Lock()
	refuse["ada@example.com"] = errSuspended
	mu.Unlock()
	r = do(t, h, "POST", "/v1/auth/login", login("ada@example.com", testPassword))
	if r.code != http.StatusForbidden || r.json["code"] != "test_suspended" || r.json["detail"] != "this account is suspended" || r.json["token"] != nil {
		t.Errorf("refused sign-in = %d %s", r.code, r.body)
	}
	if notes := a.notes(t, ada); len(notes) != 1 {
		t.Errorf("notes after a refusal = %v, want the refused sign-in's write rolled back", notes)
	}
	if events := auditEvents(t, a.url, "auth.login.failed"); len(events) == 0 || events[0].Metadata["reason"] != "refused" || events[0].Metadata["code"] != "test_suspended" || events[0].ResourceID != ada {
		t.Errorf("audit after a refusal = %+v", events)
	}

	// Any other error fails closed.
	mu.Lock()
	refuse["ada@example.com"] = errors.New("billing service down")
	mu.Unlock()
	if r := do(t, h, "POST", "/v1/auth/login", login("ada@example.com", testPassword)); r.code != http.StatusInternalServerError || strings.Contains(r.body, "billing") {
		t.Errorf("sign-in with a failing hook = %d %s, want 500 without the cause", r.code, r.body)
	}
	// A Refusal built without Refuse can't use a built-in code.
	mu.Lock()
	refuse["ada@example.com"] = &Refusal{Code: "invalid_credentials", Detail: "spoofed"}
	mu.Unlock()
	if r := do(t, h, "POST", "/v1/auth/login", login("ada@example.com", testPassword)); r.code != http.StatusInternalServerError {
		t.Errorf("sign-in refused with a built-in code = %d %s, want 500", r.code, r.body)
	}

	// No oracle: the hook refuses everyone, yet unknown addresses, wrong
	// passwords and unverified addresses answer as without the hook, and the
	// hook never runs for them.
	mu.Lock()
	for _, e := range []string{"nobody@example.com", "ada@example.com", "pending@example.com", "banned@example.com"} {
		refuse[e] = errSuspended
	}
	mu.Unlock()
	if _, err := a.Auth().CreateUser(actor.With(context.Background(), actor.System("test")), "pending@example.com", testPassword, false); err != nil {
		t.Fatal(err)
	}
	banned := a.verifiedUser(t, "banned@example.com")
	if _, err := a.pool.Exec(context.Background(), `UPDATE auth_users SET banned_at = now() WHERE id = $1`, banned); err != nil {
		t.Fatal(err)
	}
	before := len(attempts.all())
	plain := newApp(t, nil)
	if _, err := plain.Auth().CreateUser(actor.With(context.Background(), actor.System("test")), "ada@example.com", testPassword, true); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, body string }{
		{"unknown address", login("nobody@example.com", testPassword)},
		{"wrong password", login("ada@example.com", "a wrong password here")},
	} {
		withHook := do(t, h, "POST", "/v1/auth/login", tc.body)
		without := do(t, plain.Handler(), "POST", "/v1/auth/login", tc.body)
		if withoutRequestID(withHook) != withoutRequestID(without) || withHook.code != http.StatusUnauthorized {
			t.Errorf("%s: with the hook %d %s, without %d %s", tc.name, withHook.code, withHook.body, without.code, without.body)
		}
	}
	if r := do(t, h, "POST", "/v1/auth/login", login("pending@example.com", testPassword)); r.code != http.StatusForbidden || r.json["code"] != "email_not_verified" {
		t.Errorf("unverified address = %d %s", r.code, r.body)
	}
	if r := do(t, h, "POST", "/v1/auth/login", login("banned@example.com", testPassword)); r.code != http.StatusForbidden || r.json["code"] != "account_banned" {
		t.Errorf("banned account = %d %s, want account_banned before the hook", r.code, r.body)
	}
	if after := len(attempts.all()); after != before {
		t.Errorf("BeforeLogin ran %d times for failed sign-ins, want 0", after-before)
	}
}

// TestHooksCantSkipTheSecondFactor: an account with two-factor
// authentication gets its challenge whatever the hooks return, and
// BeforeLogin runs only after the second factor, knowing both.
func TestHooksCantSkipTheSecondFactor(t *testing.T) {
	var attempts hookCalls[LoginAttempt]
	var events hookCalls[LoginEvent]
	a := newHookApp(t, nil, []Option{
		BeforeLogin(func(_ context.Context, _ pgx.Tx, at LoginAttempt) error { attempts.add(at); return nil }),
		AfterLogin(func(_ context.Context, e LoginEvent) error { events.add(e); return nil }),
	})
	h := a.Handler()
	id := a.verifiedUser(t, "mfa@example.com")
	enrollment, _, err := a.Auth().EnrollTOTP(actor.With(context.Background(), actor.System("test")), id)
	if err != nil {
		t.Fatal(err)
	}
	r := do(t, h, "POST", "/v1/auth/login", login("mfa@example.com", testPassword))
	if r.code != http.StatusAccepted || r.json["token"] != nil {
		t.Fatalf("POST /v1/auth/login with 2FA = %d %s, want 202", r.code, r.body)
	}
	if len(attempts.all()) != 0 || len(events.all()) != 0 {
		t.Errorf("hooks ran before the second factor: %+v %+v", attempts.all(), events.all())
	}
	token := signInWithTOTP(t, h, "mfa@example.com", testPassword, enrollment.Secret)
	got := attempts.all()
	if len(got) != 1 || got[0].Method != "password" || got[0].SecondFactor != "totp" {
		t.Errorf("BeforeLogin after the second factor = %+v", got)
	}
	ev := events.all()
	if len(ev) != 1 || ev[0].SessionID == "" || ev[0].SecondFactor != "totp" || strings.Contains(fmt.Sprint(ev[0]), token) {
		t.Errorf("AfterLogin = %+v", ev)
	}
}

// TestAfterLogin: errors and panics are logged, never returned, and a slow
// hook delays the response at most its deadline.
func TestAfterLogin(t *testing.T) {
	old := afterLoginTimeout
	afterLoginTimeout = 100 * time.Millisecond
	t.Cleanup(func() { afterLoginTimeout = old })
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	var events hookCalls[LoginEvent]
	a := newHookApp(t, nil, []Option{
		AfterLogin(func(_ context.Context, e LoginEvent) error { events.add(e); return errors.New("analytics down") }),
		AfterLogin(func(_ context.Context, e LoginEvent) error {
			if e.User.Email == "panic@example.com" {
				panic("hook bug")
			}
			return nil
		}),
		AfterLogin(func(ctx context.Context, e LoginEvent) error {
			if e.User.Email == "slow@example.com" {
				<-release // ignores its context
			}
			return nil
		}),
	})
	h := a.Handler()
	for _, email := range []string{"ada@example.com", "panic@example.com", "slow@example.com"} {
		id := a.verifiedUser(t, email)
		start := time.Now()
		r := do(t, h, "POST", "/v1/auth/login", login(email, testPassword))
		if r.code != http.StatusOK {
			t.Errorf("%s: POST /v1/auth/login = %d %s", email, r.code, r.body)
		}
		if elapsed := time.Since(start); elapsed > 3*time.Second {
			t.Errorf("%s: sign-in took %s, want the hooks bounded by their deadline", email, elapsed)
		}
		if ev := events.all(); len(ev) == 0 || ev[len(ev)-1].User.ID != id || ev[len(ev)-1].Method != "password" || ev[len(ev)-1].SessionID == "" {
			t.Errorf("%s: AfterLogin = %+v", email, ev)
		}
	}
}

// TestOnRegister: the hook runs in the transaction of every way an account
// is created, and its error rolls the account back without changing
// registration's answer.
func TestOnRegister(t *testing.T) {
	srv := socialtest.New(t)
	var accounts hookCalls[NewAccount]
	a := newHookApp(t, socialEnv(t), []Option{OnRegister(func(ctx context.Context, tx pgx.Tx, acct NewAccount) error {
		accounts.add(acct)
		if _, err := tx.Exec(ctx, `INSERT INTO hook_rows VALUES ($1, $2)`, acct.User.ID, "created by "+acct.Method); err != nil {
			return err
		}
		switch {
		case strings.HasPrefix(acct.User.Email, "refused"):
			return Refuse("test_domain_closed", "sign-ups from this address are closed")
		case strings.HasPrefix(acct.User.Email, "broken"):
			return errors.New("profile service down")
		}
		return nil
	})}, func(auth *Authenticator) gorbital.Module {
		auth.endpoints.Google = srv.Endpoints()
		return gorbital.Module{}
	})
	h := a.Handler()

	// Email registration.
	if r := do(t, h, "POST", "/v1/auth/register", `{"email":"ada@example.com","password":"correct horse battery"}`); r.code != http.StatusAccepted {
		t.Fatalf("POST /v1/auth/register = %d %s", r.code, r.body)
	}
	got := accounts.all()
	if len(got) != 1 || got[0].Method != "password" || got[0].User.Email != "ada@example.com" || got[0].User.EmailVerified || got[0].User.ID == "" {
		t.Fatalf("OnRegister = %+v", got)
	}
	if notes := a.notes(t, got[0].User.ID); !slices.Equal(notes, []string{"created by password"}) {
		t.Errorf("hook_rows = %v", notes)
	}

	// A failing hook rolls back, and registration answers as for an address
	// that already has an account.
	a.verifiedUser(t, "exists@example.com")
	exists := do(t, h, "POST", "/v1/auth/register", `{"email":"exists@example.com","password":"correct horse battery"}`)
	for _, email := range []string{"refused@example.com", "broken@example.com"} {
		r := do(t, h, "POST", "/v1/auth/register", fmt.Sprintf(`{"email":%q,"password":"correct horse battery"}`, email))
		if withoutRequestID(r) != withoutRequestID(exists) {
			t.Errorf("%s: POST /v1/auth/register = %d %s, want the answer for an existing address %s", email, r.code, r.body, exists.body)
		}
		if n := a.countUsers(t, email); n != 0 {
			t.Errorf("%s: %d accounts after the hook failed, want rolled back", email, n)
		}
	}
	if notes := a.notes(t, ""); len(notes) != 2 { // ada and exists@, created by operator
		t.Errorf("hook_rows = %v, want the failed registrations' rows rolled back", notes)
	}
	if events := auditEvents(t, a.url, "auth.user.registered"); len(events) != 1 {
		t.Errorf("auth.user.registered events = %+v, want ada's only", events)
	}

	// Operators.
	ops := actor.With(context.Background(), actor.System("test"))
	if _, err := a.Auth().CreateUser(ops, "refused-op@example.com", testPassword, true); !errors.As(err, new(*authdomain.Refusal)) {
		t.Errorf("CreateUser refused = %v, want the refusal", err)
	}
	if n := a.countUsers(t, "refused-op@example.com"); n != 0 {
		t.Errorf("an operator's refused account exists")
	}
	if got := accounts.all(); got[len(got)-1].Method != "operator" {
		t.Errorf("OnRegister for an operator = %+v", got[len(got)-1])
	}

	// A first Google sign-in: the provider's name, and a refusal's code.
	google := func(subject, email, name string) response {
		nonce := do(t, h, "POST", "/v1/auth/google/nonce", "")
		token := srv.IDToken(socialtest.Claims{Subject: subject, Audience: "ios-client", Email: email, EmailVerified: true, Nonce: fmt.Sprint(nonce.json["nonce"]), Extra: map[string]any{"name": name}})
		return do(t, h, "POST", "/v1/auth/google/token", fmt.Sprintf(`{"id_token":%q,"nonce":%q,"transport":"bearer"}`, token, nonce.json["nonce"]))
	}
	if r := google("g-bob", "bob@gmail.com", "Bob Smith"); r.code != http.StatusOK {
		t.Fatalf("Google sign-in = %d %s", r.code, r.body)
	}
	if got := accounts.all(); got[len(got)-1].Method != "google" || got[len(got)-1].Name != "Bob Smith" || !got[len(got)-1].User.EmailVerified {
		t.Errorf("OnRegister for Google = %+v", got[len(got)-1])
	}
	r := google("g-refused", "refused@gmail.com", "")
	if r.code != http.StatusForbidden || r.json["code"] != "test_domain_closed" {
		t.Errorf("refused Google sign-up = %d %s", r.code, r.body)
	}
	if n := a.countUsers(t, "refused@gmail.com"); n != 0 {
		t.Errorf("a refused Google account exists")
	}
	if r := google("g-broken", "broken@gmail.com", ""); r.code != http.StatusInternalServerError || a.countUsers(t, "broken@gmail.com") != 0 {
		t.Errorf("Google sign-up with a failing hook = %d %s", r.code, r.body)
	}
	// A second sign-in isn't a registration.
	calls := len(accounts.all())
	if r := google("g-bob", "bob@gmail.com", "Bob Smith"); r.code != http.StatusOK || len(accounts.all()) != calls {
		t.Errorf("second Google sign-in = %d, hook calls %d → %d", r.code, calls, len(accounts.all()))
	}
}

// TestSignInCustomMethod: a module's method gets login's results: a
// session in either transport, the challenge for two-factor accounts, bans,
// unverified addresses, limits, hooks and audit.
func TestSignInCustomMethod(t *testing.T) {
	var attempts hookCalls[LoginAttempt]
	a := newHookApp(t, nil, []Option{BeforeLogin(func(_ context.Context, _ pgx.Tx, at LoginAttempt) error {
		attempts.add(at)
		return nil
	})}, testSignInModule)
	h := a.Handler()
	signIn := func(userID, transport string) response {
		return do(t, h, "POST", "/v1/test/sign-in", fmt.Sprintf(`{"user_id":%q,"transport":%q}`, userID, transport))
	}

	ada := a.verifiedUser(t, "ada@example.com")
	r := signIn(ada, "bearer")
	token, _ := r.json["token"].(string)
	if user, _ := r.json["user"].(map[string]any); r.code != http.StatusOK || token == "" || user["id"] != ada {
		t.Fatalf("SignIn bearer = %d %s", r.code, r.body)
	}
	if me := do(t, h, "GET", "/v1/auth/me", "", "Authorization", "Bearer "+token); me.code != http.StatusOK {
		t.Errorf("GET /v1/auth/me with the custom method's token = %d %s", me.code, me.body)
	}
	r = signIn(ada, "")
	if r.code != http.StatusOK || r.json["token"] != nil || cookieValue(r, "__Host-session") == "" {
		t.Errorf("SignIn cookie = %d %v %s", r.code, r.header.Values("Set-Cookie"), r.body)
	}
	if got := attempts.all(); len(got) != 2 || got[0].Method != "phone_code" {
		t.Errorf("BeforeLogin = %+v", got)
	}
	if events := auditEvents(t, a.url, "auth.login.succeeded"); len(events) == 0 || events[0].Metadata["method"] != "phone_code" {
		t.Errorf("auth.login.succeeded = %+v", events)
	}

	// Two-factor authentication: 202, then POST /v1/auth/login/mfa, which
	// knows how the sign-in started.
	mfa := a.verifiedUser(t, "mfa@example.com")
	enrollment, _, err := a.Auth().EnrollTOTP(actor.With(context.Background(), actor.System("test")), mfa)
	if err != nil {
		t.Fatal(err)
	}
	r = signIn(mfa, "bearer")
	challenge, _ := r.json["mfa"].(map[string]any)
	challengeToken, _ := challenge["challenge_token"].(string)
	if r.code != http.StatusAccepted || challengeToken == "" || r.json["token"] != nil {
		t.Fatalf("SignIn with 2FA = %d %s", r.code, r.body)
	}
	code, _ := authlib.TOTPCode(enrollment.Secret, time.Now())
	// The method is bound to the challenge: changing it makes it unknown.
	forged := strings.Replace(challengeToken, ".phone_code", ".password_x", 1)
	if r := do(t, h, "POST", "/v1/auth/login/mfa", fmt.Sprintf(`{"challenge_token":%q,"code":%q}`, forged, code)); r.code != http.StatusUnauthorized || r.json["code"] != "invalid_mfa" {
		t.Errorf("challenge with another method = %d %s", r.code, r.body)
	}
	if r := do(t, h, "POST", "/v1/auth/login/mfa", fmt.Sprintf(`{"challenge_token":%q,"code":%q}`, strings.Split(challengeToken, ".")[0], code)); r.code != http.StatusUnauthorized {
		t.Errorf("challenge without its method = %d %s", r.code, r.body)
	}
	r = do(t, h, "POST", "/v1/auth/login/mfa", fmt.Sprintf(`{"challenge_token":%q,"code":%q,"transport":"bearer"}`, challengeToken, code))
	if r.code != http.StatusOK || r.json["token"] == nil {
		t.Fatalf("POST /v1/auth/login/mfa = %d %s", r.code, r.body)
	}
	if got := attempts.all(); got[len(got)-1].Method != "phone_code" || got[len(got)-1].SecondFactor != "totp" {
		t.Errorf("BeforeLogin after the second factor = %+v", got[len(got)-1])
	}
	if events := auditEvents(t, a.url, "auth.login.succeeded"); events[0].Metadata["method"] != "phone_code" || events[0].Metadata["mfa_method"] != "totp" {
		t.Errorf("auth.login.succeeded after the second factor = %+v", events[0])
	}

	// Refusals of login.
	pending, err := a.Auth().CreateUser(actor.With(context.Background(), actor.System("test")), "pending@example.com", testPassword, false)
	if err != nil {
		t.Fatal(err)
	}
	if r := signIn(pending.ID, "bearer"); r.code != http.StatusForbidden || r.json["code"] != "email_not_verified" {
		t.Errorf("SignIn unverified = %d %s", r.code, r.body)
	}
	banned := a.verifiedUser(t, "banned@example.com")
	if _, err := a.pool.Exec(context.Background(), `UPDATE auth_users SET banned_at = now() WHERE id = $1`, banned); err != nil {
		t.Fatal(err)
	}
	if r := signIn(banned, "bearer"); r.code != http.StatusForbidden || r.json["code"] != "account_banned" {
		t.Errorf("SignIn banned = %d %s", r.code, r.body)
	}
	if r := signIn("usr_unknown", "bearer"); r.code != http.StatusUnauthorized || r.json["code"] != "invalid_credentials" {
		t.Errorf("SignIn unknown = %d %s", r.code, r.body)
	}
	if r := do(t, h, "POST", "/v1/test/sign-in", fmt.Sprintf(`{"user_id":%q,"method":"password"}`, ada)); r.code != http.StatusInternalServerError {
		t.Errorf("SignIn as password = %d %s, want 500", r.code, r.body)
	}
	limited := false
	for range authlib.DefaultLoginAttempts + 1 {
		if r := signIn(ada, "bearer"); r.code == http.StatusTooManyRequests && r.json["code"] == "too_many_attempts" {
			limited = true
			break
		}
	}
	if !limited {
		t.Errorf("SignIn never hit the login limit")
	}

	u, err := a.auth.User(context.Background(), ada)
	if err != nil || u.Email != "ada@example.com" || !u.EmailVerified || !slices.Contains(u.Roles, "user") && len(u.Roles) != 0 {
		t.Errorf("User() = %+v, %v", u, err)
	}
	if _, err := a.auth.User(context.Background(), "usr_unknown"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("User(unknown) error = %v", err)
	}
}

// testSignInModule stands in for a module whose method verified the user:
// it signs in whoever the request names, which only a test may do.
func testSignInModule(auth *Authenticator) gorbital.Module {
	type input struct {
		Body struct {
			UserID    string `json:"user_id"`
			Transport string `json:"transport,omitempty"`
			Method    string `json:"method,omitempty"`
		}
	}
	return gorbital.Module{
		Name: "test_sign_in",
		Routes: func(r *gorbital.Router, _ gorbital.Deps) {
			gorbital.Post(r, "/v1/test/sign-in", func(ctx context.Context, in *input) (*SignedIn, error) {
				method := in.Body.Method
				if method == "" {
					method = "phone_code"
				}
				return auth.SignIn(ctx, SignInRequest{UserID: in.Body.UserID, Method: method, Transport: in.Body.Transport})
			}, guard.Public())
		},
	}
}

func TestRefuse(t *testing.T) {
	if err := Refuse("test_code", "detail"); err.Error() != "authhttp: refused: test_code: detail" {
		t.Errorf("Refuse() = %v", err)
	}
	for _, code := range []string{"invalid_credentials", "account_banned", "mfa_required", "validation_failed", "not_found", "registration_closed", "weak_password", "Bad", "x", "has space", ""} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Refuse(%q) didn't panic", code)
				}
			}()
			_ = Refuse(code, "detail")
		}()
	}
}
