package authhttp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/gorbital"
	authlib "gorbital.dev/modules/auth"
	"gorbital.dev/modules/jobs"
	"gorbital.dev/modules/postgres/pgtest"
	"gorbital.dev/modules/settings"
)

// The Methods tests check what an app serves when it chooses its sign-in
// methods (ADR-0089): the operations of each one, that a method's paths are
// 404 without it, and that it declares no settings, jobs, limiters or
// permissions. The default, every method, is v0.2.1's document.

// methodOperations are the operation IDs each sign-in method registers,
// which together are the 66 operations delivery.Register adds. The other 8
// of the 74 in the delivery package are organisations' service accounts,
// which Authenticator.OrgServiceAccountRoutes registers with MethodAPIKeys.
var methodOperations = map[Method][]string{
	MethodPassword: {
		"auth-change-password", "auth-delete-account", "auth-forgot-password", "auth-list-sessions",
		"auth-login", "auth-logout", "auth-logout-all", "auth-me", "auth-register",
		"auth-resend-verification", "auth-reset-password", "auth-revoke-session", "auth-verify-email",
	},
	MethodOperators: {
		"ops-ban-user", "ops-create-user", "ops-delete-user", "ops-enroll-user-totp", "ops-get-user",
		"ops-grant-user-role", "ops-impersonate-user", "ops-list-users", "ops-remove-user-identity",
		"ops-remove-user-passkey", "ops-reset-user-mfa", "ops-revoke-user-role", "ops-revoke-user-session",
		"ops-revoke-user-sessions", "ops-unban-user", "ops-verify-user-email",
	},
	MethodTOTP: {
		"auth-confirm-totp", "auth-disable-totp", "auth-login-mfa", "auth-regenerate-recovery-codes",
		"auth-start-totp",
	},
	MethodPasskeys: {
		"auth-begin-passkey-registration", "auth-begin-passkey-verification", "auth-create-passkey",
		"auth-list-passkeys", "auth-login-mfa-passkey", "auth-passkey-login", "auth-passkey-login-options",
		"auth-remove-passkey", "auth-rename-passkey",
	},
	MethodSocial: {
		"auth-apple-callback", "auth-apple-notifications", "auth-apple-token", "auth-github-callback",
		"auth-google-callback", "auth-google-token", "auth-link-identity", "auth-list-identities",
		"auth-remove-identity", "auth-social-nonce", "auth-start-identity-link", "auth-start-social",
	},
	MethodAPIKeys: {
		"auth-create-api-key", "auth-list-api-keys", "auth-revoke-api-key",
		"ops-create-service-account", "ops-create-service-account-key", "ops-delete-service-account",
		"ops-get-service-account", "ops-list-service-account-keys", "ops-list-service-accounts",
		"ops-revoke-service-account-key", "ops-update-service-account",
	},
}

// methodPaths are an operation of each method that isn't MethodPassword,
// each behind a session and with a body the operation accepts, so an app
// that serves the method answers 401 and one that doesn't answers 404.
var methodPaths = map[Method]struct{ method, path, body string }{
	MethodOperators: {http.MethodGet, "/ops/auth/users", ""},
	MethodTOTP:      {http.MethodPost, "/v1/auth/mfa/totp", `{"password":"correct horse battery"}`},
	MethodPasskeys:  {http.MethodGet, "/v1/auth/passkeys", ""},
	MethodSocial:    {http.MethodGet, "/v1/auth/identities", ""},
	MethodAPIKeys:   {http.MethodGet, "/v1/auth/api-keys", ""},
}

// operationIDs are the operation IDs of doc, sorted.
func operationIDs(t *testing.T, doc []byte) []string {
	t.Helper()
	var parsed openAPIDoc
	if err := json.Unmarshal(doc, &parsed); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, item := range parsed.Paths {
		for _, raw := range item {
			var op struct {
				ID string `json:"operationId"`
			}
			if err := json.Unmarshal(raw, &op); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, op.ID)
		}
	}
	slices.Sort(ids)
	return ids
}

