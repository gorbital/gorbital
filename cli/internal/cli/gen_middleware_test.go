package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGenMiddleware generates each kind into a copy of Shelfie and compares
// the files with testdata/gen-middleware; rewrite them with -update.
func TestGenMiddleware(t *testing.T) {
	dir := newMainApp(t, false)
	golden := filepath.Join(repoRoot(t), "cli", "internal", "cli", "testdata", "gen-middleware")
	for _, tt := range []struct {
		args    []string
		kind    string
		files   []string
		wire    string
		summary string
	}{
		{[]string{"RequireClientVersion", "--module", "books"}, "module",
			[]string{"internal/modules/books/delivery/require_client_version.go", "internal/modules/books/delivery/require_client_version_test.go"},
			"gorbital.Use(RequireClientVersion)", "delivery.RequireClientVersion to Middleware in internal/modules/books/module.go"},
		{[]string{"active-subscription", "--module", "books", "--guard"}, "guard",
			[]string{"internal/modules/books/delivery/active_subscription.go", "internal/modules/books/delivery/active_subscription_test.go"},
			"ActiveSubscription()", `{Err: delivery.ErrActiveSubscriptionRefused, Status: http.StatusForbidden, Code: "active_subscription_refused"`},
		{[]string{"TenantHeader", "--global"}, "global",
			[]string{"internal/middleware/tenant_header.go", "internal/middleware/tenant_header_test.go", "internal/middleware/doc.go"},
			"gorbital.WithMiddleware(middleware.TenantHeader)", `importing "example.com/shelfie/internal/middleware"`},
	} {
		t.Run(tt.kind, func(t *testing.T) {
			code, out, errOut := runOrb(t, append([]string{"gen", "middleware", "--allow-dirty", "--json"}, tt.args...)...)
			var res genMiddlewareResult
			if code != 0 || json.Unmarshal([]byte(out), &res) != nil || res.Kind != tt.kind || res.Wire != tt.wire || strings.Join(res.Files, " ") != strings.Join(tt.files, " ") {
				t.Fatalf("orb gen middleware %v = %d %s %s", tt.args, code, out, errOut)
			}
			for _, f := range tt.files {
				got := readFile(t, filepath.Join(dir, filepath.FromSlash(f)))
				path := filepath.Join(golden, filepath.Base(f)+".golden")
				if *updateJSON {
					writeFile(t, path, got)
				}
				if want := readFile(t, path); got != want {
					t.Errorf("%s:\n%s\nwant (%s, rewrite with -update):\n%s", f, got, path, want)
				}
			}
			// Again, as a dry run: refused, the file exists.
			if code, _, errOut := runOrb(t, append([]string{"gen", "middleware", "--dry-run"}, tt.args...)...); code != 1 || !strings.Contains(errOut, "already declares") {
				t.Errorf("orb gen middleware again = %d %q", code, errOut)
			}
		})
	}

	// The text output names the next steps.
	code, out, _ := runOrb(t, "gen", "middleware", "Audit", "--module", "books", "--guard", "--dry-run")
	if code != 0 || !strings.Contains(out, "Would create (dry run) guard Audit") || !strings.Contains(out, "403 audit_refused") {
		t.Errorf("orb gen middleware --dry-run = %d %s", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "internal", "modules", "books", "delivery", "audit.go")); err == nil {
		t.Error("--dry-run wrote the guard")
	}
}

func TestGenMiddlewareErrors(t *testing.T) {
	newMainApp(t, false)
	writeFile(t, filepath.Join("internal", "middleware", "doc.go"), "// Package middleware.\npackage middleware\n")
	for _, tt := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{}, 2, "missing name"},
		{[]string{"A", "B", "--global"}, 2, "unexpected arguments"},
		{[]string{"Timing"}, 2, "--module <name>"},
		{[]string{"Timing", "--global", "--module", "books"}, 2, "exclude each other"},
		{[]string{"Timing", "--global", "--guard"}, 2, "--guard needs --module"},
		{[]string{"Timing", "--module", "Books"}, 2, "must be the name of a directory"},
		{[]string{"Timing", "--module", "authors"}, 1, "internal/modules/authors/delivery doesn't exist"},
		{[]string{"Register", "--module", "books"}, 2, "can't name middleware"},
		{[]string{"BookResponse", "--module", "books"}, 1, "already declares BookResponse"},
		{[]string{"9lives", "--global"}, 2, "must start with a letter"},
	} {
		code, _, errOut := runOrb(t, append([]string{"gen", "middleware", "--dry-run"}, tt.args...)...)
		if code != tt.code || !strings.Contains(errOut, tt.want) {
			t.Errorf("orb gen middleware %q = %d %q, want %d containing %q", tt.args, code, errOut, tt.code, tt.want)
		}
	}
	// The package comment already exists: the first global middleware adds no doc.go.
	code, out, errOut := runOrb(t, "gen", "middleware", "Timing", "--global", "--dry-run", "--json")
	var res genMiddlewareResult
	if code != 0 || json.Unmarshal([]byte(out), &res) != nil || len(res.Files) != 2 {
		t.Errorf("orb gen middleware --global with doc.go = %d %s %s", code, out, errOut)
	}

	newResourceApp(t)
	if code, _, errOut := runOrb(t, "gen", "middleware", "Timing", "--global"); code != 1 || !strings.Contains(errOut, "v0.1 layout") {
		t.Errorf("orb gen middleware in a v0.1 app = %d %q", code, errOut)
	}
}
