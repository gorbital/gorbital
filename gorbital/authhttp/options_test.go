package authhttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"

	"gorbital.dev/actor"
	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"
	"gorbital.dev/mail"
	authlib "gorbital.dev/modules/auth"
	"gorbital.dev/modules/auth/social/socialtest"
	"gorbital.dev/modules/settings"
)

// profileFields are the registration fields of TestRegisterFields.
type profileFields struct {
	DisplayName string `json:"display_name" minLength:"1" maxLength:"50"`
	Country     string `json:"country,omitempty" pattern:"^[A-Z]{2}$" doc:"ISO 3166-1 alpha-2"`
	Newsletter  bool   `json:"newsletter,omitempty"`
}

// Resolve refuses a display name that is only spaces, for every request.
func (f *profileFields) Resolve(huma.Context) []error {
	if f.DisplayName != "" && strings.TrimSpace(f.DisplayName) == "" {
		return []error{&huma.ErrorDetail{Location: "body.display_name", Message: "must not be blank", Value: f.DisplayName}}
	}
	return nil
}

func TestInvalidOptions(t *testing.T) {
	type fields struct {
		Email string `json:"EMAIL"`
	}
	for name, tc := range map[string]struct {
		opts []Option
		want string
	}{
		"short minimum":         {[]Option{MinPasswordLength(8)}, "MinPasswordLength(8)"},
		"long minimum":          {[]Option{MinPasswordLength(200)}, "MinPasswordLength(200)"},
		"nil policy":            {[]Option{PasswordPolicy(nil)}, "PasswordPolicy(nil)"},
		"short API key cap":     {[]Option{APIKeyMaxTTL(time.Hour)}, "APIKeyMaxTTL(1h0m0s)"},
		"nil hook":              {[]Option{BeforeLogin(nil), AfterLogin(nil), OnRegister(nil)}, "BeforeLogin(nil)"},
		"fields not a struct":   {[]Option{RegisterFields(func(context.Context, pgx.Tx, NewAccount, string) error { return nil })}, "the fields are a struct"},
		"fields named email":    {[]Option{RegisterFields(func(context.Context, pgx.Tx, NewAccount, fields) error { return nil })}, `the field "EMAIL" is sign-in's`},
		"fields twice":          {[]Option{RegisterFields(saveNothing), RegisterFields(saveNothing)}, "given twice"},
		"fields without signup": {[]Option{RegisterFields(saveNothing), WithoutRegistration()}, "without registration"},
	} {
		t.Run(name, func(t *testing.T) {
			err := New(tc.opts...).CheckConfig(gorbital.Config{Env: "development"})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("CheckConfig() = %v, want %q", err, tc.want)
			}
		})
	}
	if err := New().CheckConfig(gorbital.Config{Env: "development"}); err != nil {
		t.Errorf("CheckConfig() without options = %v", err)
	}
}

func saveNothing(context.Context, pgx.Tx, NewAccount, profileFields) error { return nil }

// TestPasswordOptions: the minimum and the policy apply to every new
// password, after the built-in rules.
func TestPasswordOptions(t *testing.T) {
	a := newHookApp(t, nil, []Option{
		MinPasswordLength(16),
		PasswordPolicy(func(_ context.Context, pw string) error {
			if strings.Contains(strings.ToLower(pw), "acme") {
				return errors.New("must not contain the app's name")
			}
			return nil
		}),
	})
	h := a.Handler()
	for _, tc := range []struct{ password, detail string }{
		{"short", "the password must be at least 12 characters"},
		{"fifteen chars..", "the password must be at least 16 characters"},
		{"acme acme acme acme", "the password must not contain the app's name"},
	} {
		r := do(t, h, "POST", "/v1/auth/register", fmt.Sprintf(`{"email":"ada@example.com","password":%q}`, tc.password))
		if r.code != http.StatusUnprocessableEntity || r.json["code"] != "weak_password" || r.json["detail"] != tc.detail {
			t.Errorf("register with %q = %d %s, want %q", tc.password, r.code, r.body, tc.detail)
		}
	}
	if r := do(t, h, "POST", "/v1/auth/register", `{"email":"ada@example.com","password":"sixteen chars ok"}`); r.code != http.StatusAccepted {
		t.Errorf("register with a long enough password = %d %s", r.code, r.body)
	}
	ops := actor.With(context.Background(), actor.System("test"))
	if _, err := a.Auth().CreateUser(ops, "bob@example.com", "fifteen chars..", true); err == nil {
		t.Errorf("CreateUser with a short password succeeded")
	}
}

