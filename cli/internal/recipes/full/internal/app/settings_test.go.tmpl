package app_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

// platformOnlyGroups and platformOnlyPrefixes hold settings that protect
// every account and organisation, so no organisation may set its own value
// (ADR-0056): sign-in, rate limits, retention, maintenance and who email
// comes from.
var (
	platformOnlyGroups   = []string{"rate_limits", "retention", "maintenance", "mail"}
	platformOnlyPrefixes = []string{"auth.", "audit.", "mail.", "maintenance.", "ops.", "releases."}
	// Where invitation links, with their tokens, go; a limit per user; how
	// long deleted organisations are kept.
	platformOnlyKeys = []string{"orgs.invitation_url", "orgs.max_owned", "orgs.deleted_org_retention", "orgs.user_invitations_per_hour"}
)

// TestSecuritySettingsArentOrgOverridable checks every declared setting: a
// security-relevant one declared settings.OrgOverridable would let an
// organisation's admins change it for their organisation.
func TestSecuritySettingsArentOrgOverridable(t *testing.T) {
	a := newApp(t, nil)
	ops, _ := signIn(t, a, "ops@example.com", "platform_admin")
	r := do(t, a.Handler(), "GET", "/ops/settings", "", ops...)
	all, _ := r.json["settings"].([]any)
	if r.code != http.StatusOK || len(all) == 0 {
		t.Fatalf("GET /ops/settings = %d %s", r.code, r.body)
	}
	for _, s := range all {
		setting := s.(map[string]any)
		key, _ := setting["key"].(string)
		group, _ := setting["group"].(string)
		if setting["org_overridable"] != true {
			continue
		}
		if slices.Contains(platformOnlyGroups, group) || slices.Contains(platformOnlyKeys, key) ||
			slices.ContainsFunc(platformOnlyPrefixes, func(p string) bool { return strings.HasPrefix(key, p) }) {
			t.Errorf("%s (group %s) is declared OrgOverridable, but it protects the whole platform; remove settings.OrgOverridable()", key, group)
		}
	}
}
