package main

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/flagshttp"
	"gorbital.dev/gorbital/gorbitaltest"
	"gorbital.dev/gorbital/opshttp"

	"example.com/admin-tool/db/migrations"
	"example.com/admin-tool/internal/modules"
	"example.com/admin-tool/internal/modules/announcements"
	authhttp "example.com/admin-tool/internal/modules/auth"
)

// These tests are in package main to build sign-in with signInOptions, as
// main.go does; operations_test.go builds the app without it, so its
// operators are gorbitaltest principals.

// staffPassword is long enough for MinPasswordLength(16).
const staffPassword = "a long enough admin password"

// docs:start new-signed-in-app

// newSignedInApp builds the admin tool with main.go's options, sign-in
// included, so accounts, roles and second factors are the real ones.
func newSignedInApp(t *testing.T) *gorbitaltest.App {
	t.Helper()
	return gorbitaltest.New(t,
		gorbital.WithName("admin-tool"),
		gorbital.WithAuth(authhttp.New(signInOptions()...)),
		gorbital.WithModules(opshttp.Module(), flagshttp.Module()),
		gorbital.WithModules(modules.All()...),
		gorbital.WithMigrations(migrations.FS),
	)
}

// docs:end new-signed-in-app

// docs:start no-sign-up

// TestNobodySignsUp: WithoutRegistration leaves POST /v1/auth/register
// unserved and out of the OpenAPI document, so a client generated from it
// doesn't offer sign-up. Signing in is still there.
func TestNobodySignsUp(t *testing.T) {
	app := newSignedInApp(t)

	app.Client().Post("/v1/auth/register", map[string]string{"email": "ada@example.com", "password": staffPassword}).
		AssertStatus(t, http.StatusNotFound)

	var doc struct {
		Paths map[string]json.RawMessage `json:"paths"`
	}
	app.Client().Get("/openapi.json").JSON(t, &doc)
	if _, ok := doc.Paths["/v1/auth/register"]; ok {
		t.Error("the OpenAPI document still has POST /v1/auth/register")
	}
	if _, ok := doc.Paths["/v1/auth/login"]; !ok {
		t.Error("the OpenAPI document lost POST /v1/auth/login")
	}
}

// docs:end no-sign-up

// docs:start operator-creates-staff

// TestOperatorsCreateStaffAccounts: an operator creates the account and
// grants announcements_editor; the password minimum applies to their call;
// and the new staff member, signed in without a second factor, doesn't hold
// the role's permission yet.
func TestOperatorsCreateStaffAccounts(t *testing.T) {
	app := newSignedInApp(t)
	operator := app.As(gorbitaltest.User("usr_ops", "ops.auth.read", "ops.auth.write"))

	operator.Post("/ops/auth/users", map[string]any{"email": "ada@example.com", "password": "fifteen chars.!", "email_verified": true}).
		AssertProblem(t, http.StatusUnprocessableEntity, "weak_password")

	res := operator.Post("/ops/auth/users", map[string]any{"email": "ada@example.com", "password": staffPassword, "email_verified": true})
	res.AssertStatus(t, http.StatusCreated)
	var account struct {
		ID string `json:"id"`
	}
	res.JSON(t, &account)
	operator.Post("/ops/auth/users/"+account.ID+"/roles", map[string]string{"role": announcements.RoleEditor}).
		AssertStatus(t, http.StatusOK)

	res = app.Client().Post("/v1/auth/login", map[string]string{"email": "ada@example.com", "password": staffPassword, "transport": "bearer"})
	res.AssertStatus(t, http.StatusOK)
	var session struct {
		Token string `json:"token"`
	}
	res.JSON(t, &session)
	ada := app.Client().WithHeader("Authorization", "Bearer "+session.Token)

	// RequireMFA(announcements.RoleEditor): the role grants nothing until
	// Ada enrols a second factor and signs in with it.
	ada.Post("/v1/announcements", map[string]any{
		"title": "Scheduled maintenance", "body": "Saturday, 02:00 to 03:00 UTC.", "ends_at": time.Now().Add(time.Hour),
	}).AssertProblem(t, http.StatusForbidden, "mfa_required")
}

// docs:end operator-creates-staff

// docs:start no-robot-editors

// TestNoRobotPublishes: a role requiring a second factor can't be given to
// a service account, so no API key ever holds the editor role.
func TestNoRobotPublishes(t *testing.T) {
	app := newSignedInApp(t)
	operator := app.As(gorbitaltest.User("usr_ops", "ops.service_accounts.read", "ops.service_accounts.write"))

	operator.Post("/ops/service-accounts", map[string]any{"name": "Status page", "roles": []string{announcements.RoleEditor}}).
		AssertProblem(t, http.StatusUnprocessableEntity, "invalid_service_account_role")
	operator.Post("/ops/service-accounts", map[string]any{"name": "Status page"}).
		AssertStatus(t, http.StatusCreated)
}

// docs:end no-robot-editors

// docs:start api-key-ttl

// TestAPIKeysExpireWithinTheMonth: APIKeyMaxTTL narrows the runtime setting
// operators change, so a key can't outlive 30 days.
func TestAPIKeysExpireWithinTheMonth(t *testing.T) {
	app := newSignedInApp(t)
	operator := app.As(gorbitaltest.User("usr_ops", "ops.settings.read", "ops.settings.write"))

	var setting struct {
		Value string `json:"value"`
	}
	operator.Get("/ops/settings/auth.api_key_max_ttl").JSON(t, &setting)
	if d, err := time.ParseDuration(setting.Value); err != nil || d != 30*24*time.Hour {
		t.Errorf("auth.api_key_max_ttl = %q (%v), want 720h, the cap as its default", setting.Value, err)
	}
	operator.Put("/ops/settings/auth.api_key_max_ttl", map[string]any{"value": "2160h", "version": 0, "reason": "longer keys for the import script"}).
		AssertProblem(t, http.StatusUnprocessableEntity, "invalid_setting_value")
}

// docs:end api-key-ttl
