// Package integration_test checks the built-in modules together, as a v0.1
// app had them: sign-in (authhttp), the operations API (opshttp), client
// flags (flagshttp) and email events (mailevents) in one app on
// gorbital.New, with real sign-in. Each module's own tests cover it alone;
// these cover the seams between them: what /ops lists about sign-in, the
// permissions both use, and the contract of the whole app against the
// frozen v0.1.0 documents.
package integration_test

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/authhttp"
	"gorbital.dev/gorbital/flagshttp"
	"gorbital.dev/gorbital/gorbitaltest"
	"gorbital.dev/gorbital/mailevents"
	"gorbital.dev/gorbital/opshttp"
	authlib "gorbital.dev/modules/auth"
)

// repo is the repository root, relative to this package.
var repo = filepath.Join("..", "..", "..")

// golden is the v0.1 golden app whose wiring the modules replace.
var golden = filepath.Join(repo, "examples", "full-single", "internal")

const (
	appName  = "acme-api"
	password = "correct horse battery staple"
)

// encryptionKeys is AUTH_ENCRYPTION_KEYS of the test apps.
var encryptionKeys = authlib.NewKeyringKey("integration")

// options are the options of an app with every built-in module, as main.go
// adds them.
func options(auth *authhttp.Authenticator) []gorbital.Option {
	return []gorbital.Option{
		gorbital.WithName(appName),
		gorbital.WithAuth(auth),
		gorbital.WithModules(opshttp.Module(), flagshttp.Module(), mailevents.Module()),
	}
}

// app is an app with every built-in module on its own database.
type app struct {
	*gorbitaltest.App
	auth *authhttp.Authenticator
}

func newApp(t *testing.T) *app {
	t.Helper()
	auth := authhttp.New()
	a := gorbitaltest.NewWithEnv(t, map[string]string{"AUTH_ENCRYPTION_KEYS": encryptionKeys}, options(auth)...)
	return &app{App: a, auth: auth}
}

// command runs one of sign-in's commands, as gorbital.Main does.
func (a *app) command(t *testing.T, name string, args ...string) string {
	t.Helper()
	for _, c := range a.auth.Commands() {
		if c.Name == name {
			var out strings.Builder
			if err := c.Run(context.Background(), a.Config(), args, &out); err != nil {
				t.Fatalf("%s %v: %v", name, args, err)
			}
			return out.String()
		}
	}
	t.Fatalf("no command %q", name)
	return ""
}

var sixDigits = regexp.MustCompile(`\b(\d{6})\b`)

// emailedCode returns the 6-digit code of the newest email queued to to.
func (a *app) emailedCode(t *testing.T, to string) string {
	t.Helper()
	sent := a.Mail(t)
	for i := len(sent) - 1; i >= 0; i-- {
		if len(sent[i].To) > 0 && sent[i].To[0].Email == to {
			if m := sixDigits.FindStringSubmatch(sent[i].Text); m != nil {
				return m[1]
			}
		}
	}
	t.Fatalf("no email with a code to %s in %d queued", to, len(sent))
	return ""
}

// operator is a platform administrator signed in through sign-in's own
// endpoints.
type operator struct {
	id     string
	client *gorbitaltest.Client
	token  string
}