// TestRequireMFA: a module's role grants its permissions only with a second
// factor; a role nobody declares, or user, fails Setup.
func TestRequireMFA(t *testing.T) {
	billing := func(*Authenticator) gorbital.Module {
		return gorbital.Module{
			Name:        "billing",
			Permissions: []gorbital.Permission{{Name: "billing.invoice.read", Description: "Read invoices", Roles: []string{"billing_admin"}}},
			Routes: func(r *gorbital.Router, _ gorbital.Deps) {
				gorbital.Get(r, "/v1/invoices", func(context.Context, *struct{}) (*struct{}, error) { return nil, nil }, guard.Permission("billing.invoice.read"))
			},
		}
	}
	a := newHookApp(t, nil, []Option{RequireMFA("billing_admin")}, billing)
	ops := actor.With(context.Background(), actor.System("test"))
	id := a.verifiedUser(t, "ada@example.com")
	if err := a.Auth().GrantRole(ops, id, "billing_admin"); err != nil {
		t.Fatal(err)
	}
	r := do(t, a.Handler(), "POST", "/v1/auth/login", login("ada@example.com", testPassword))
	token, _ := r.json["token"].(string)
	if r := do(t, a.Handler(), "GET", "/v1/invoices", "", "Authorization", "Bearer "+token); r.code != http.StatusForbidden || r.json["code"] != "mfa_required" {
		t.Errorf("GET /v1/invoices without a second factor = %d %s", r.code, r.body)
	}

	for _, role := range []string{"nobody_declares", "user"} {
		catalog := authlib.NewCatalog()
		catalog.Role("billing_admin", "Billing")
		err := New(RequireMFA(role)).Setup(context.Background(), gorbital.AuthSetup{Permissions: catalog})
		if err == nil || !strings.Contains(err.Error(), "RequireMFA") {
			t.Errorf("Setup with RequireMFA(%q) = %v", role, err)
		}
	}
}

// TestAPIKeyMaxTTL: the setting's range and default follow the cap.
func TestAPIKeyMaxTTL(t *testing.T) {
	a := newHookApp(t, nil, []Option{APIKeyMaxTTL(30 * 24 * time.Hour)})
	if got := a.auth.settings.apiKeyMaxTTL.Get(context.Background()); got != 30*24*time.Hour {
		t.Errorf("auth.api_key_max_ttl = %s, want the cap as its default", got)
	}
	if _, err := a.Deps().Settings.Set(actor.With(context.Background(), actor.System("test")), "auth.api_key_max_ttl", json.RawMessage(`"2160h"`), settings.Change{Reason: "longer"}); !errors.As(err, new(*settings.InvalidValueError)) {
		t.Errorf("setting auth.api_key_max_ttl above the cap succeeded")
	}
}