// wantOperations are the operation IDs of the given methods, sorted.
func wantOperations(methods ...Method) []string {
	var ids []string
	for _, m := range methods {
		ids = append(ids, methodOperations[m]...)
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

// TestMethodsServeTheirOperations: each method registers exactly its
// operations beside MethodPassword's, and every method together is the 66
// operations of an app without the option.
func TestMethodsServeTheirOperations(t *testing.T) {
	for _, m := range allMethods {
		t.Run(string(m), func(t *testing.T) {
			got := operationIDs(t, authDocumentJSON(t, Methods(MethodPassword, m)))
			if want := wantOperations(MethodPassword, m); !slices.Equal(got, want) {
				t.Errorf("Methods(password, %s) serves\n%v\nwant\n%v", m, got, want)
			}
		})
	}
	t.Run("password alone", func(t *testing.T) {
		got := operationIDs(t, authDocumentJSON(t, Methods(MethodPassword)))
		if want := wantOperations(MethodPassword); !slices.Equal(got, want) {
			t.Errorf("Methods(password) serves\n%v\nwant\n%v", got, want)
		}
	})
	t.Run("every method", func(t *testing.T) {
		want := wantOperations(allMethods...)
		if len(want) != 66 {
			t.Fatalf("the methods list %d operations, want 66", len(want))
		}
		if got := operationIDs(t, authDocumentJSON(t)); !slices.Equal(got, want) {
			t.Errorf("without the option sign-in serves\n%v\nwant\n%v", got, want)
		}
		if got := operationIDs(t, authDocumentJSON(t, Methods(allMethods...))); !slices.Equal(got, want) {
			t.Errorf("Methods with every method differs from the default:\n%v", got)
		}
	})
	t.Run("basic", func(t *testing.T) {
		// ADR-0089's basic profile: 29 of the 74 operations in the
		// delivery package, none of them a passkey's, a provider's or a
		// key's.
		got := operationIDs(t, authDocumentJSON(t, Methods(MethodPassword, MethodOperators)))
		if want := wantOperations(MethodPassword, MethodOperators); !slices.Equal(got, want) || len(got) != 29 {
			t.Errorf("a basic app serves %d operations\n%v\nwant 29\n%v", len(got), got, want)
		}
	})
}

// TestDisabledMethodPathIsNotFound: a path of a method the app doesn't
// serve is 404 from the router, never 401 (which would say the operation
// is there, authenticate) or 500 (which would say it is there and broken).
// The same path with the method on is 401, so the difference is the method
// and not the path.
func TestDisabledMethodPathIsNotFound(t *testing.T) {
	for m, op := range methodPaths {
		t.Run(string(m), func(t *testing.T) {
			others := slices.DeleteFunc(slices.Clone(allMethods), func(other Method) bool { return other == m })
			if r := do(t, authMux(t, Methods(others...)), op.method, op.path, op.body); r.code != http.StatusNotFound {
				t.Errorf("%s %s without %s = %d %s, want 404", op.method, op.path, m, r.code, r.body)
			}
			if r := do(t, authMux(t, Methods(allMethods...)), op.method, op.path, op.body); r.code != http.StatusUnauthorized {
				t.Errorf("%s %s with %s = %d %s, want 401", op.method, op.path, m, r.code, r.body)
			}
		})
	}
}

// declarations are what an app's modules declare: the permission catalog
// with the platform roles granted, and the runtime settings.
func declarations(t *testing.T, opts ...Option) (*authlib.Catalog, *settings.Registry) {
	t.Helper()
	module := New(opts...).Module()
	catalog, reg := authlib.NewCatalog(), settings.NewRegistry()
	if err := gorbital.Declare(gorbital.Declarations{Permissions: catalog, Settings: reg}, module); err != nil {
		t.Fatalf("Declare() error = %v", err)
	}
	for _, role := range []string{rolePlatformAdmin, roleOpsViewer} {
		catalog.Role(role, "", gorbital.Grants(role, module)...)
	}
	return catalog, reg
}

// TestDisabledMethodDeclaresNothing: a method the app doesn't serve has no
// permissions in the catalog, no runtime settings, no jobs and no rate
// limiters, so /ops lists none of them.
func TestDisabledMethodDeclaresNothing(t *testing.T) {
	basic := []Option{Methods(MethodPassword, MethodOperators)}

	t.Run("permissions", func(t *testing.T) {
		catalog, _ := declarations(t, basic...)
		for _, p := range catalog.AllPermissions() {
			if strings.HasPrefix(p.Name, "ops.service_accounts.") {
				t.Errorf("permission %q is declared by an app without API keys", p.Name)
			}
		}
		for _, role := range []string{rolePlatformAdmin, roleOpsViewer} {
			if perms := catalog.Permissions(role); slices.ContainsFunc(perms, func(p string) bool {
				return strings.HasPrefix(p, "ops.service_accounts.")
			}) {
				t.Errorf("role %q holds a service-account permission: %v", role, perms)
			}
		}
		// A role can't be granted one: the catalog refuses an undeclared
		// permission, so a grant fails at startup instead of waiting for
		// the method to be turned on.
		defer func() {
			if recover() == nil {
				t.Error("the catalog granted a role a permission of a method the app doesn't serve")
			}
		}()
		catalog.Role("support", "", "ops.service_accounts.read")
	})

	t.Run("settings", func(t *testing.T) {
		_, reg := declarations(t, basic...)
		for _, key := range reg.Keys() {
			switch key {
			case "auth.api_key_max_ttl", "auth.api_key_failures_per_minute", "auth.mfa_change_attempts":
				t.Errorf("setting %q is declared by an app without its method", key)
			}
		}
		// With the methods on they are back, in v0.2's order.
		_, every := declarations(t)
		for _, key := range []string{"auth.mfa_change_attempts", "auth.api_key_max_ttl", "auth.api_key_failures_per_minute"} {
			if !slices.Contains(every.Keys(), key) {
				t.Errorf("setting %q isn't declared by an app that serves every method", key)
			}
		}
	})

	t.Run("jobs", func(t *testing.T) {
		defs := jobs.NewDefinitions()
		New(basic...).Module().Jobs(defs, gorbital.Deps{})
		if got := defs.Names(); !slices.Equal(got, []string{"auth_cleanup"}) {
			t.Errorf("an app without Google, Apple or GitHub sign-in defines %v, want only auth_cleanup", got)
		}
	})

	t.Run("rate limiters", func(t *testing.T) {
		var names []string
		for _, l := range New(basic...).Module().RateLimiters {
			names = append(names, l.Name)
		}
		want := []string{"auth_login", "auth_login_address", "auth_reauth", "auth_code", "auth_notice"}
		if !slices.Equal(names, want) {
			t.Errorf("a basic app's limiters are %v, want %v", names, want)
		}
	})
}

// TestMethodsNeedPassword: a set without MethodPassword, or with a method
// that doesn't exist, is refused by CheckConfig, as an invalid option is,
// so the app exits with status 2 before it connects.
func TestMethodsNeedPassword(t *testing.T) {
	for _, tc := range []struct {
		name    string
		methods []Method
		want    string
	}{
		{"without passwords", []Method{MethodOperators, MethodTOTP}, "password is required"},
		{"empty", nil, "password is required"},
		{"unknown", []Method{MethodPassword, Method("magic_links")}, `Methods("magic_links")`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := New(Methods(tc.methods...)).CheckConfig(gorbital.Config{Env: "development"})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("CheckConfig() error = %v, want one naming %q", err, tc.want)
			}
		})
	}
	if err := New(Methods(allMethods...)).CheckConfig(gorbital.Config{Env: "development"}); err != nil {
		t.Errorf("CheckConfig() with every method = %v, want no error", err)
	}
}

