package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorbital.dev/cli/internal/recipes"
)

func addRLS(t *testing.T, wantCode int, args ...string) (addRLSResult, string, string) {
	t.Helper()
	code, out, errOut := runOrb(t, append([]string{"add", "rls"}, args...)...)
	if code != wantCode {
		t.Fatalf("orb add rls %v = %d, want %d; stdout %s stderr %s", args, code, wantCode, out, errOut)
	}
	var res addRLSResult
	if strings.Contains(strings.Join(args, " "), "--json") && out != "" {
		if err := json.Unmarshal([]byte(out), &res); err != nil {
			t.Fatalf("orb add rls --json output %q: %v", out, err)
		}
	}
	return res, out, errOut
}

// TestAddRLSWritesTheMigration: orb add rls copies db/row_level_security.sql
// into a new migration, records rls: true in gorbital.yaml and the lock, is
// a no-op the second time, and orb upgrade and orb gen resource keep it.
func TestAddRLSWritesTheMigration(t *testing.T) {
	newGitApp(t, "--preset", "full", "--tenancy", "multi")

	dry, _, _ := addRLS(t, 0, "--dry-run", "--json")
	if !dry.DryRun || !strings.HasSuffix(dry.Migration, "_row_level_security.sql") || git(t, "status", "--porcelain") != "" {
		t.Errorf("dry run = %+v and changed the app", dry)
	}

	_, out, _ := addRLS(t, 0)
	if !strings.Contains(out, "superuser") || !strings.Contains(out, "go run ./cmd/migrate") {
		t.Errorf("next steps = %q, want the database role and migrate", out)
	}
	migrations, _ := filepath.Glob("db/migrations/*_row_level_security.sql")
	all, _ := filepath.Glob("db/migrations/*.sql")
	if len(migrations) != 1 || all[len(all)-1] != migrations[0] {
		t.Fatalf("migrations = %v, want one after every existing migration (%v)", migrations, all)
	}
	if readFile(t, migrations[0]) != readFile(t, recipes.RowLevelSecurityPath) {
		t.Error("the migration isn't db/row_level_security.sql")
	}
	if !strings.Contains(readFile(t, "gorbital.yaml"), "\nrls: true\n") {
		t.Errorf("gorbital.yaml:\n%s", readFile(t, "gorbital.yaml"))
	}
	lock, err := readLock(".")
	if err != nil || !lock.Inputs.RLS {
		t.Errorf("lock inputs = %+v, %v; want rls", lock.Inputs, err)
	}
	assertLockRebuilds(t, ".")
	commitAll(t, "Add row-level security")

	// Again: nothing changes.
	again, _, _ := addRLS(t, 0, "--json")
	if !again.AlreadyOn || git(t, "status", "--porcelain") != "" {
		t.Errorf("second run = %+v; status %q", again, git(t, "status", "--porcelain"))
	}

	// orb upgrade rebuilds the base with rls: true, so the app is up to date
	// instead of losing the line.
	useRelease(t, recipes.Embedded())
	if up := upgrade(t, 0, "--from", "v0.5.0"); !up.UpToDate {
		t.Errorf("upgrade after orb add rls = %+v, want up to date", up)
	}

	// Organisation resources generated from now on carry the policy; user
	// resources don't need one.
	code, _, errOut := runOrb(t, "gen", "resource", "Customer", "name:string", "--allow-dirty", "--json")
	if code != 0 {
		t.Fatalf("orb gen resource = %d: %s", code, errOut)
	}
	customers, _ := filepath.Glob("db/migrations/*_customers.sql")
	if len(customers) != 1 || !strings.Contains(readFile(t, customers[0]), "FORCE ROW LEVEL SECURITY") || !strings.Contains(readFile(t, customers[0]), "CREATE POLICY org_isolation ON customers") {
		t.Errorf("customers migration %v has no policy", customers)
	}
	if code, _, errOut := runOrb(t, "gen", "resource", "Note", "title:string", "--scope", "user", "--allow-dirty"); code != 0 {
		t.Fatalf("orb gen resource --scope user = %d: %s", code, errOut)
	}
	notes, _ := filepath.Glob("db/migrations/*_notes.sql")
	if len(notes) != 1 || strings.Contains(readFile(t, notes[0]), "ROW LEVEL SECURITY") {
		t.Errorf("user-scoped notes migration %v has a policy", notes)
	}
}

func TestAddRLSRefuses(t *testing.T) {
	t.Run("single-tenant", func(t *testing.T) {
		newGitApp(t, "--preset", "full")
		if _, _, errOut := addRLS(t, 1); !strings.Contains(errOut, "single-tenant") || !strings.Contains(errOut, "orb add orgs") {
			t.Errorf("stderr = %q", errOut)
		}
	})
	t.Run("minimal preset", func(t *testing.T) {
		newGitApp(t, "--preset", "minimal")
		if _, _, errOut := addRLS(t, 1); !strings.Contains(errOut, "single-tenant") {
			t.Errorf("stderr = %q", errOut)
		}
	})
	t.Run("older release", func(t *testing.T) {
		newGitApp(t, "--preset", "full", "--tenancy", "multi")
		l, _ := readLock(".")
		l.Orb = lockOrb{Version: "v0.4.9"}
		b, _ := l.encode()
		writeFile(t, lockPath, string(b))
		commitAll(t, "Older lock")
		if _, _, errOut := addRLS(t, 1); !strings.Contains(errOut, "run orb upgrade first") {
			t.Errorf("stderr = %q", errOut)
		}
	})
	t.Run("dirty tree", func(t *testing.T) {
		newGitApp(t, "--preset", "full", "--tenancy", "multi")
		writeFile(t, "scratch.txt", "x")
		if _, _, errOut := addRLS(t, 1); !strings.Contains(errOut, "uncommitted changes") {
			t.Errorf("stderr = %q", errOut)
		}
		if m, _ := filepath.Glob("db/migrations/*_row_level_security.sql"); len(m) != 0 {
			t.Errorf("wrote %v in a dirty tree", m)
		}
	})
	t.Run("lock recording it for a single-tenant app", func(t *testing.T) {
		newGitApp(t, "--preset", "full")
		l, _ := readLock(".")
		l.Inputs.RLS = true
		b, _ := l.encode()
		writeFile(t, lockPath, string(b))
		if _, err := readLock("."); err == nil || !strings.Contains(err.Error(), "without organisations") {
			t.Errorf("readLock() error = %v", err)
		}
		if err := os.WriteFile("gorbital.yaml", []byte(readFile(t, "gorbital.yaml")+"rls: true\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := readManifest("."); err == nil {
			t.Error("readManifest() accepted rls: true for a single-tenant app")
		}
	})
}

func TestDoctorReportsRowLevelSecurity(t *testing.T) {
	newGitApp(t, "--preset", "full", "--tenancy", "multi")
	writeFile(t, ".env", readFile(t, ".env.example"))
	fakeDoctorCommands(t, `{"current":9,"latest":9,"pending":0,"row_level_security":["row-level security is on, but the database role app is a superuser or has BYPASSRLS"]}`)
	res := doctorRun(t, 0)
	if c := check(res, "row-level security"); c.Status != doctorWarn || !strings.Contains(c.Detail, "BYPASSRLS") {
		t.Errorf("row-level security check = %+v", c)
	}
}