// TestWithoutRegistration: no sign-up, by email or by provider; operators'
// accounts and existing accounts' sign-ins still work.
func TestWithoutRegistration(t *testing.T) {
	srv := socialtest.New(t)
	a := newHookApp(t, socialEnv(t), []Option{WithoutRegistration()}, func(auth *Authenticator) gorbital.Module {
		auth.endpoints.Google = srv.Endpoints()
		return gorbital.Module{}
	})
	h := a.Handler()
	if r := do(t, h, "POST", "/v1/auth/register", `{"email":"ada@example.com","password":"correct horse battery"}`); r.code != http.StatusNotFound {
		t.Errorf("POST /v1/auth/register = %d %s, want 404", r.code, r.body)
	}
	doc := do(t, h, "GET", "/openapi.json", "")
	if strings.Contains(doc.body, `"/v1/auth/register"`) || !strings.Contains(doc.body, `"/v1/auth/login"`) {
		t.Errorf("the OpenAPI document still has POST /v1/auth/register, or lost login")
	}
	google := func(subject, email string) response {
		nonce := do(t, h, "POST", "/v1/auth/google/nonce", "")
		token := srv.IDToken(socialtest.Claims{Subject: subject, Audience: "ios-client", Email: email, EmailVerified: true, Nonce: fmt.Sprint(nonce.json["nonce"])})
		return do(t, h, "POST", "/v1/auth/google/token", fmt.Sprintf(`{"id_token":%q,"nonce":%q,"transport":"bearer"}`, token, nonce.json["nonce"]))
	}
	if r := google("g-new", "new@gmail.com"); r.code != http.StatusForbidden || r.json["code"] != "registration_closed" || a.countUsers(t, "new@gmail.com") != 0 {
		t.Errorf("first Google sign-in = %d %s", r.code, r.body)
	}
	// A web sign-in gets the code in the redirect's fragment.
	start := do(t, h, "GET", "/v1/auth/google/start?return_to="+url.QueryEscape("https://app.example.com/after"), "")
	location, _ := url.Parse(start.header.Get("Location"))
	q := location.Query()
	code := srv.Code(socialtest.Claims{Subject: "g-web", Audience: "web-client", Email: "web@gmail.com", EmailVerified: true, Nonce: q.Get("nonce")}, "")
	callback := do(t, h, "GET", "/v1/auth/google/callback?code="+url.QueryEscape(code)+"&state="+url.QueryEscape(q.Get("state")), "", "Cookie", "__Host-oauth="+cookieValue(start, "__Host-oauth"))
	if callback.code != http.StatusSeeOther || callback.header.Get("Location") != "https://app.example.com/after#error=registration_closed" || cookieValue(callback, "__Host-session") != "" {
		t.Errorf("web Google sign-up = %d %q", callback.code, callback.header.Get("Location"))
	}

	// An account an operator created signs in, with its password or Google.
	a.verifiedUser(t, "ada@gmail.com")
	if r := do(t, h, "POST", "/v1/auth/login", login("ada@gmail.com", testPassword)); r.code != http.StatusOK {
		t.Errorf("password sign-in = %d %s", r.code, r.body)
	}
	if r := google("g-ada", "ada@gmail.com"); r.code != http.StatusOK {
		t.Errorf("Google sign-in to an existing account = %d %s", r.code, r.body)
	}
}

// TestBrandAndRouteMiddleware: emails carry the brand; the middleware runs
// on /v1/auth/ and not on /ops/auth/users.
func TestBrandAndRouteMiddleware(t *testing.T) {
	var paths hookCalls[string]
	a := newHookApp(t, nil, []Option{
		Brand(mail.Brand{SupportEmail: "help@acme.example", Footer: "Acme Ltd, 1 Main Street"}),
		RouteMiddleware(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths.add(r.URL.Path)
				if r.Header.Get("X-Captcha") == "fail" {
					http.Error(w, "captcha", http.StatusTeapot)
					return
				}
				next.ServeHTTP(w, r)
			})
		}),
	})
	h := a.Handler()
	if r := do(t, h, "POST", "/v1/auth/register", `{"email":"ada@example.com","password":"correct horse battery"}`, "X-Captcha", "fail"); r.code != http.StatusTeapot {
		t.Errorf("register refused by the middleware = %d", r.code)
	}
	if r := do(t, h, "POST", "/v1/auth/register", `{"email":"ada@example.com","password":"correct horse battery"}`); r.code != http.StatusAccepted {
		t.Fatalf("register = %d %s", r.code, r.body)
	}
	do(t, h, "GET", "/ops/auth/users", "")
	if got := paths.all(); !slices.Equal(got, []string{"/v1/auth/register", "/v1/auth/register"}) {
		t.Errorf("middleware ran on %v", got)
	}
	var text string
	if err := a.pool.QueryRow(context.Background(), `SELECT args->'message'->>'text' FROM river_job WHERE kind = 'gorbital.mail.send' ORDER BY id DESC LIMIT 1`).Scan(&text); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "help@acme.example") || !strings.Contains(text, "Acme Ltd, 1 Main Street") || !strings.Contains(text, testAppName) {
		t.Errorf("verification email = %q, want the brand", text)
	}
}

