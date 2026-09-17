package gorbital

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"gorbital.dev/actor"
	"gorbital.dev/mail"
	"gorbital.dev/modules/auth"
)

// setupAuth is an authenticator with every optional method, recording what
// the app hands it.
type setupAuth struct {
	checkErr error
	setups   []AuthSetup
	seen     *actor.Actor // the actor handlers after the Auth step saw on /ops/
}

func (a *setupAuth) Middleware(*slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler { return next }
}

func (a *setupAuth) CheckConfig(Config) error { return a.checkErr }

func (a *setupAuth) Setup(_ context.Context, s AuthSetup) error {
	a.setups = append(a.setups, s)
	s.Permissions.Freeze()
	s.Handle("GET /.well-known/test.json", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, ok := actor.From(r.Context()); ok {
			a.seen = &got
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	s.MailPreviews(auth.EmailPreview{Name: "auth.hello", Description: "Hello", Category: "auth", Build: func(_ context.Context, to string) (mail.Message, error) {
		return mail.Message{To: []mail.Address{{Email: to}}, Subject: "Hello"}, nil
	}})
	return nil
}

func (a *setupAuth) Module() Module {
	return Module{Name: "signin", Permissions: []Permission{
		{Name: "ops.auth.read", Description: "Read accounts", Roles: []string{"platform_admin", "ops_viewer"}},
		{Name: "signin.custom", Description: "Something", Roles: []string{"support"}},
	}}
}

func (a *setupAuth) Commands() []Command {
	return []Command{{Name: "whoami", Usage: "whoami   print the app", Run: func(_ context.Context, cfg Config, _ []string, w io.Writer) error {
		s := a.setups[len(a.setups)-1]
		fmt.Fprintf(w, "%s %s db=%v roles=%v", s.Name, s.Config.Env, s.Deps.DB != nil, len(s.Permissions.Roles()))
		return nil
	}}}
}

// TestAuthSetupFromMain: before a contributed command, Main checks the
// authenticator's configuration (exit 2 on an error) and hands it the
// catalog with zero Deps.
func TestAuthSetupFromMain(t *testing.T) {
	a := &setupAuth{}
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"whoami"}, env(nil), &stdout, &stderr, []Option{WithName("shelfie"), WithAuth(a)}); code != 0 || stdout.String() != "shelfie development db=false roles=3" {
		t.Fatalf("whoami = %d %q %q", code, stdout.String(), stderr.String())
	}
	s := a.setups[0]
	if roles := s.Permissions.Roles(); !slices.ContainsFunc(roles, func(r auth.Role) bool {
		return r.Name == "platform_admin" && r.Description == "Operates the platform: every /ops permission"
	}) || !slices.ContainsFunc(roles, func(r auth.Role) bool { return r.Name == "support" && r.Description == "Granted by the app's modules" }) {
		t.Errorf("roles = %+v, want v0.1's descriptions for its roles", roles)
	}

	a = &setupAuth{checkErr: errors.New("APPLE_PRIVATE_KEY_FILE: not a key")}
	stdout.Reset()
	stderr.Reset()
	if code := run(context.Background(), []string{"whoami"}, env(nil), &stdout, &stderr, []Option{WithName("shelfie"), WithAuth(a)}); code != 2 ||
		stderr.String() != "shelfie: invalid configuration:\nAPPLE_PRIVATE_KEY_FILE: not a key\n" || len(a.setups) != 0 {
		t.Errorf("whoami with invalid sign-in configuration = %d %q, setups %d; want exit 2 before Setup", code, stderr.String(), len(a.setups))
	}
	if code := run(context.Background(), []string{"serve"}, env(nil), io.Discard, &stderr, []Option{WithName("shelfie"), WithAuth(a)}); code != 2 || !strings.Contains(stderr.String(), "APPLE_PRIVATE_KEY_FILE: not a key") {
		t.Errorf("serve with invalid sign-in configuration = %d %q, want exit 2", code, stderr.String())
	}
}

// TestAuthSetupFromNew: New hands the authenticator its dependencies and
// catalog, serves the handlers it adds behind the stack, previews its
// emails and puts the dev operator after it on /ops/.
func TestAuthSetupFromNew(t *testing.T) {
	const token = "q3Jt0tBq0Xvqf7i5Tq1hYw2m9x8Zr4Kc6Lp2Nd5Vb3E"
	a := &setupAuth{}
	app, err := testApp(t, map[string]string{"DEV_CONSOLE_TOKEN": token}, io.Discard, WithName("shelfie"), WithAuth(a),
		WithModules(Module{Name: "ops", Routes: func(r *Router, _ Deps) {
			Get(r, "/ops/whoami", func(ctx context.Context, _ *struct{}) (*struct{ Body actor.Actor }, error) {
				got, _ := actor.From(ctx)
				return &struct{ Body actor.Actor }{Body: got}, nil
			})
		}}))
	if err != nil {
		t.Fatal(err)
	}
	if len(a.setups) != 1 {
		t.Fatalf("Setup called %d times, want once", len(a.setups))
	}
	s := a.setups[0]
	if s.Name != "shelfie" || s.Deps.DB == nil || s.Deps.Audit == nil || s.Deps.Mailer == nil || s.Deps.RateLimits == nil || s.Deps.Logger == nil || !s.DevConsole || s.Config.DevConsole.Token.IsZero() {
		t.Errorf("AuthSetup = %+v, want the app's name, dependencies and dev console", s)
	}

	srv := httptest.NewServer(app.Handler())
	defer srv.Close()
	get := func(path string) (int, http.Header, string) {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		return res.StatusCode, res.Header, string(body)
	}
	if code, header, body := get("/.well-known/test.json"); code != http.StatusOK || body != `{"ok":true}` || header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("GET a handler the authenticator added = %d %q %v, want it served behind the stack", code, body, header)
	}
	if a.seen != nil {
		t.Errorf("outside /ops/, the dev console token is no actor: %+v", a.seen)
	}
	if code, _, body := get("/ops/whoami"); code != http.StatusOK || !strings.Contains(body, `"ID":"dev-console"`) || !strings.Contains(body, `"ops.auth.read"`) {
		t.Errorf("GET /ops/whoami with the console token = %d %s, want the dev operator with platform_admin's permissions", code, body)
	}
	if code, _, body := get("/_dev/routes"); code != http.StatusOK || !strings.Contains(body, `"path":"/.well-known/test.json"`) {
		t.Errorf("/_dev/routes = %d %s, want the authenticator's handler", code, body)
	}
	if code, _, body := get("/_dev/mail/previews"); code != http.StatusOK || !strings.Contains(body, `"name":"auth.hello"`) || !strings.Contains(body, `"name":"test"`) {
		t.Errorf("/_dev/mail/previews = %d %s, want the authenticator's and the test message", code, body)
	}
}