// v021Prefixes are the paths of the frozen v0.2.1 document the Methods
// option could change: /ops/service-accounts is the rest of sign-in's
// surface, which TestOpenAPIMatchesV010 covers.
var v021Prefixes = []string{"/v1/auth/", "/ops/auth/users"}

// underV021Prefixes reports whether path is one the default method set
// must keep.
func underV021Prefixes(path string) bool {
	return slices.ContainsFunc(v021Prefixes, func(p string) bool { return strings.HasPrefix(path, p) })
}

// v021Document is the frozen v0.2.1 document of the full-single app.
func v021Document(t *testing.T) openAPIDoc {
	t.Helper()
	var doc openAPIDoc
	path := filepath.Join(repo, "internal", "contracts", "v0.2.1", "examples", "full-single", "api", "openapi.json")
	if err := json.Unmarshal(readFile(t, path), &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

// TestDefaultMethodsServeV021Operations: without the option, the
// operations under /v1/auth/ and /ops/auth/users are exactly the frozen
// v0.2.1 document's, by path, method and operation ID.
func TestDefaultMethodsServeV021Operations(t *testing.T) {
	base, cur := v021Document(t), openAPIDoc{}
	if err := json.Unmarshal(authDocumentJSON(t), &cur); err != nil {
		t.Fatal(err)
	}
	operations := 0
	for path, item := range base.Paths {
		if !underV021Prefixes(path) {
			continue
		}
		for method, op := range item {
			operations++
			got, ok := cur.Paths[path][method]
			if !ok {
				t.Errorf("%s %s of v0.2.1 is missing without the Methods option", method, path)
				continue
			}
			if a, b := operationID(t, op), operationID(t, got); a != b {
				t.Errorf("%s %s is %q, was v0.2.1's %q", method, path, b, a)
			}
		}
	}
	for path, item := range cur.Paths {
		for method := range item {
			if _, ok := base.Paths[path][method]; underV021Prefixes(path) && !ok {
				t.Errorf("%s %s is new under sign-in's paths", method, path)
			}
		}
	}
	if operations != 58 {
		t.Errorf("compared %d operations, want v0.2.1's 58", operations)
	}
}

// operationID is raw's operationId.
func operationID(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var op struct {
		ID string `json:"operationId"`
	}
	if err := json.Unmarshal(raw, &op); err != nil {
		t.Fatal(err)
	}
	return op.ID
}

// TestDefaultMethodsMatchV021Document: an app without the option serves
// those operations byte for byte as v0.2.1 documented them — parameters,
// request bodies, responses and guards — so the default is v0.2's
// contract and not merely its operation IDs.
func TestDefaultMethodsMatchV021Document(t *testing.T) {
	base := v021Document(t)
	r := do(t, newApp(t, nil).Handler(), "GET", "/openapi.json", "")
	if r.code != http.StatusOK {
		t.Fatalf("GET /openapi.json = %d", r.code)
	}
	var cur openAPIDoc
	if err := json.Unmarshal([]byte(r.body), &cur); err != nil {
		t.Fatal(err)
	}
	for path, item := range base.Paths {
		if !underV021Prefixes(path) {
			continue
		}
		for method, op := range item {
			got, ok := cur.Paths[path][method]
			if !ok {
				t.Errorf("%s %s of v0.2.1 is missing", method, path)
				continue
			}
			if a, b := canonical(t, op), canonical(t, got); a != b {
				t.Errorf("%s %s changed:\nv0.2.1: %s\nnow:    %s", method, path, a, b)
			}
		}
	}
}

// newAppWithOptions is newAppWithURL for an app whose sign-in takes
// options, such as Methods.
func newAppWithOptions(t *testing.T, opts ...Option) (*testApp, string) {
	t.Helper()
	url := pgtest.NewDatabase(t)
	cfg := testConfig(t, map[string]string{"DATABASE_URL": url})
	return buildAppWith(t, cfg, New(opts...)), url
}

// TestBasicAppKeepsEveryTable: an app that serves passwords and the
// operators' account APIs migrates all eight of sign-in's migrations, so
// the passkey, social and API-key tables are there and stay empty
// (ADR-0089, decision 3). Turning a method on later is one line in
// main.go, with no migration to catch up on.
func TestBasicAppKeepsEveryTable(t *testing.T) {
	a, url := newAppWithOptions(t, Methods(MethodPassword, MethodOperators))
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	// An account registers and verifies, so the flows a basic app does
	// serve have run.
	const email, password = "ada@example.com", "a long enough password"
	creds := fmt.Sprintf(`{"email":%q,"password":%q}`, email, password)
	h := a.Handler()
	if r := do(t, h, "POST", "/v1/auth/register", creds); r.code != http.StatusAccepted {
		t.Fatalf("register = %d %s", r.code, r.body)
	}
	if r := do(t, h, "POST", "/v1/auth/verify-email", fmt.Sprintf(`{"email":%q,"code":%q}`, email, emailedCode(t, pool, email))); r.code != http.StatusNoContent {
		t.Fatalf("verify = %d %s", r.code, r.body)
	}

	ctx := context.Background()
	for _, table := range []string{"auth_passkeys", "auth_webauthn_ceremonies", "auth_identities", "auth_oauth_states", "auth_social_nonces", "auth_api_keys", "auth_service_accounts"} {
		var rows int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&rows); err != nil {
			t.Errorf("%s: %v", table, err)
			continue
		}
		if rows != 0 {
			t.Errorf("%s has %d rows in an app that doesn't serve its method", table, rows)
		}
	}
}
