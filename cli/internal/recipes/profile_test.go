package recipes_test

import (
	"slices"
	"strings"
	"testing"

	"gorbital.dev/cli/internal/recipes"
)

// TestLegalProfiles: the nine combinations, and nothing else.
func TestLegalProfiles(t *testing.T) {
	profiles := recipes.LegalProfiles()
	if len(profiles) != 9 {
		t.Fatalf("LegalProfiles() has %d profiles, want 9: %v", len(profiles), profiles)
	}
	seen := map[string]bool{}
	for _, p := range profiles {
		if seen[p.String()] {
			t.Errorf("LegalProfiles() repeats %s", p)
		}
		seen[p.String()] = true
		if !p.Legal() {
			t.Errorf("%s is in LegalProfiles() but Legal() is false", p)
		}
		got, err := recipes.ParseProfile(p.Auth, p.Scope)
		if err != nil {
			t.Errorf("ParseProfile(%q, %q): %v", p.Auth, p.Scope, err)
			continue
		}
		if got.ScopeName != p.ScopeName {
			t.Errorf("ParseProfile(%q, %q).ScopeName = %q, want %q", p.Auth, p.Scope, got.ScopeName, p.ScopeName)
		}
	}
	for _, want := range []string{
		"--auth none --scope none",
		"--auth basic --scope none", "--auth basic --scope single",
		"--auth basic --scope custom", "--auth basic --scope organisation",
		"--auth full --scope none", "--auth full --scope single",
		"--auth full --scope custom", "--auth full --scope organisation",
	} {
		if !seen[want] {
			t.Errorf("LegalProfiles() is missing %s", want)
		}
	}
}

// TestParseProfileRefusesIllegal: --auth none pairs only with --scope none,
// and the refusal says why.
func TestParseProfileRefusesIllegal(t *testing.T) {
	for _, scope := range []string{"single", "custom", "organisation", "merchant"} {
		_, err := recipes.ParseProfile(recipes.AuthNone, scope)
		if err == nil {
			t.Fatalf("ParseProfile(none, %q) = no error, want a refusal", scope)
		}
		for _, want := range []string{
			"--auth none --scope " + scope + " is not a shape orb can create",
			"A scope decides which user may act in it",
			"Use --scope none, or --auth basic.",
		} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("ParseProfile(none, %q) error is missing %q:\n%s", scope, want, err)
			}
		}
	}
}

func TestParseProfileRejectsUnknownValues(t *testing.T) {
	if _, err := recipes.ParseProfile("some", recipes.ScopeNone); err == nil || !strings.Contains(err.Error(), recipes.AuthUsage) {
		t.Errorf("ParseProfile(some, none) = %v, want an error naming %s", err, recipes.AuthUsage)
	}
	for _, scope := range []string{"Merchant", "merchant-shop", "merchants2", ""} {
		if scope == "" {
			continue
		}
		if _, err := recipes.ParseProfile(recipes.AuthFull, scope); err == nil {
			t.Errorf("ParseProfile(full, %q) = no error, want a refusal", scope)
		}
	}
}

// TestParseProfileDefaults: no flags is today's Full single-tenant app.
func TestParseProfileDefaults(t *testing.T) {
	p, err := recipes.ParseProfile("", "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Auth != recipes.AuthFull || p.Scope != recipes.ScopeSingle {
		t.Errorf("ParseProfile(\"\", \"\") = %s, want --auth full --scope single", p)
	}
}

// TestProfileCopies: what orb new copies into the app, and in what order.
func TestProfileCopies(t *testing.T) {
	names := func(p recipes.Profile) []string {
		var out []string
		for _, m := range p.Copies() {
			out = append(out, m.Name)
		}
		return out
	}
	cases := []struct {
		auth, scope string
		want        []string
	}{
		{recipes.AuthNone, recipes.ScopeNone, nil},
		{recipes.AuthBasic, recipes.ScopeNone, []string{"auth"}},
		{recipes.AuthBasic, recipes.ScopeSingle, []string{"auth"}},
		{recipes.AuthBasic, recipes.ScopeCustom, []string{"auth"}},
		{recipes.AuthBasic, "merchant", []string{"orgs", "auth"}},
		{recipes.AuthFull, recipes.ScopeNone, []string{"auth"}},
		{recipes.AuthFull, "organisation", []string{"orgs", "auth"}},
	}
	for _, tc := range cases {
		p, err := recipes.ParseProfile(tc.auth, tc.scope)
		if err != nil {
			t.Fatal(err)
		}
		if got := names(p); !slices.Equal(got, tc.want) {
			t.Errorf("%s copies %v, want %v", p, got, tc.want)
		}
	}
}

