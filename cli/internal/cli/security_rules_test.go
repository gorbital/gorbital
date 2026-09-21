package cli

import (
	"path/filepath"
	"slices"
	"testing"
)

// securityFixture is the directory of one of the two fixtures
// orb doctor --security's rules are tested against.
func securityFixture(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "cli", "internal", "cli", "testdata", "security", name)
}

// TestSecurityRulesAreQuietOnTheGoldenApps is the rule set's first
// obligation. Every one of these apps is what orb new writes, so a finding
// in one is a false positive by definition, and a false positive is what
// teaches people to stop reading the output.
func TestSecurityRulesAreQuietOnTheGoldenApps(t *testing.T) {
	for _, app := range []string{"full-multi", "full-single", "api-basic", "shelfie"} {
		t.Run(app, func(t *testing.T) {
			dir := filepath.Join(repoRoot(t), "examples", app)
			for _, f := range scanApp(dir).rules() {
				t.Errorf("%s:%d %s: %s", f.File, f.Line, f.Rule, f.Message)
			}
		})
	}
}

// TestSecurityRulesFindThePlantedFlaws runs every rule against a fixture
// holding one instance of each (roadmap item 44), and against a near-miss
// fixture holding the same code written properly.
func TestSecurityRulesFindThePlantedFlaws(t *testing.T) {
	want := map[string][]string{
		"credential-stored-unhashed": {
			"db/migrations/20260101000001_fixture.sql",     // the column
			"internal/modules/fixture/repository/store.go", // a table no migration declares
		},
		"password-without-kdf":            {"internal/modules/fixture/repository/store.go"},
		"scope-query-without-soft-delete": {"internal/modules/fixture/repository/store.go"},
		"secret-compared-directly":        {"internal/modules/fixture/usecase/tokens.go"},
		"weak-random-secret":              {"internal/modules/fixture/usecase/tokens.go"},
		"secret-in-log":                   {"internal/modules/fixture/usecase/tokens.go"},
		"credential-in-route-path":        {"internal/modules/fixture/delivery/routes.go"},
	}
	found := map[string]int{}
	for _, f := range scanApp(securityFixture(t, "flawed")).rules() {
		found[f.Rule]++
		if f.Line == 0 || f.Message == "" || f.Why == "" || f.Fix == "" || f.Docs == "" {
			t.Errorf("%s has an incomplete finding: %+v", f.Rule, f)
		}
		if files, ok := want[f.Rule]; ok && !slices.Contains(files, f.File) {
			t.Errorf("%s reported %s, want one of %q", f.Rule, f.File, files)
		}
	}
	for _, rule := range securityRules {
		if found[rule.Name] == 0 {
			t.Errorf("%s found nothing in the flawed fixture", rule.Name)
		}
	}
	// The one mistake reported twice is the one to watch: the flawed
	// fixture writes its plaintext token column as well as declaring it,
	// and the migration is the only place it should be reported.
	if found["credential-stored-unhashed"] != 2 {
		t.Errorf("credential-stored-unhashed reported %d times, want 2: the migration's column and the statement writing a table no migration declares", found["credential-stored-unhashed"])
	}

	for _, f := range scanApp(securityFixture(t, "near-miss")).rules() {
		t.Errorf("false positive: %s:%d %s: %s", f.File, f.Line, f.Rule, f.Message)
	}
}

func TestCredentialNames(t *testing.T) {
	for name, want := range map[string]bool{
		"token": true, "session_token": true, "sessionToken": true, "APIKey": true,
		"api_key": true, "secret": true, "password": true, "privateKey": true,
		"token_hash": false, "password_hash": false, "secret_ciphertext": false,
		"refresh_token_ciphertext": false, "key_id": false, "keyID": false,
		"api_key_prefix": false, "password_changed_at": false, "key": false,
		"credential": false, "pkce_verifier": false, "lookup_id": false,
	} {
		if got := credentialName(name); got != want {
			t.Errorf("credentialName(%q) = %v, want %v", name, got, want)
		}
	}
}