// signUpOperator registers email, verifies it with the emailed code, grants
// it platform_admin with the grant-role command, checks that the role waits
// for a second factor, turns on an authenticator app and signs in with a
// second factor.
func (a *app) signUpOperator(t *testing.T, email string) operator {
	t.Helper()
	anon := a.Client()
	account := map[string]string{"email": email, "password": password}
	anon.Post("/v1/auth/register", account).AssertStatus(t, http.StatusAccepted)
	anon.Post("/v1/auth/verify-email", map[string]string{"email": email, "code": a.emailedCode(t, email)}).AssertStatus(t, http.StatusNoContent)
	if out := a.command(t, "grant-role", email, "platform_admin"); out != "✓ "+email+" now has roles: platform_admin\n" {
		t.Fatalf("grant-role = %q", out)
	}

	login := func() map[string]any {
		t.Helper()
		res := anon.Post("/v1/auth/login", map[string]string{"email": email, "password": password, "transport": "bearer"})
		var body map[string]any
		res.JSON(t, &body)
		return body
	}
	token, _ := login()["token"].(string)
	if token == "" {
		t.Fatal("no session token")
	}
	withPassword := a.Client().WithHeader("Authorization", "Bearer "+token)

	// The ops roles require a second factor (v0.1's RequireMFA): signed in
	// with a password, the role's permissions wait, for opshttp's
	// operations and sign-in's alike.
	withPassword.Get("/ops/settings").AssertProblem(t, http.StatusForbidden, "mfa_required")
	withPassword.Get("/ops/auth/providers").AssertProblem(t, http.StatusForbidden, "mfa_required")
	withPassword.Get("/ops/auth/users").AssertProblem(t, http.StatusForbidden, "mfa_required")

	var setup struct {
		Secret string `json:"secret"`
	}
	withPassword.Post("/v1/auth/mfa/totp", map[string]string{"password": password}).JSON(t, &setup)
	code, err := authlib.TOTPCode(setup.Secret, time.Now())
	if setup.Secret == "" || err != nil {
		t.Fatalf("authenticator app setup: secret %q, %v", setup.Secret, err)
	}
	var confirm struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	withPassword.Post("/v1/auth/mfa/totp/confirm", map[string]string{"code": code}).JSON(t, &confirm)
	if len(confirm.RecoveryCodes) == 0 {
		t.Fatal("no recovery codes")
	}

	// Sign in again with a second factor: the code just used is spent, so a
	// recovery code.
	challenge := login()
	mfa, _ := challenge["mfa"].(map[string]any)
	challengeToken, _ := mfa["challenge_token"].(string)
	if challengeToken == "" {
		t.Fatalf("login with a second factor = %v, want a challenge", challenge)
	}
	res := anon.Post("/v1/auth/login/mfa", map[string]string{"challenge_token": challengeToken, "recovery_code": confirm.RecoveryCodes[0], "transport": "bearer"})
	res.AssertStatus(t, http.StatusOK)
	var session struct {
		Token string `json:"token"`
	}
	res.JSON(t, &session)
	if session.Token == "" {
		t.Fatalf("login/mfa = %s", res.Body)
	}
	client := a.Client().WithHeader("Authorization", "Bearer "+session.Token)
	var me struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	client.Get("/v1/auth/me").JSON(t, &me)
	if me.User.ID == "" {
		t.Fatal("GET /v1/auth/me has no user ID")
	}
	client.Get("/ops/settings").AssertStatus(t, http.StatusOK)
	return operator{id: me.User.ID, client: client, token: session.Token}
}

