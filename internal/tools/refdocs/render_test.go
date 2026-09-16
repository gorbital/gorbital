package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestDumpTestHasBuildConstraint(t *testing.T) {
	// dumpApp removes this line; without it the overlay file would be skipped.
	if !bytes.HasPrefix(dumpTest, []byte("//go:build ignore\n")) {
		t.Fatal("overlay/reference_dump_test.go must start with //go:build ignore")
	}
}

func TestHumanDuration(t *testing.T) {
	for in, want := range map[string]string{
		"720h0m0s":   "30 days",
		"8760h0m0s":  "1 year",
		"87600h0m0s": "10 years",
		"36h0m0s":    "36 hours",
		"1h0m0s":     "1 hour",
		"15m0s":      "15 minutes",
		"30s":        "30 seconds",
		"1m30s":      "90 seconds",
		"soon":       "`soon`",
	} {
		if got := humanDuration(in); got != want {
			t.Errorf("humanDuration(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSchedule(t *testing.T) {
	for in, want := range map[string]string{
		"":           "on demand",
		"@every 1h":  "`@every 1h` (every hour)",
		"@every 15m": "`@every 15m` (every 15 minutes)",
		"30 3 * * *": "`30 3 * * *` (daily at 03:30)",
		"5 * * * *":  "`5 * * * *` (hourly at minute 5)",
		"0 3 * * 1":  "`0 3 * * 1`",
		"@daily":     "`@daily`",
	} {
		if got := schedule(in); got != want {
			t.Errorf("schedule(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConstraints(t *testing.T) {
	for _, tc := range []struct {
		in   map[string]any
		want string
	}{
		{nil, "any"},
		{map[string]any{"min": "24h0m0s", "max": "8760h0m0s"}, "1 day to 1 year"},
		{map[string]any{"min": float64(3), "max": float64(100)}, "3 to 100"},
		{map[string]any{"max_len": float64(100)}, "at most 100 characters"},
		{map[string]any{"format": "email"}, "an email address"},
		{map[string]any{"one_of": []any{"a", "b"}, "max_items": float64(5)}, "at most 5 items; one of `a`, `b`"},
	} {
		if got := constraints(tc.in); got != tc.want {
			t.Errorf("constraints(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestStatuses(t *testing.T) {
	for _, tc := range []struct {
		in   []code
		want string
	}{
		{[]code{{Status: 404}}, "404"},
		{[]code{{Status: 409}, {Status: 409, Generic: true}}, "409"},
		{[]code{{Status: 500}, {Status: 500, Generic: true}, {Status: 501, Generic: true}}, "500, any other 5xx"},
		{[]code{{Status: 0}}, "varies"},
	} {
		if got := statuses(tc.in); got != tc.want {
			t.Errorf("statuses(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestArea(t *testing.T) {
	for in, want := range map[string]string{
		"internal/modules/auth/delivery/auth.go": "auth",
		"internal/app/module_ops.go":             "ops",
		"internal/app/routes.go":                 "app",
		"gorbital.dev/httpx":                     "app",
	} {
		if got := area(in); got != want {
			t.Errorf("area(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSentence(t *testing.T) {
	for in, want := range map[string]string{
		"the cursor is not valid":    "The cursor is not valid.",
		"return_to must be absolute": "return_to must be absolute.",
		"Done.":                      "Done.",
		"":                           "",
	} {
		if got := sentence(in); got != want {
			t.Errorf("sentence(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPagesMarkMultiTenantOnlyNames(t *testing.T) {
	multi := dump{
		ServiceName: "acme-api",
		Catalogs:    []catalog{{Name: "platform", Roles: []role{{Name: "user", Permissions: []string{"orgs.org.create"}}}, Permissions: []permission{{Name: "orgs.org.create", Description: "create organisations"}}}, {Name: "org", Roles: []role{{Name: "owner", Permissions: []string{"orgs.org.read"}}}, Permissions: []permission{{Name: "orgs.org.read", Description: "see the organisation"}}}},
		Settings:    []setting{{Key: "mail.from_name", Kind: "string", Group: "mail", Default: []byte(`"acme-api"`)}, {Key: "orgs.max_owned", Kind: "int", Group: "orgs", Default: []byte(`20`)}},
		Jobs:        []job{{Name: "orgs_purge", Definition: true, Description: "Purges.", Schedule: "45 3 * * *", Timeout: "10m0s"}},
		Codes:       []code{{Code: "org_not_found", Status: 404, Detail: "no organisation", Location: "internal/app/module_orgs.go"}},
		Actions:     []action{{Action: "orgs.org.created", Metadata: []string{"personal"}}},
	}
	single := dump{
		ServiceName: "acme-api",
		Catalogs:    []catalog{{Name: "platform", Roles: []role{{Name: "user", Permissions: []string{"notes.note.read"}}}, Permissions: []permission{{Name: "notes.note.read", Description: "see your notes"}}}},
		Settings:    multi.Settings[:1],
	}
	r := newReference(multi, single, descriptions{AuditActions: map[string]string{"orgs.org.created": "An organisation was created."}})
	pages := map[string]string{}
	for _, p := range r.pages() {
		pages[p.file] = string(p.content)
	}
	for file, want := range map[string]string{
		"error-codes.md":   "| `org_not_found` | 404 | No organisation. *Multi-tenant apps only.* | `/v1/orgs`, `/v1/invitations` |",
		"audit-actions.md": "## Organisations\n\n*Multi-tenant apps only.*",
		"permissions.md":   "*Multi-tenant apps only.* Every member",
		"settings.md":      "| `mail.from_name` | string | the app's name |",
		"jobs.md":          "Purges. *Multi-tenant apps only.* |",
	} {
		if !strings.Contains(pages[file], want) {
			t.Errorf("%s doesn't contain %q:\n%s", file, want, pages[file])
		}
	}
	// A platform permission only one app declares is marked.
	if want := "| `orgs.org.create` | Create organisations. *Multi-tenant apps only.* | yes |\n| `notes.note.read` | See your notes. *Single-tenant apps only.* | yes |"; !strings.Contains(pages["permissions.md"], want) {
		t.Errorf("permissions.md doesn't contain %q:\n%s", want, pages["permissions.md"])
	}
	if w := r.warnings(); len(w) != 0 {
		t.Errorf("warnings = %q, want none", w)
	}
}
