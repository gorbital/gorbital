package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// newMigrationApp creates a stand-in for a Full preset app with one
// migration and makes it the working directory.
func newMigrationApp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/shop\n\ngo 1.26.0\n")
	writeFile(t, filepath.Join(dir, "db", "migrations", "20260915000001_auth.sql"), "-- auth\n")
	writeFile(t, filepath.Join(dir, "db", "migrations", "migrations.go"), "package migrations\n")
	t.Chdir(dir)
	return dir
}

func TestGenMigration(t *testing.T) {
	newMigrationApp(t)
	code, out, errOut := runOrb(t, "gen", "migration", "AddCustomerPhone", "--json")
	if code != 0 {
		t.Fatalf("orb gen migration = %d, stderr %q", code, errOut)
	}
	var res genMigrationResult
	if err := json.Unmarshal([]byte(out), &res); err != nil || res.Name != "add_customer_phone" || res.DryRun ||
		res.Version <= "20260915000001" || res.File != "db/migrations/"+res.Version+"_add_customer_phone.sql" {
		t.Fatalf("orb gen migration --json = %q (%v)", out, err)
	}
	want := "-- Add customer phone.\n--\n" +
		"-- Change this migration freely until it is released; afterwards, add a new\n" +
		"-- one.\n\n" +
		"-- +goose Up\n"
	got := readFile(t, filepath.FromSlash(res.File))
	if got != want {
		t.Errorf("%s =\n%s\nwant\n%s", res.File, got, want)
	}
	// Goose parses any comment line mentioning +goose as an annotation and
	// rejects the file, so only the Up line may.
	if n := strings.Count(got, "+goose"); n != 1 {
		t.Errorf("%s mentions +goose %d times, want only the Up annotation", res.File, n)
	}

	// The same name again is a new migration that runs after the first.
	code, out, errOut = runOrb(t, "gen", "migration", "add-customer-phone")
	if code != 0 || !strings.Contains(out, "✓ Created migration db/migrations/") || !strings.Contains(out, "go run ./cmd/migrate") {
		t.Fatalf("orb gen migration again = %d %q %q", code, out, errOut)
	}
	entries, _ := os.ReadDir(filepath.Join("db", "migrations"))
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 4 || !strings.HasSuffix(names[1], "_add_customer_phone.sql") || !strings.HasSuffix(names[2], "_add_customer_phone.sql") || names[1] == names[2] {
		t.Errorf("migrations = %v, want two add_customer_phone migrations after auth", names)
	}
}

func TestGenMigrationDryRunWritesNothing(t *testing.T) {
	dir := newMigrationApp(t)
	code, out, errOut := runOrb(t, "gen", "migration", "--dry-run", "create_invoices")
	if code != 0 || !strings.Contains(out, "Would create (dry run) migration db/migrations/") || strings.Contains(out, "Next:") {
		t.Fatalf("orb gen migration --dry-run = %d %q %q", code, out, errOut)
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, "db", "migrations")); len(entries) != 2 {
		t.Errorf("dry run wrote a migration: %v", entries)
	}
}

func TestGenMigrationValidation(t *testing.T) {
	dir := newMigrationApp(t)
	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantErr  string
	}{
		{"missing name", []string{"gen", "migration", "--yes"}, 2, "missing migration name"},
		{"two names", []string{"gen", "migration", "add", "phone"}, 2, "takes one name"},
		{"starts with a digit", []string{"gen", "migration", "2fa"}, 2, "must start with a letter"},
		{"path", []string{"gen", "migration", "../../etc/passwd"}, 2, "must start with a letter"},
		{"SQL", []string{"gen", "migration", "x;DROP TABLE users"}, 2, "must start with a letter"},
		{"too long", []string{"gen", "migration", strings.Repeat("a", 61)}, 2, "max 60"},
		{"unknown flag", []string{"gen", "migration", "x", "--down"}, 1, "flag provided but not defined"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, errOut := runOrb(t, tt.args...)
			if code != tt.wantCode || !strings.Contains(errOut, tt.wantErr) {
				t.Errorf("orb %s = %d %q, want %d containing %q", strings.Join(tt.args, " "), code, errOut, tt.wantCode, tt.wantErr)
			}
		})
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, "db", "migrations")); len(entries) != 2 {
		t.Errorf("failed commands wrote migrations: %v", entries)
	}
}

func TestGenMigrationOutsideFullPresetApp(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/minimal\n")
	t.Chdir(dir)
	if code, _, errOut := runOrb(t, "gen", "migration", "add_phone"); code != 1 || !strings.Contains(errOut, "db/migrations") || !strings.Contains(errOut, "Full preset") {
		t.Errorf("orb gen migration in a Minimal app = %d %q, want Full preset guidance", code, errOut)
	}
}

func TestMigrationName(t *testing.T) {
	for input, want := range map[string]string{
		"add_customer_phone":   "add_customer_phone",
		"AddCustomerPhone":     "add_customer_phone",
		"add-customer-phone":   "add_customer_phone",
		" create_invoices ":    "create_invoices",
		"AddV2Index":           "add_v2_index",
		"HTTPLogs":             "http_logs",
		"drop__legacy--tables": "drop_legacy_tables",
	} {
		words, err := migrationName(input)
		if got := strings.Join(words, "_"); err != nil || got != want {
			t.Errorf("migrationName(%q) = %q, %v, want %q", input, got, err, want)
		}
	}
	for _, input := range []string{"", "_add", "add phone", "añadir"} {
		if words, err := migrationName(input); err == nil {
			t.Errorf("migrationName(%q) = %v, want an error", input, words)
		}
	}
	if !slices.Equal(splitWords("CleanupSessions"), []string{"cleanup", "sessions"}) {
		t.Errorf("splitWords(CleanupSessions) = %v", splitWords("CleanupSessions"))
	}
}