// TestOpsWithSignIn builds an app with every built-in module, signs an
// operator in for real, and reads what /ops reports about sign-in, as a v0.1
// app's ops tests did.
func TestOpsWithSignIn(t *testing.T) {
	a := newApp(t)
	ops := a.signUpOperator(t, "ops@example.com")

	t.Run("sign-in methods", func(t *testing.T) {
		res := ops.client.Get("/ops/auth/providers")
		res.AssertStatus(t, http.StatusOK)
		var got struct {
			Methods []struct {
				Key     string   `json:"key"`
				Name    string   `json:"name"`
				Enabled bool     `json:"enabled"`
				Detail  string   `json:"detail"`
				Missing []string `json:"missing"`
				Guide   string   `json:"guide"`
			} `json:"methods"`
		}
		res.JSON(t, &got)
		// The same report as the authenticator's, in v0.1's order.
		want := a.auth.SignInMethods(a.Config())
		if len(got.Methods) != len(want) {
			t.Fatalf("GET /ops/auth/providers = %s, want %d methods", res.Body, len(want))
		}
		for i, m := range want {
			g := got.Methods[i]
			if g.Key != m.Key || g.Name != m.Name || g.Enabled != m.Enabled || g.Detail != m.Detail || !slices.Equal(g.Missing, m.Missing) || g.Guide != m.Guide {
				t.Errorf("method %d = %+v, want %+v", i, g, m)
			}
		}
		keys := make([]string, len(got.Methods))
		for i, m := range got.Methods {
			keys[i] = m.Key
		}
		if v01 := v01SignInMethods(t); !slices.Equal(keys, v01) {
			t.Errorf("methods = %v, want v0.1's %v", keys, v01)
		}
		byKey := map[string]bool{}
		for _, m := range got.Methods {
			byKey[m.Key] = m.Enabled
		}
		if !byKey["email_password"] || !byKey["authenticator_app"] || byKey["github"] {
			t.Errorf("enabled methods = %v, want email and password and authenticator apps only", byKey)
		}
		if strings.Contains(string(res.Body), encryptionKeys) || strings.Contains(string(res.Body), strings.TrimPrefix(encryptionKeys, "integration:")) {
			t.Error("GET /ops/auth/providers includes AUTH_ENCRYPTION_KEYS")
		}
	})

	t.Run("rate limits", func(t *testing.T) {
		type limiter struct {
			Name        string `json:"name"`
			Keys        string `json:"keys"`
			Description string `json:"description"`
		}
		var got struct {
			Limiters []limiter `json:"limiters"`
		}
		ops.client.Get("/ops/auth/rate-limits").JSON(t, &got)
		// Every limiter v0.1 listed, with its keys and description. The
		// order differs: gorbital's auth_ip, then each module's, so
		// ops_test_email comes last.
		v01 := v01RateLimiters(t)
		if len(got.Limiters) != len(v01) {
			t.Errorf("GET /ops/auth/rate-limits = %+v, want v0.1's %d limiters", got.Limiters, len(v01))
		}
		for _, want := range v01 {
			if !slices.ContainsFunc(got.Limiters, func(l limiter) bool {
				return l.Name == want[0] && l.Keys == want[1] && l.Description == want[2]
			}) {
				t.Errorf("limiter %v isn't listed as in v0.1: %+v", want, got.Limiters)
			}
		}

		// Wrong passwords spend the address's budget; the operator resets it.
		for range 2 {
			a.Client().Post("/v1/auth/login", map[string]string{"email": "victim@example.com", "password": "not the password"}).AssertProblem(t, http.StatusUnauthorized, "invalid_credentials")
		}
		reset := func(name, key string) map[string]any {
			t.Helper()
			res := ops.client.Post("/ops/auth/rate-limits/reset", map[string]string{"name": name, "key": key})
			res.AssertStatus(t, http.StatusOK)
			var body map[string]any
			res.JSON(t, &body)
			return body
		}
		if r := reset("auth_login_address", "victim@example.com"); r["reset"] != true {
			t.Errorf("reset of a spent budget = %v, want reset", r)
		}
		if r := reset("auth_login_address", "victim@example.com"); r["reset"] != false {
			t.Errorf("second reset = %v, want nothing to reset", r)
		}
		ops.client.Post("/ops/auth/rate-limits/reset", map[string]string{"name": "nope", "key": "x"}).AssertProblem(t, http.StatusNotFound, "rate_limiter_not_found")
		var audit struct {
			Events []struct {
				Action  string `json:"action"`
				ActorID string `json:"actor_id"`
			} `json:"events"`
		}
		ops.client.Get("/ops/audit?action=ops.rate_limit.reset").JSON(t, &audit)
		if len(audit.Events) != 2 || audit.Events[0].ActorID != ops.id {
			t.Errorf("audit events of resets = %+v, want two by %s", audit.Events, ops.id)
		}
	})

	t.Run("retention", func(t *testing.T) {
		var got struct {
			Policies []struct {
				Data       string  `json:"data"`
				Setting    string  `json:"setting"`
				Retention  float64 `json:"retention_seconds"`
				Job        string  `json:"job"`
				EnforcedBy string  `json:"enforced_by"`
				NextRunAt  *string `json:"next_run_at"`
			} `json:"policies"`
		}
		ops.client.Get("/ops/retention").JSON(t, &got)
		var data []string
		for _, p := range got.Policies {
			data = append(data, p.Data)
			switch p.Data {
			case "deleted_accounts":
				if p.Setting != "auth.deleted_account_retention" || p.Job != "auth_cleanup" || p.Retention != authlib.DefaultDeletedAccountRetention.Seconds() || p.NextRunAt == nil {
					t.Errorf("deleted_accounts = %+v", p)
				}
			case "unverified_accounts":
				if p.Setting != "auth.unverified_account_ttl" || p.Job != "auth_cleanup" || p.Retention != authlib.DefaultUnverifiedAccountTTL.Seconds() {
					t.Errorf("unverified_accounts = %+v", p)
				}
			}
		}
		// The rows and the order of v0.1.
		if want := v01Retention(t); !slices.Equal(data, want) {
			t.Errorf("GET /ops/retention data = %v, want v0.1's %v", data, want)
		}
	})

	t.Run("system", func(t *testing.T) {
		var got struct {
			Database struct {
				Status     string `json:"status"`
				Migrations struct {
					Current int64 `json:"current"`
					Latest  int64 `json:"latest"`
					Pending int   `json:"pending"`
				} `json:"migrations"`
			} `json:"database"`
		}
		res := ops.client.Get("/ops/system")
		res.AssertStatus(t, http.StatusOK)
		res.JSON(t, &got)
		// The merged history includes sign-in's migrations, applied.
		m := got.Database.Migrations
		if got.Database.Status != "ok" || m.Pending != 0 || m.Current != m.Latest || m.Latest < 20260918000070 {
			t.Errorf("GET /ops/system database = %+v, want sign-in's migrations applied", got.Database)
		}
	})

	t.Run("users", func(t *testing.T) {
		var list struct {
			Users []struct {
				ID    string   `json:"id"`
				Email string   `json:"email"`
				Roles []string `json:"roles"`
			} `json:"users"`
		}
		res := ops.client.Get("/ops/auth/users?q=ops")
		res.AssertStatus(t, http.StatusOK)
		res.JSON(t, &list)
		if len(list.Users) != 1 || list.Users[0].Email != "ops@example.com" || !slices.Contains(list.Users[0].Roles, "platform_admin") {
			t.Errorf("GET /ops/auth/users?q=ops = %s", res.Body)
		}
		if len(list.Users) == 1 && list.Users[0].ID != ops.id {
			t.Errorf("user ID = %s, want %s", list.Users[0].ID, ops.id)
		}
		// An account without a role can't read accounts.
		reader := a.Client()
		reader.Post("/v1/auth/register", map[string]string{"email": "reader@example.com", "password": password}).AssertStatus(t, http.StatusAccepted)
		reader.Post("/v1/auth/verify-email", map[string]string{"email": "reader@example.com", "code": a.emailedCode(t, "reader@example.com")}).AssertStatus(t, http.StatusNoContent)
		var session struct {
			Token string `json:"token"`
		}
		reader.Post("/v1/auth/login", map[string]string{"email": "reader@example.com", "password": password, "transport": "bearer"}).JSON(t, &session)
		signedIn := a.Client().WithHeader("Authorization", "Bearer "+session.Token)
		signedIn.Get("/ops/auth/users").AssertProblem(t, http.StatusForbidden, "forbidden")
		signedIn.Get("/ops/auth/rate-limits").AssertProblem(t, http.StatusForbidden, "forbidden")
	})

	t.Run("service accounts", func(t *testing.T) {
		res := ops.client.Post("/ops/service-accounts", map[string]string{"name": "Billing sync", "description": "Nightly"})
		res.AssertStatus(t, http.StatusCreated)
		var created struct {
			ID string `json:"id"`
		}
		res.JSON(t, &created)
		var list struct {
			ServiceAccounts []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"service_accounts"`
		}
		listed := ops.client.Get("/ops/service-accounts")
		listed.AssertStatus(t, http.StatusOK)
		listed.JSON(t, &list)
		if created.ID == "" || len(list.ServiceAccounts) != 1 || list.ServiceAccounts[0].ID != created.ID {
			t.Errorf("GET /ops/service-accounts = %s, want %s", listed.Body, created.ID)
		}
	})

	t.Run("roles", func(t *testing.T) {
		out := a.command(t, "roles")
		for _, want := range []string{
			"platform_admin\n  Operates the platform: every /ops permission\n",
			"ops_viewer\n  Reads operational data without changing anything\n",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("roles = %q, want %q", out, want)
			}
		}
		for role, perms := range v01Roles(t) {
			line := regexp.MustCompile(`(?m)^` + role + `\n.*\n  permissions: (.*)$`).FindStringSubmatch(out)
			if line == nil {
				t.Errorf("roles has no %s: %q", role, out)
				continue
			}
			got := strings.Split(line[1], ", ")
			for _, p := range perms {
				if !slices.Contains(got, p) {
					t.Errorf("%s doesn't hold %s: %v", role, p, got)
				}
			}
		}
	})
}

// TestStreamEndsWhenSigningOut: the operations API's live stream checks the
// session again with sign-in's middleware (gorbital.Platform.Authenticate),
// so signing out through sign-in's own endpoint ends it.
func TestStreamEndsWhenSigningOut(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for the stream's check")
	}
	a := newApp(t)
	ops := a.signUpOperator(t, "stream@example.com")
	srv := httptest.NewServer(a.App.App().Handler())
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/ops/observability/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+ops.token)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("stream = %d %s", resp.StatusCode, body)
	}
	events := readEvents(resp.Body)
	if e := nextEvent(t, events); e.name != "retry" {
		t.Fatalf("first line = %+v, want retry", e)
	}

	ops.client.Post("/v1/auth/logout", nil).AssertStatus(t, http.StatusNoContent)
	for {
		e := nextEvent(t, events)
		if e.name == "overview" {
			continue // sent before the check noticed
		}
		if e.name != "end" || e.data != `{"reason":"unauthorized"}` {
			t.Errorf("after signing out: %+v, want end with reason unauthorized", e)
		}
		break
	}
}

// event is one Server-Sent Event.
type event struct{ name, data string }

func readEvents(body io.Reader) <-chan event {
	events := make(chan event, 16)
	go func() {
		defer close(events)
		scanner := bufio.NewScanner(body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
		var e event
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				e.name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				e.data = strings.TrimPrefix(line, "data: ")
			case strings.HasPrefix(line, "retry: "):
				events <- event{name: "retry", data: strings.TrimPrefix(line, "retry: ")}
			case line == "" && e.name != "":
				events <- e
				e = event{}
			}
		}
	}()
	return events
}

func nextEvent(t *testing.T, events <-chan event) event {
	t.Helper()
	select {
	case e, ok := <-events:
		if !ok {
			t.Fatal("the stream ended without an event")
		}
		return e
	case <-time.After(15 * time.Second):
		t.Fatal("no event within 15s")
	}
	return event{}
}

// v01SignInMethods returns the method keys the golden app's providers.go
// reports, in order.
func v01SignInMethods(t *testing.T) []string {
	t.Helper()
	src := read(t, filepath.Join(golden, "app", "providers.go"))
	keys := []string{"email_password"}
	for _, m := range regexp.MustCompile(`method\("([a-z_]+)"`).FindAllStringSubmatch(src, -1) {
		keys = append(keys, m[1])
	}
	if len(keys) < 10 {
		t.Fatalf("found %v in the golden providers.go, want every method", keys)
	}
	return keys
}

// v01RateLimiters returns name, keys and description of each limiter the
// golden app's rate_limits.go lists.
func v01RateLimiters(t *testing.T) [][3]string {
	t.Helper()
	src := read(t, filepath.Join(golden, "app", "rate_limits.go"))
	str := `("(?:[^"\\]|\\.)*")`
	var out [][3]string
	for _, m := range regexp.MustCompile(`\{Name: `+str+`, Keys: `+str+`, Description: `+str+`\}`).FindAllStringSubmatch(src, -1) {
		var l [3]string
		for i := range l {
			s, err := strconv.Unquote(m[i+1])
			if err != nil {
				t.Fatal(err)
			}
			l[i] = s
		}
		out = append(out, l)
	}
	if len(out) != 9 {
		t.Fatalf("found %d limiters in the golden rate_limits.go, want 9", len(out))
	}
	return out
}

// v01Retention returns the data names the golden app's app.go lists for
// /ops/retention, in order.
func v01Retention(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, m := range regexp.MustCompile(`\{data: "([a-z_]+)"`).FindAllStringSubmatch(read(t, filepath.Join(golden, "app", "app.go")), -1) {
		out = append(out, m[1])
	}
	if len(out) != 9 {
		t.Fatalf("found %v in the golden app.go, want 9 policies", out)
	}
	return out
}

// v01Roles returns the ops permissions each ops role holds in the v0.1.0
// surface's catalog, from the golden app's permissions.go.
func v01Roles(t *testing.T) map[string][]string {
	t.Helper()
	src := read(t, filepath.Join(golden, "app", "permissions.go"))
	constants := map[string]string{}
	for qualifier, file := range map[string]string{
		"opsdomain":   filepath.Join(golden, "modules", "ops", "domain", "permissions.go"),
		"authusecase": filepath.Join(golden, "modules", "auth", "usecase", "operators.go"),
	} {
		for _, m := range regexp.MustCompile(`(Perm[A-Za-z]+)\s*=\s*"([a-z_.]+)"`).FindAllStringSubmatch(read(t, file), -1) {
			constants[qualifier+"."+m[1]] = m[2]
		}
	}
	for _, m := range regexp.MustCompile(`(Perm[A-Za-z]+)\s*=\s*"([a-z_.]+)"`).FindAllStringSubmatch(read(t, filepath.Join(golden, "modules", "auth", "usecase", "service_accounts.go")), -1) {
		constants["authusecase."+m[1]] = m[2]
	}
	names := func(list string) []string {
		var out []string
		for _, m := range regexp.MustCompile(`[a-z]+\.Perm[A-Za-z]+`).FindAllString(list, -1) {
			if v, ok := constants[m]; ok {
				out = append(out, v)
			}
		}
		return out
	}
	viewer := regexp.MustCompile(`(?s)c\.Role\(roleOpsViewer, "[^"]*",(.*?)\)\n`).FindStringSubmatch(src)
	if viewer == nil {
		t.Fatal("no ops_viewer role in the golden permissions.go")
	}
	var admin []string
	for _, v := range constants {
		if strings.HasPrefix(v, "ops.") {
			admin = append(admin, v)
		}
	}
	return map[string][]string{"platform_admin": admin, "ops_viewer": names(viewer[1])}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // the repository's own files
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
