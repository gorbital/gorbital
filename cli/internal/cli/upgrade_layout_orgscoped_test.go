package cli

import (
	"fmt"
	"strings"
	"testing"

	"gorbital.dev/cli/internal/recipes"
)

// TestOrgScopedReadsTheWholeList: orb upgrade --layout v0.2 decides from
// internal/app/permissions.go whether a module's permissions belong to the
// organisation roles or to the platform "user" role. It used to read only
// the first 20 lines under the anchor, so in an app with more org-scoped
// resources than that the oldest ones were regraded to "user" -- a role
// every signed-in account holds -- instead of "owner", "admin", "member"
// (internal security review, 2026-09, CLI-1).
func TestOrgScopedReadsTheWholeList(t *testing.T) {
	var list strings.Builder
	list.WriteString("func orgResourcePermissions() []resourcePermissions {\n\treturn []resourcePermissions{\n\t\t")
	list.WriteString(recipes.OrgPermissionsAnchor)
	list.WriteString("\n")
	for i := range 40 {
		fmt.Fprintf(&list, "\t\tresource%dPermissions,\n", i)
	}
	list.WriteString("\t}\n}\n")

	m := &layoutMove{ours: map[string][]byte{"internal/app/permissions.go": []byte(list.String())}}
	for _, name := range []string{"resource0", "resource19", "resource20", "resource39"} {
		if !m.orgScoped(name) {
			t.Errorf("orgScoped(%q) = false; its permissions would go to the platform \"user\" role", name)
		}
	}
	for _, name := range []string{"resource40", "billing", ""} {
		if m.orgScoped(name) {
			t.Errorf("orgScoped(%q) = true, but the list doesn't name it", name)
		}
	}

	// A module named only after the list closes is not org-scoped.
	trailing := list.String() + "\nvar _ = otherPermissions,\n"
	m = &layoutMove{ours: map[string][]byte{"internal/app/permissions.go": []byte(trailing)}}
	if m.orgScoped("other") {
		t.Error(`orgScoped("other") = true for a name outside the list`)
	}

	// No file and no anchor both mean "not org-scoped".
	if (&layoutMove{ours: map[string][]byte{}}).orgScoped("projects") {
		t.Error("orgScoped without permissions.go = true")
	}
	m = &layoutMove{ours: map[string][]byte{"internal/app/permissions.go": []byte("package app\n\nvar projectsPermissions = 1\n")}}
	if m.orgScoped("projects") {
		t.Error("orgScoped without the anchor = true")
	}
}