// TestProfileMethods: basic is password and operators, full is every method.
func TestProfileMethods(t *testing.T) {
	basic, err := recipes.ParseProfile(recipes.AuthBasic, recipes.ScopeNone)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := basic.MethodList(), "[password, operators]"; got != want {
		t.Errorf("basic methods = %s, want %s", got, want)
	}
	if got, want := basic.Operations(), 29; got != want {
		t.Errorf("basic operations = %d, want %d", got, want)
	}
	if basic.AllMethods() {
		t.Error("basic serves every method")
	}
	full, err := recipes.ParseProfile(recipes.AuthFull, recipes.ScopeSingle)
	if err != nil {
		t.Fatal(err)
	}
	if !full.AllMethods() {
		t.Errorf("full methods = %s, want every method", full.MethodList())
	}
	if got, want := full.Operations(), 74; got != want {
		t.Errorf("full operations = %d, want %d", got, want)
	}
	if got, want := full.Migrations(), 8; got != want {
		t.Errorf("full migrations = %d, want %d", got, want)
	}
	none, err := recipes.ParseProfile(recipes.AuthNone, recipes.ScopeNone)
	if err != nil {
		t.Fatal(err)
	}
	if none.Operations() != 0 || none.Migrations() != 0 || none.SignsIn() {
		t.Errorf("--auth none costs %d operations and %d migrations", none.Operations(), none.Migrations())
	}
}

// TestProfileFromTenancy: the deprecated alias keeps writing v0.2.1's apps.
func TestProfileFromTenancy(t *testing.T) {
	single, err := recipes.ProfileFromTenancy(recipes.TenancySingle)
	if err != nil {
		t.Fatal(err)
	}
	if single.Auth != recipes.AuthFull || single.Scope != recipes.ScopeSingle {
		t.Errorf("--tenancy single = %s", single)
	}
	multi, err := recipes.ProfileFromTenancy(recipes.TenancyMulti)
	if err != nil {
		t.Fatal(err)
	}
	if multi.Auth != recipes.AuthFull || multi.ScopeName != recipes.DefaultScopeName {
		t.Errorf("--tenancy multi = %s", multi)
	}
	if got, want := multi.Tenancy(), recipes.TenancyMulti; got != want {
		t.Errorf("Tenancy() = %q, want %q", got, want)
	}
	if got, want := single.Tenancy(), recipes.TenancySingle; got != want {
		t.Errorf("Tenancy() = %q, want %q", got, want)
	}
	if _, err := recipes.ProfileFromTenancy("some"); err == nil {
		t.Error("ProfileFromTenancy(some) = no error")
	}
}

// TestProfileVocabulary: a named scope's words, and organisations for the
// scopes that name none.
func TestProfileVocabulary(t *testing.T) {
	merchant, err := recipes.ParseProfile(recipes.AuthFull, "merchant")
	if err != nil {
		t.Fatal(err)
	}
	v := merchant.Vocabulary()
	if v.Name != "merchant" || v.PathParam != "merchantId" || v.Column != "merchant_id" || !v.Declared {
		t.Errorf("merchant vocabulary = %+v", v)
	}
	custom, err := recipes.ParseProfile(recipes.AuthFull, recipes.ScopeCustom)
	if err != nil {
		t.Fatal(err)
	}
	if v := custom.Vocabulary(); v.Name != recipes.CustomScopeName || !v.Declared {
		t.Errorf("custom vocabulary = %+v, want the app's own %s", v, recipes.CustomScopeName)
	}
	for _, scope := range []string{recipes.ScopeNone, recipes.ScopeSingle, recipes.DefaultScopeName} {
		p, err := recipes.ParseProfile(recipes.AuthFull, scope)
		if err != nil {
			t.Fatal(err)
		}
		if got := p.Vocabulary(); got.Name != recipes.DefaultScopeName || got.Declared {
			t.Errorf("--scope %s vocabulary = %+v, want the undeclared organisations one", scope, got)
		}
	}
}