// TestRegisterFields: the fields are validated for every request, shown in
// OpenAPI, and saved in the account's transaction after OnRegister.
func TestRegisterFields(t *testing.T) {
	var order hookCalls[string]
	var saved hookCalls[profileFields]
	a := newHookApp(t, nil, []Option{
		RegisterFields(func(ctx context.Context, tx pgx.Tx, acct NewAccount, f profileFields) error {
			order.add("fields")
			saved.add(f)
			_, err := tx.Exec(ctx, `INSERT INTO hook_rows VALUES ($1, $2)`, acct.User.ID, f.DisplayName+"/"+f.Country)
			return err
		}),
		OnRegister(func(context.Context, pgx.Tx, NewAccount) error { order.add("on_register"); return nil }),
	})
	h := a.Handler()

	doc := do(t, h, "GET", "/openapi.json", "")
	var openapi struct {
		Paths map[string]map[string]struct {
			RequestBody struct {
				Content map[string]struct {
					Schema struct {
						Ref string `json:"$ref"`
					} `json:"schema"`
				} `json:"content"`
			} `json:"requestBody"`
		} `json:"paths"`
		Components struct {
			Schemas map[string]struct {
				Properties           map[string]any `json:"properties"`
				Required             []string       `json:"required"`
				AdditionalProperties any            `json:"additionalProperties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(doc.body), &openapi); err != nil {
		t.Fatal(err)
	}
	ref := openapi.Paths["/v1/auth/register"]["post"].RequestBody.Content["application/json"].Schema.Ref
	schema := openapi.Components.Schemas[strings.TrimPrefix(ref, "#/components/schemas/")]
	for _, p := range []string{"email", "password", "display_name", "country", "newsletter"} {
		if schema.Properties[p] == nil {
			t.Errorf("register body schema %s lacks %s: %+v", ref, p, schema)
		}
	}
	if !slices.Contains(schema.Required, "display_name") || slices.Contains(schema.Required, "country") || schema.AdditionalProperties != true {
		t.Errorf("register body schema required %v additionalProperties %v", schema.Required, schema.AdditionalProperties)
	}

	// Invalid fields: 422 for a new address and an existing one alike.
	a.verifiedUser(t, "exists@example.com")
	order = hookCalls[string]{}
	for _, email := range []string{"new@example.com", "exists@example.com"} {
		for _, body := range []string{
			`{"email":%q,"password":"correct horse battery"}`,
			`{"email":%q,"password":"correct horse battery","display_name":"Ada","country":"france"}`,
			`{"email":%q,"password":"correct horse battery","display_name":"   "}`,
		} {
			r := do(t, h, "POST", "/v1/auth/register", fmt.Sprintf(body, email))
			if r.code != http.StatusUnprocessableEntity || r.json["code"] != "validation_failed" {
				t.Errorf("register %s = %d %s, want 422 validation_failed", fmt.Sprintf(body, email), r.code, r.body)
			}
		}
	}
	if len(saved.all()) != 0 || a.countUsers(t, "new@example.com") != 0 {
		t.Errorf("invalid fields reached the hook or created an account")
	}

	// Valid fields, with a property the body doesn't name (v0.1 clients may
	// send more).
	r := do(t, h, "POST", "/v1/auth/register", `{"email":"ada@example.com","password":"correct horse battery","display_name":"Ada","country":"GB","newsletter":true,"referrer":"blog"}`)
	if r.code != http.StatusAccepted {
		t.Fatalf("register = %d %s", r.code, r.body)
	}
	if got := saved.all(); len(got) != 1 || got[0] != (profileFields{DisplayName: "Ada", Country: "GB", Newsletter: true}) {
		t.Errorf("saved fields = %+v", got)
	}
	if got := order.all(); !slices.Equal(got, []string{"on_register", "fields"}) {
		t.Errorf("hooks ran in order %v", got)
	}
	if notes := a.notes(t, ""); !slices.Equal(notes, []string{"Ada/GB"}) {
		t.Errorf("hook_rows = %v", notes)
	}
	// Operators' accounts run OnRegister, not the fields hook.
	a.verifiedUser(t, "op@example.com")
	if got := order.all(); !slices.Equal(got, []string{"on_register", "fields", "on_register"}) {
		t.Errorf("hooks ran in order %v, want the fields hook only for the email registration", got)
	}
}
